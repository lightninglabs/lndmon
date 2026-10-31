package collectors

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	paymentHistoryPageSize          = 500
	paymentHistoryReconcileInterval = 30 * time.Second
)

// A failed payment can be retried with the same hash. LND allocates a new
// payment index for that retry, so each index/hash pair is a distinct payment.
type paymentIdentity struct {
	index uint64
	hash  string
}

var (
	// totalPayments tracks the total number of payments initiated, labeled
	// by final payment status. This permits computation of both throughput
	// and success/failure rates.
	totalPayments = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "lnd_total_payments",
			Help: "Total number of payments initiated, labeled by final status",
		},
		[]string{"status"},
	)

	// totalHTLCAttempts tracks the number of HTLC attempts made based on
	// the payment status (success or fail). When combined with the payment
	// counter, this permits tracking the number of attempts per payment.
	totalHTLCAttempts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "lnd_total_htlc_attempts",
			Help: "Total number of HTLC attempts across all payments, labeled by final payment status",
		},
		[]string{"status"},
	)

	// paymentAttempts is a histogram for visualizing what portion of
	// payments complete within a given number of attempts.
	paymentAttempts = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "lnd_payment_attempts_per_payment",
			Help:    "Histogram tracking the number of attempts per payment",
			Buckets: prometheus.ExponentialBuckets(1, 2, 10),
		},
	)

	// totalPaymentsSat tracks the total volume of payments sent in
	// satoshis, labeled by final payment status. This permits computation
	// of both successfully sent volume and volume of failed send
	// attempts.
	totalPaymentsSat = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "lnd_total_payments_sat",
			Help: "Total volume of payments sent in satoshis, labeled by final status",
		},
		[]string{"status"},
	)

	// totalPaymentsFeesSat tracks the total routing fees paid for
	// payments sent, in satoshis, labeled by final payment status. Since
	// failed payments pay no routing fee, in practice only the
	// "succeeded" label accumulates a non-zero total.
	totalPaymentsFeesSat = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "lnd_total_payments_fees_sat",
			Help: "Total routing fees paid for payments sent, in satoshis, labeled by final status",
		},
		[]string{"status"},
	)

	// paymentDuration is a histogram tracking how long it took a payment
	// to reach a terminal state, measured from creation to the
	// resolution of its terminal HTLC attempt. This allows median/avg
	// payment speed to be computed via histogram_quantile and
	// sum/count respectively.
	paymentDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "lnd_payment_duration_seconds",
			Help: "Histogram tracking the time (in seconds) taken " +
				"for a payment to reach a terminal state, " +
				"labeled by final status",
			Buckets: []float64{
				0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300, 600,
			},
		},
		[]string{"status"},
	)

	// paymentHops is a histogram tracking the number of hops in the
	// route used by a payment's terminal HTLC attempt. This allows
	// median/avg hop count to be computed via histogram_quantile and
	// sum/count respectively.
	paymentHops = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "lnd_payment_num_hops",
			Help: "Histogram tracking the number of hops in the " +
				"route of a payment's terminal HTLC attempt, " +
				"labeled by final status",
			Buckets: prometheus.LinearBuckets(1, 1, 20),
		},
		[]string{"status"},
	)
)

// paymentsMonitor listens for payments and updates Prometheus metrics.
type paymentsMonitor struct {
	client routerrpc.RouterClient

	lnd *lndclient.LndServices

	errChan chan error

	seedMetrics  bool
	rpcTimeout   time.Duration
	ctx          context.Context
	cancel       context.CancelFunc
	streamReady  chan struct{}
	readyOnce    sync.Once
	metricsMu    sync.Mutex
	seenPayments map[paymentIdentity]struct{}

	wg sync.WaitGroup
}

// newPaymentsMonitor creates a new payments monitor and ensures the context
// includes macaroon authentication.
func newPaymentsMonitor(lnd *lndclient.LndServices,
	errChan chan error, seedMetrics bool,
	rpcTimeout time.Duration) *paymentsMonitor {

	return &paymentsMonitor{
		client:       routerrpc.NewRouterClient(lnd.ClientConn),
		lnd:          lnd,
		errChan:      errChan,
		seedMetrics:  seedMetrics,
		rpcTimeout:   rpcTimeout,
		streamReady:  make(chan struct{}),
		seenPayments: make(map[paymentIdentity]struct{}),
	}
}

// start subscribes to `TrackPayments` and updates Prometheus metrics.
func (p *paymentsMonitor) start(parent context.Context) error {
	paymentLogger.Info("Starting payments monitor...")
	if p.seedMetrics && p.rpcTimeout <= 0 {
		return fmt.Errorf("payment history RPC timeout must be positive")
	}
	p.ctx, p.cancel = context.WithCancel(parent)

	// Attach macaroon authentication for the router service.
	ctx, err := p.lnd.WithMacaroonAuthForService(
		p.ctx, lndclient.RouterServiceMac,
	)
	if err != nil {
		p.cancel()

		return fmt.Errorf("failed to get macaroon-authenticated "+
			"context: %w", err)
	}

	stream, err := p.client.TrackPayments(
		ctx, &routerrpc.TrackPaymentsRequest{
			// NOTE: We only need to know the final result of the
			// payment and all attempts.
			// In seed mode any first update confirms registration.
			// Nonterminal updates never increment metrics.
			NoInflightUpdates: !p.seedMetrics,
		},
	)
	if err != nil {
		paymentLogger.Errorf("Failed to subscribe to TrackPayments: %v",
			err)

		p.cancel()

		return err
	}

	p.wg.Add(1)
	go p.receivePayments(stream)

	if p.seedMetrics {
		// Complete the initial history before the exporter exposes its
		// HTTP endpoint. The reader runs concurrently to avoid blocking
		// LND's stream while history is paged.
		if err := p.seedHistory(); err != nil {
			p.stop()
			return err
		}
		p.wg.Add(1)
		go p.reconcileHistory()
	}

	return nil
}

// stop cancels the payments monitor subscription.
func (p *paymentsMonitor) stop() {
	paymentLogger.Info("Stopping payments monitor...")

	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
}

func (p *paymentsMonitor) receivePayments(
	stream routerrpc.Router_TrackPaymentsClient) {

	defer p.wg.Done()
	defer p.cancel()
	for {
		payment, err := stream.Recv()
		if err != nil {
			p.reportError(fmt.Errorf("receive payment update: %w", err))
			return
		}
		p.readyOnce.Do(p.markStreamReady)
		status, terminal := terminalPaymentStatus(payment)
		if !terminal {
			continue
		}
		recordLivePaymentMetrics(payment, status)
		p.recordPaymentMetrics(payment)
	}
}

func (p *paymentsMonitor) markStreamReady() {
	close(p.streamReady)
}

func (p *paymentsMonitor) reportError(err error) {
	if p.ctx.Err() != nil {
		return
	}
	paymentLogger.Error(err)
	select {
	case p.errChan <- err:
	case <-p.ctx.Done():
	}
}

// TrackPayments has no registration acknowledgement. Returning from its client
// call does not prove that LND installed the subscription, so one startup scan
// can miss a payment finalizing before registration. Until the first received
// update proves registration, repeat the bounded scan. Then scan once more,
// starting after that update, to cover the entire gap. Quiet nodes keep polling
// every 30 seconds; each pass has a fixed highest index and bounded RPCs.
func (p *paymentsMonitor) reconcileHistory() {
	defer p.wg.Done()
	ticker := time.NewTicker(paymentHistoryReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-p.streamReady:
			p.reportHistoryError(p.seedHistory())
			return
		case <-ticker.C:
			if err := p.seedHistory(); err != nil {
				p.reportHistoryError(err)
				return
			}
		}
	}
}

func (p *paymentsMonitor) reportHistoryError(err error) {
	if err != nil {
		p.reportError(fmt.Errorf("seed payment metrics: %w", err))
		p.cancel()
	}
}

func (p *paymentsMonitor) listPayments(
	request *lnrpc.ListPaymentsRequest) (*lnrpc.ListPaymentsResponse, error) {

	ctx, cancel := context.WithTimeout(p.ctx, p.rpcTimeout)
	defer cancel()
	// The lndclient conversion omits CreationTimeNs, which duration
	// observations need. Reuse its authenticated raw client instead.
	ctx, _, client := p.lnd.Client.RawClientWithMacAuth(ctx)
	response, err := client.ListPayments(ctx, request)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, fmt.Errorf("ListPayments returned a nil response")
	}
	return response, nil
}

// seedHistory reads retained history, not a durable event log. Deleted payments
// and failed retries replaced by LND cannot be reconstructed. The highest index
// is fixed at the start of each pass so continuous new payments cannot prevent
// the pass from finishing. Only the new metrics receive historical values.
func (p *paymentsMonitor) seedHistory() error {
	latest, err := p.listPayments(&lnrpc.ListPaymentsRequest{
		IncludeIncomplete: true,
		MaxPayments:       1,
		Reversed:          true,
	})
	if err != nil {
		return err
	}
	if len(latest.Payments) == 0 {
		return nil
	}
	if latest.Payments[0] == nil || latest.Payments[0].PaymentIndex == 0 {
		return fmt.Errorf("ListPayments returned an invalid payment index")
	}
	upperIndex := latest.Payments[0].PaymentIndex
	var offset uint64
	for offset < upperIndex {
		page, err := p.listPayments(&lnrpc.ListPaymentsRequest{
			IncludeIncomplete: true,
			IndexOffset:       offset,
			MaxPayments:       paymentHistoryPageSize,
		})
		if err != nil {
			return err
		}
		if len(page.Payments) == 0 {
			return nil
		}
		next := offset
		for _, payment := range page.Payments {
			if payment == nil || payment.PaymentIndex <= next {
				return fmt.Errorf("ListPayments indices did not advance")
			}
			next = payment.PaymentIndex
			if next <= upperIndex {
				p.recordPaymentMetrics(payment)
			}
		}
		if page.LastIndexOffset != next {
			return fmt.Errorf("ListPayments returned an inconsistent offset")
		}
		offset = next
	}
	return nil
}

// recordPaymentMetrics serializes the snapshot and live paths. Terminal IDs
// stay in memory for this process so overlapping scans and duplicate stream
// events cannot add a second histogram observation. Inflight IDs are not marked
// as seen: their eventual terminal update still needs to be recorded.
func (p *paymentsMonitor) recordPaymentMetrics(payment *lnrpc.Payment) {
	status, terminal := terminalPaymentStatus(payment)
	if !terminal {
		return
	}
	p.metricsMu.Lock()
	defer p.metricsMu.Unlock()
	if p.seedMetrics {
		identity := paymentIdentity{
			index: payment.PaymentIndex,
			hash:  payment.PaymentHash,
		}
		if _, exists := p.seenPayments[identity]; exists {
			return
		}
		p.seenPayments[identity] = struct{}{}
	}
	recordPaymentMetrics(payment, status)
}

// collectors returns all of the collectors that the htlc monitor uses.
func (p *paymentsMonitor) collectors() []prometheus.Collector {
	return []prometheus.Collector{
		totalPayments, totalHTLCAttempts, paymentAttempts,
		totalPaymentsSat, totalPaymentsFeesSat, paymentDuration,
		paymentHops,
	}
}

func terminalPaymentStatus(payment *lnrpc.Payment) (string, bool) {
	if payment == nil {
		return "", false
	}
	switch payment.Status {
	case lnrpc.Payment_SUCCEEDED:
		return "succeeded", true
	case lnrpc.Payment_FAILED:
		return "failed", true
	default:
		return "", false
	}
}

// recordLivePaymentMetrics retains the existing stream-only count and attempt
// metrics. Historical seeding never calls this helper.
func recordLivePaymentMetrics(payment *lnrpc.Payment, status string) {
	totalPayments.WithLabelValues(status).Inc()
	attemptCount := len(payment.Htlcs)
	totalHTLCAttempts.WithLabelValues(status).Add(float64(attemptCount))
	paymentAttempts.Observe(float64(attemptCount))
	paymentLogger.Debugf("Payment %s updated: status=%s, %d attempts",
		payment.PaymentHash, status, attemptCount)
}

// recordPaymentMetrics updates only the metrics added with payment seeding.
func recordPaymentMetrics(payment *lnrpc.Payment, status string) {
	totalPaymentsSat.WithLabelValues(status).Add(
		float64(payment.ValueMsat) / 1000,
	)
	totalPaymentsFeesSat.WithLabelValues(status).Add(
		float64(payment.FeeMsat) / 1000,
	)

	// Record duration through the final successful shard, or the last
	// resolved failed attempt. Hop count describes that selected attempt;
	// it is not the sum of the routes of all MPP shards.
	if htlc := terminalHTLCAttempt(payment.Htlcs); htlc != nil {
		if payment.CreationTimeNs > 0 && htlc.ResolveTimeNs > 0 {
			durationNs := htlc.ResolveTimeNs - payment.CreationTimeNs
			if durationNs > 0 {
				paymentDuration.WithLabelValues(status).Observe(
					time.Duration(durationNs).Seconds(),
				)
			}
		}

		if htlc.Route != nil {
			paymentHops.WithLabelValues(status).Observe(
				float64(len(htlc.Route.Hops)),
			)
		}
	}
}

// terminalHTLCAttempt selects the last-resolved successful shard, or the
// last-resolved failed attempt when none succeeded. Selecting the first success
// would understate the duration of a multipart payment.
func terminalHTLCAttempt(htlcs []*lnrpc.HTLCAttempt) *lnrpc.HTLCAttempt {
	var successfulAttempt, failedAttempt *lnrpc.HTLCAttempt

	for _, htlc := range htlcs {
		if htlc == nil {
			continue
		}
		if htlc.Status == lnrpc.HTLCAttempt_SUCCEEDED {
			if successfulAttempt == nil ||
				htlc.ResolveTimeNs > successfulAttempt.ResolveTimeNs {

				successfulAttempt = htlc
			}
			continue
		}
		if htlc.Status != lnrpc.HTLCAttempt_FAILED {
			continue
		}
		if failedAttempt == nil ||
			htlc.ResolveTimeNs > failedAttempt.ResolveTimeNs {

			failedAttempt = htlc
		}
	}
	if successfulAttempt != nil {
		return successfulAttempt
	}
	return failedAttempt
}
