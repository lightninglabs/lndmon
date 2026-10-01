package collectors

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	invoiceStateLabel = "state"

	invoiceStateOpenValue     = "open"
	invoiceStateSettledValue  = "settled"
	invoiceStateCanceledValue = "canceled"
	invoiceStateAcceptedValue = "accepted"
	invoiceStateUnknownValue  = "unknown"

	invoicePageSize        = 1000
	invoiceRefreshInterval = time.Minute
)

const (
	invoiceOpenIndex = iota
	invoiceSettledIndex
	invoiceCanceledIndex
	invoiceAcceptedIndex
	invoiceUnknownIndex
	invoiceStateCount
)

var (
	invoiceStates = [invoiceStateCount]string{
		invoiceOpenIndex:     invoiceStateOpenValue,
		invoiceSettledIndex:  invoiceStateSettledValue,
		invoiceCanceledIndex: invoiceStateCanceledValue,
		invoiceAcceptedIndex: invoiceStateAcceptedValue,
		invoiceUnknownIndex:  invoiceStateUnknownValue,
	}

	invoiceCountDesc = prometheus.NewDesc(
		"lnd_invoices", "Number of retained invoices, grouped by "+
			"their current LND-reported state",
		[]string{invoiceStateLabel}, nil,
	)

	invoiceVolumeDesc = prometheus.NewDesc(
		"lnd_invoices_sat", "Volume of retained invoices in satoshis, "+
			"grouped by current LND-reported state; paid amount for "+
			"settled invoices and requested amount otherwise",
		[]string{invoiceStateLabel}, nil,
	)
)

// invoiceSnapshot contains one complete inventory. Values are copied together
// so a scrape cannot combine states from different refreshes.
type invoiceSnapshot struct {
	counts [invoiceStateCount]uint64
	sats   [invoiceStateCount]float64
}

// invoicesMonitor publishes current invoice state, not lifecycle event counts.
// ListInvoices cannot reconstruct historical accepted transitions, while the
// global invoice subscription omits accepted and canceled transitions entirely.
type invoicesMonitor struct {
	lnd        *lndclient.LndServices
	client     lnrpc.LightningClient
	errChan    chan error
	rpcTimeout time.Duration

	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu       sync.RWMutex
	snapshot invoiceSnapshot
	ready    bool
	err      error
}

// newInvoicesMonitor creates an invoice inventory monitor.
func newInvoicesMonitor(lnd *lndclient.LndServices, errChan chan error,
	rpcTimeout time.Duration) *invoicesMonitor {

	if rpcTimeout <= 0 {
		rpcTimeout = 30 * time.Second
	}

	return &invoicesMonitor{
		lnd:        lnd,
		errChan:    errChan,
		rpcTimeout: rpcTimeout,
	}
}

// start reads a complete initial inventory before the exporter opens HTTP.
// The caller's context also interrupts pagination during startup or shutdown.
func (i *invoicesMonitor) start(parent context.Context) error {
	invoiceLogger.Info("Starting invoice inventory monitor...")

	ctx, cancel := context.WithCancel(parent)
	i.cancel = cancel
	ctx, _, client := i.lnd.Client.RawClientWithMacAuth(ctx)
	i.client = client

	snapshot, err := i.readSnapshot(ctx)
	if err != nil {
		cancel()
		return fmt.Errorf("read initial invoice inventory: %w", err)
	}
	i.publish(snapshot)

	i.wg.Add(1)
	go i.run(ctx)

	return nil
}

// run refreshes complete inventories without overlapping pagination calls.
func (i *invoicesMonitor) run(ctx context.Context) {
	defer i.wg.Done()
	defer i.cancel()

	ticker := time.NewTicker(invoiceRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			snapshot, err := i.readSnapshot(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}

				failure := fmt.Errorf("refresh invoice "+
					"inventory: %w", err)
				i.invalidate(failure)
				select {
				case i.errChan <- failure:
				case <-ctx.Done():
				}
				return
			}

			i.publish(snapshot)
		}
	}
}

// stop interrupts in-flight RPCs and also handles monitors that never started.
func (i *invoicesMonitor) stop() {
	invoiceLogger.Info("Stopping invoice inventory monitor...")

	if i.cancel != nil {
		i.cancel()
	}
	i.wg.Wait()
}

// collectors returns one collector so every invoice sample uses one snapshot.
func (i *invoicesMonitor) collectors() []prometheus.Collector {
	return []prometheus.Collector{i}
}

// Describe implements prometheus.Collector.
func (i *invoicesMonitor) Describe(ch chan<- *prometheus.Desc) {
	ch <- invoiceCountDesc
	ch <- invoiceVolumeDesc
}

// Collect implements prometheus.Collector. It emits zero values for empty
// states and refuses to serve an old inventory after a failed refresh.
func (i *invoicesMonitor) Collect(ch chan<- prometheus.Metric) {
	snapshot, err := i.currentSnapshot()
	if err != nil {
		ch <- prometheus.NewInvalidMetric(invoiceCountDesc, err)
		return
	}

	for index, state := range invoiceStates {
		ch <- prometheus.MustNewConstMetric(
			invoiceCountDesc, prometheus.GaugeValue,
			float64(snapshot.counts[index]), state,
		)
		ch <- prometheus.MustNewConstMetric(
			invoiceVolumeDesc, prometheus.GaugeValue,
			snapshot.sats[index], state,
		)
	}
}

func (i *invoicesMonitor) publish(snapshot invoiceSnapshot) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.snapshot = snapshot
	i.ready = true
	i.err = nil
}

func (i *invoicesMonitor) invalidate(err error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.err = err
}

func (i *invoicesMonitor) currentSnapshot() (invoiceSnapshot, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	if i.err != nil {
		return invoiceSnapshot{}, i.err
	}
	if !i.ready {
		return invoiceSnapshot{}, errors.New("invoice inventory is not ready")
	}

	return i.snapshot, nil
}

func (i *invoicesMonitor) listPage(ctx context.Context,
	request *lnrpc.ListInvoiceRequest) (*lnrpc.ListInvoiceResponse, error) {

	rpcCtx, cancel := context.WithTimeout(ctx, i.rpcTimeout)
	defer cancel()

	response, err := i.client.ListInvoices(rpcCtx, request)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("empty ListInvoices response")
	}

	return response, nil
}

// readSnapshot fixes an upper add index before scanning. New invoices cannot
// extend the scan indefinitely. State changes or deletions during pagination
// are reflected by the next refresh; LND supplies no cross-page transaction.
func (i *invoicesMonitor) readSnapshot(ctx context.Context) (
	invoiceSnapshot, error) {

	var snapshot invoiceSnapshot
	latest, err := i.listPage(ctx, &lnrpc.ListInvoiceRequest{
		NumMaxInvoices: 1,
		Reversed:       true,
	})
	if err != nil {
		return snapshot, err
	}
	if len(latest.Invoices) == 0 {
		return snapshot, nil
	}
	if len(latest.Invoices) != 1 || latest.Invoices[0] == nil ||
		latest.Invoices[0].AddIndex == 0 {

		return snapshot, errors.New("invalid latest invoice index")
	}
	upper := latest.Invoices[0].AddIndex

	var offset uint64
	for offset < upper {
		page, err := i.listPage(ctx, &lnrpc.ListInvoiceRequest{
			IndexOffset:    offset,
			NumMaxInvoices: invoicePageSize,
		})
		if err != nil {
			return invoiceSnapshot{}, err
		}
		if len(page.Invoices) == 0 {
			break
		}
		if err := snapshot.addPage(page, offset, upper); err != nil {
			return invoiceSnapshot{}, err
		}
		offset = page.LastIndexOffset
	}

	return snapshot, nil
}

func (s *invoiceSnapshot) addPage(page *lnrpc.ListInvoiceResponse,
	offset, upper uint64) error {

	previous := offset
	for _, invoice := range page.Invoices {
		if invoice == nil || invoice.AddIndex <= previous {
			return errors.New("invoice page has non-increasing indexes")
		}
		previous = invoice.AddIndex
		if invoice.AddIndex > upper {
			continue
		}
		if err := s.addInvoice(invoice); err != nil {
			return err
		}
	}
	if page.LastIndexOffset != previous || page.LastIndexOffset <= offset {
		return errors.New("invoice pagination did not advance correctly")
	}

	return nil
}

func (s *invoiceSnapshot) addInvoice(invoice *lnrpc.Invoice) error {
	state := invoiceUnknownIndex
	amount := invoice.ValueMsat
	switch invoice.State {
	case lnrpc.Invoice_OPEN:
		state = invoiceOpenIndex
	case lnrpc.Invoice_SETTLED:
		state = invoiceSettledIndex
		// The RPC reports an AMP invoice as settled once any set has
		// settled. Its aggregate paid amount includes its retained sets;
		// count the parent once rather than recounting each stream update.
		amount = invoice.AmtPaidMsat
	case lnrpc.Invoice_CANCELED:
		state = invoiceCanceledIndex
	case lnrpc.Invoice_ACCEPTED:
		state = invoiceAcceptedIndex
	}
	if amount < 0 {
		return errors.New("invoice has a negative amount")
	}

	s.counts[state]++
	s.sats[state] += float64(amount) / 1000
	return nil
}
