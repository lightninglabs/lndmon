package collectors

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lightninglabs/lndclient"
	"github.com/stretchr/testify/require"
)

// fakeInfoLND is a minimal lndclient.LightningClient used to count GetInfo
// calls. It embeds the interface so only GetInfo needs an implementation; any
// other method called would nil-panic, which is fine as the tests only exercise
// GetInfo.
type fakeInfoLND struct {
	lndclient.LightningClient

	calls int64
	info  *lndclient.Info
	err   error

	// delay optionally widens the window in which concurrent callers pile up
	// behind the single in-flight call.
	delay time.Duration
}

func (f *fakeInfoLND) GetInfo(_ context.Context) (*lndclient.Info, error) {
	atomic.AddInt64(&f.calls, 1)
	if f.delay > 0 {
		time.Sleep(f.delay)
	}

	return f.info, f.err
}

// TestGetInfoSharesSingleCallPerScrape asserts that many concurrent getInfo
// calls sharing one per-scrape provider trigger exactly one underlying GetInfo
// and all observe the same result.
func TestGetInfoSharesSingleCallPerScrape(t *testing.T) {
	t.Parallel()

	info := &lndclient.Info{Alias: "shared"}
	fake := &fakeInfoLND{info: info, delay: 20 * time.Millisecond}

	ctx := withInfoProvider(
		context.Background(), newInfoProvider(fake),
	)

	const n = 50
	var wg sync.WaitGroup
	results := make([]*lndclient.Info, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = getInfo(ctx)
		}(i)
	}
	wg.Wait()

	require.Equal(t, int64(1), atomic.LoadInt64(&fake.calls),
		"GetInfo should run exactly once for the whole scrape")
	for i := 0; i < n; i++ {
		require.NoError(t, errs[i])
		require.Same(t, info, results[i],
			"every caller must see the same shared *Info")
	}
}

// TestInfoProviderPerScrapeIsolation asserts that two separate providers (two
// scrapes) each make their own GetInfo, so a cached result is never shared
// across scrapes.
func TestInfoProviderPerScrapeIsolation(t *testing.T) {
	t.Parallel()

	info := &lndclient.Info{Alias: "iso"}
	fake := &fakeInfoLND{info: info}

	ctxA := withInfoProvider(context.Background(), newInfoProvider(fake))
	ctxB := withInfoProvider(context.Background(), newInfoProvider(fake))

	infoA, err := getInfo(ctxA)
	require.NoError(t, err)
	require.Same(t, info, infoA)
	infoB, err := getInfo(ctxB)
	require.NoError(t, err)
	require.Same(t, info, infoB)

	require.Equal(t, int64(2), atomic.LoadInt64(&fake.calls),
		"each scrape's provider must make its own GetInfo call")
}

// TestGetInfoReusesError asserts that a failed first call is stored and the
// same error is returned to every later caller in the scrape (one scrape, one
// outcome), without re-calling GetInfo.
func TestGetInfoReusesError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	fake := &fakeInfoLND{err: wantErr}

	ctx := withInfoProvider(context.Background(), newInfoProvider(fake))

	for i := 0; i < 3; i++ {
		_, err := getInfo(ctx)
		require.ErrorIs(t, err, wantErr)
	}

	require.Equal(t, int64(1), atomic.LoadInt64(&fake.calls),
		"a stored error must not trigger another GetInfo")
}

// TestGetInfoRequiresProvider pins the visible-failure contract: with no
// provider injected (a wiring mistake), getInfo returns errNoInfoProvider
// rather than silently issuing a direct call.
func TestGetInfoRequiresProvider(t *testing.T) {
	t.Parallel()

	info, err := getInfo(context.Background())
	require.Nil(t, info)
	require.ErrorIs(t, err, errNoInfoProvider)
}

// ctxAwareLND is a fake lndclient.LightningClient whose GetInfo blocks until its
// context is canceled, then returns the context error. It is used to prove the
// single shared GetInfo honors the scrape context's cancellation.
type ctxAwareLND struct {
	lndclient.LightningClient

	calls int64
}

func (f *ctxAwareLND) GetInfo(ctx context.Context) (*lndclient.Info, error) {
	atomic.AddInt64(&f.calls, 1)
	<-ctx.Done()

	return nil, ctx.Err()
}

// TestGetInfoSharesCancellation asserts that canceling the shared scrape context
// cancels the single in-flight GetInfo and that every concurrent caller observes
// the same cancellation error, from one underlying call.
func TestGetInfoSharesCancellation(t *testing.T) {
	t.Parallel()

	fake := &ctxAwareLND{}
	ctx, cancel := context.WithCancel(context.Background())
	ctx = withInfoProvider(ctx, newInfoProvider(fake))

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)
	started := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			started <- struct{}{}
			_, errs[i] = getInfo(ctx)
		}(i)
	}

	// Once every caller is in flight, cancel the shared context the way a
	// disconnecting scraper would.
	for i := 0; i < n; i++ {
		<-started
	}
	cancel()
	wg.Wait()

	require.Equal(t, int64(1), atomic.LoadInt64(&fake.calls),
		"cancellation must not cause extra GetInfo calls")
	for i := 0; i < n; i++ {
		require.ErrorIs(t, errs[i], context.Canceled,
			"every caller must observe the shared cancellation")
	}
}
