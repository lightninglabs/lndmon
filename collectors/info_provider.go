package collectors

import (
	"context"
	"errors"
	"sync"

	"github.com/lightninglabs/lndclient"
)

// errNoInfoProvider is returned by getInfo when no per-scrape infoProvider is
// present in the context. serveScrape always injects one, so hitting this is a
// wiring mistake (a collector reached outside serveScrape, or the injection was
// dropped). We surface it instead of silently falling back to a direct GetInfo,
// so a regression to one call per collector fails visibly rather than hiding in
// production.
var errNoInfoProvider = errors.New("no infoProvider in context: GetInfo must " +
	"be shared per scrape")

// infoProvider runs lnd.GetInfo at most once per scrape and shares the single
// (result, error) with every collector that needs it. GetInfo is known to
// sometimes take far longer than average, so calling it once per scrape instead
// of once per collector cuts both lnd load and slow-scrape risk.
//
// IMPORTANT: a fresh infoProvider MUST be allocated per scrape (done in
// serveScrape) and reached only through that scrape's context. It must never be
// stored on a long-lived singleton collector: a sync.Once there would hold
// the first scrape's result forever and let concurrent scrapes share a cached
// result.
type infoProvider struct {
	lnd  lndclient.LightningClient
	once sync.Once
	info *lndclient.Info
	err  error
}

// newInfoProvider returns an infoProvider that will fetch GetInfo from lnd on
// first use.
func newInfoProvider(lnd lndclient.LightningClient) *infoProvider {
	return &infoProvider{lnd: lnd}
}

// GetInfo performs the single lnd.GetInfo on the first call, under the context
// of whichever collector arrives first, and returns the stored (info, err) to
// every later caller. Concurrent callers block in once.Do until the first
// finishes, so all collectors observe the same result and so classify the same
// error identically. sync.Once provides the happens-before edge that publishes
// info/err to the blocked readers, so no extra mutex is needed.
func (p *infoProvider) GetInfo(ctx context.Context) (*lndclient.Info, error) {
	p.once.Do(func() {
		p.info, p.err = p.lnd.GetInfo(ctx)
	})

	return p.info, p.err
}

// infoProviderKey is the unexported zero-size context key for the per-scrape
// infoProvider (the collision-free context key pattern).
type infoProviderKey struct{}

// withInfoProvider returns a child context carrying p. The child preserves the
// parent's cancellation and deadline, so a scraper disconnect still cancels the
// shared GetInfo.
func withInfoProvider(ctx context.Context, p *infoProvider) context.Context {
	return context.WithValue(ctx, infoProviderKey{}, p)
}

// getInfo is the single entry point the chain, info and channels collectors
// call instead of c.lnd.GetInfo. It requires a per-scrape infoProvider in ctx;
// if none is present it returns errNoInfoProvider rather than silently issuing a
// direct call.
//
// serveScrape injects one provider into a single scrape context shared by every
// collector, so all callers in a scrape pass the same ctx and the single RPC
// runs under that shared deadline/cancellation no matter which collector wins
// the once.Do race.
func getInfo(ctx context.Context) (*lndclient.Info, error) {
	p, ok := ctx.Value(infoProviderKey{}).(*infoProvider)
	if !ok {
		return nil, errNoInfoProvider
	}

	return p.GetInfo(ctx)
}
