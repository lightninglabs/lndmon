package collectors

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/btcsuite/btclog/v2"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type paymentPageClient struct {
	lnrpc.LightningClient

	responses []*lnrpc.ListPaymentsResponse
	requests  []*lnrpc.ListPaymentsRequest
	deadlines []bool
}

func (c *paymentPageClient) ListPayments(ctx context.Context,
	request *lnrpc.ListPaymentsRequest,
	_ ...grpc.CallOption) (*lnrpc.ListPaymentsResponse, error) {

	c.requests = append(c.requests, request)
	_, deadline := ctx.Deadline()
	c.deadlines = append(c.deadlines, deadline)
	if len(c.responses) == 0 {
		return nil, errors.New("unexpected ListPayments request")
	}
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

type paymentHistoryClient struct {
	lndclient.LightningClient
	raw lnrpc.LightningClient
}

func (c *paymentHistoryClient) RawClientWithMacAuth(ctx context.Context) (
	context.Context, time.Duration, lnrpc.LightningClient) {

	return ctx, time.Second, c.raw
}

func seededPaymentMonitor(t *testing.T, client *paymentPageClient) (
	*paymentsMonitor, *prometheus.Registry) {

	t.Helper()
	previousLogger := paymentLogger
	paymentLogger = btclog.Disabled
	t.Cleanup(func() { paymentLogger = previousLogger })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	monitor := &paymentsMonitor{
		lnd: &lndclient.LndServices{
			Client: &paymentHistoryClient{raw: client},
		},
		ctx:          ctx,
		cancel:       cancel,
		errChan:      make(chan error, 1),
		seedMetrics:  true,
		rpcTimeout:   time.Second,
		streamReady:  make(chan struct{}),
		seenPayments: make(map[paymentIdentity]struct{}),
	}
	registry := prometheus.NewRegistry()
	registry.MustRegister(monitor.collectors()...)
	return monitor, registry
}

// Existing metrics are package globals. Read deltas instead of resetting them,
// and keep these tests sequential so they do not change other tests' baselines.
func paymentMetric(t *testing.T, registry *prometheus.Registry,
	name, status string) *dto.Metric {

	t.Helper()
	families, err := registry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if status == "" {
				return metric
			}
			for _, label := range metric.GetLabel() {
				if label.GetName() == "status" && label.GetValue() == status {
					return metric
				}
			}
		}
	}
	return &dto.Metric{}
}

type paymentMetricTotals struct {
	volume, fees, duration, hops float64
	durations, routes            uint64
	payments, attempts           float64
	attemptSamples               uint64
}

func readPaymentTotals(t *testing.T, registry *prometheus.Registry,
	status string) paymentMetricTotals {

	t.Helper()
	duration := paymentMetric(t, registry, "lnd_payment_duration_seconds", status).GetHistogram()
	hops := paymentMetric(t, registry, "lnd_payment_num_hops", status).GetHistogram()
	return paymentMetricTotals{
		volume:         paymentMetric(t, registry, "lnd_total_payments_sat", status).GetCounter().GetValue(),
		fees:           paymentMetric(t, registry, "lnd_total_payments_fees_sat", status).GetCounter().GetValue(),
		duration:       duration.GetSampleSum(),
		hops:           hops.GetSampleSum(),
		durations:      duration.GetSampleCount(),
		routes:         hops.GetSampleCount(),
		payments:       paymentMetric(t, registry, "lnd_total_payments", status).GetCounter().GetValue(),
		attempts:       paymentMetric(t, registry, "lnd_total_htlc_attempts", status).GetCounter().GetValue(),
		attemptSamples: paymentMetric(t, registry, "lnd_payment_attempts_per_payment", "").GetHistogram().GetSampleCount(),
	}
}

func successfulPayment(index uint64, hash string) *lnrpc.Payment {
	return &lnrpc.Payment{
		PaymentIndex: index, PaymentHash: hash,
		Status:    lnrpc.Payment_SUCCEEDED,
		ValueMsat: 1500, FeeMsat: 125, CreationTimeNs: int64(time.Second),
		Htlcs: []*lnrpc.HTLCAttempt{{
			Status:        lnrpc.HTLCAttempt_SUCCEEDED,
			ResolveTimeNs: int64(3 * time.Second),
			Route:         &lnrpc.Route{Hops: []*lnrpc.Hop{{}, {}}},
		}},
	}
}

func paymentHistoryResponses(payment *lnrpc.Payment) []*lnrpc.ListPaymentsResponse {
	return []*lnrpc.ListPaymentsResponse{
		{Payments: []*lnrpc.Payment{{PaymentIndex: payment.PaymentIndex}}},
		{Payments: []*lnrpc.Payment{payment}, LastIndexOffset: payment.PaymentIndex},
	}
}

func recordLivePayment(monitor *paymentsMonitor, payment *lnrpc.Payment) {
	status, terminal := terminalPaymentStatus(payment)
	if terminal {
		recordLivePaymentMetrics(payment, status)
		monitor.recordPaymentMetrics(payment)
	}
}

func TestPaymentHistoryDeduplicatesLiveOverlap(t *testing.T) {
	for _, liveFirst := range []bool{false, true} {
		payment := successfulPayment(7, "snapshot-stream-overlap")
		client := &paymentPageClient{responses: paymentHistoryResponses(payment)}
		monitor, registry := seededPaymentMonitor(t, client)
		before := readPaymentTotals(t, registry, "succeeded")
		if liveFirst {
			recordLivePayment(monitor, payment)
		}
		require.NoError(t, monitor.seedHistory())
		if !liveFirst {
			// A history scan must not seed any pre-existing metrics.
			seeded := readPaymentTotals(t, registry, "succeeded")
			require.Equal(t, before.payments, seeded.payments)
			require.Equal(t, before.attempts, seeded.attempts)
			require.Equal(t, before.attemptSamples, seeded.attemptSamples)
			recordLivePayment(monitor, payment)
		}
		after := readPaymentTotals(t, registry, "succeeded")
		require.InDelta(t, 1.5, after.volume-before.volume, 1e-9)
		require.InDelta(t, 0.125, after.fees-before.fees, 1e-9)
		require.EqualValues(t, 1, after.durations-before.durations)
		require.InDelta(t, 2, after.duration-before.duration, 1e-9)
		require.EqualValues(t, 1, after.routes-before.routes)
		require.InDelta(t, 2, after.hops-before.hops, 1e-9)
		require.InDelta(t, 1, after.payments-before.payments, 1e-9)
		require.InDelta(t, 1, after.attempts-before.attempts, 1e-9)
		require.EqualValues(t, 1, after.attemptSamples-before.attemptSamples)
		require.Equal(t, []bool{true, true}, client.deadlines)
		require.True(t, client.requests[0].Reversed)
		require.True(t, client.requests[1].IncludeIncomplete)
	}
}

func TestPaymentRetryWithSameHashHasDistinctIndex(t *testing.T) {
	first := successfulPayment(10, "retried-hash")
	first.Status = lnrpc.Payment_FAILED
	first.FeeMsat = 0
	first.Htlcs[0].Status = lnrpc.HTLCAttempt_FAILED
	client := &paymentPageClient{responses: paymentHistoryResponses(first)}
	monitor, registry := seededPaymentMonitor(t, client)
	before := readPaymentTotals(t, registry, "failed")
	require.NoError(t, monitor.seedHistory())

	// LND replaces failed attempts under the same hash with a new index.
	retry := *first
	retry.PaymentIndex = 11
	monitor.recordPaymentMetrics(&retry)
	monitor.recordPaymentMetrics(&retry)
	after := readPaymentTotals(t, registry, "failed")
	require.InDelta(t, 3, after.volume-before.volume, 1e-9)
	require.EqualValues(t, 2, after.durations-before.durations)
	require.EqualValues(t, 2, after.routes-before.routes)
	require.Equal(t, before.payments, after.payments)
}

func TestPaymentReconciliationClosesInitialSubscriptionGap(t *testing.T) {
	terminal := successfulPayment(4, "completed-before-subscription")
	inflight := *terminal
	inflight.Status = lnrpc.Payment_IN_FLIGHT
	client := &paymentPageClient{responses: paymentHistoryResponses(&inflight)}
	monitor, registry := seededPaymentMonitor(t, client)
	before := readPaymentTotals(t, registry, "succeeded")
	require.NoError(t, monitor.seedHistory())
	require.Equal(t, before, readPaymentTotals(t, registry, "succeeded"))

	// No terminal event was received for this payment. A later unrelated
	// stream update proves registration, triggering a fresh final scan.
	client.responses = paymentHistoryResponses(terminal)
	monitor.markStreamReady()
	monitor.wg.Add(1)
	monitor.reconcileHistory()
	after := readPaymentTotals(t, registry, "succeeded")
	require.InDelta(t, 1.5, after.volume-before.volume, 1e-9)
	require.EqualValues(t, 1, after.durations-before.durations)
	require.Equal(t, before.payments, after.payments)
	require.Len(t, client.requests, 4)
	require.Empty(t, monitor.errChan)
}

func TestPaymentHistoryUsesFixedUpperIndexAndAllowsDeletedGaps(t *testing.T) {
	client := &paymentPageClient{
		responses: []*lnrpc.ListPaymentsResponse{
			{Payments: []*lnrpc.Payment{{PaymentIndex: 9}}},
			{Payments: []*lnrpc.Payment{successfulPayment(3, "three")}, LastIndexOffset: 3},
			{
				Payments: []*lnrpc.Payment{
					successfulPayment(9, "nine"), successfulPayment(12, "newer"),
				},
				LastIndexOffset: 12,
			},
		},
	}
	monitor, registry := seededPaymentMonitor(t, client)
	before := readPaymentTotals(t, registry, "succeeded")
	require.NoError(t, monitor.seedHistory())
	after := readPaymentTotals(t, registry, "succeeded")
	require.InDelta(t, 3, after.volume-before.volume, 1e-9)
	require.EqualValues(t, 2, after.durations-before.durations)
	require.Len(t, client.requests, 3)
	require.EqualValues(t, 0, client.requests[1].IndexOffset)
	require.EqualValues(t, 3, client.requests[2].IndexOffset)
}

func TestPaymentHistoryRejectsNonProgressingPage(t *testing.T) {
	client := &paymentPageClient{
		responses: []*lnrpc.ListPaymentsResponse{
			{Payments: []*lnrpc.Payment{{PaymentIndex: 9}}},
			{Payments: []*lnrpc.Payment{successfulPayment(3, "three")}, LastIndexOffset: 3},
			{Payments: []*lnrpc.Payment{successfulPayment(3, "three")}, LastIndexOffset: 3},
		},
	}
	monitor, _ := seededPaymentMonitor(t, client)
	require.Error(t, monitor.seedHistory())
	require.Len(t, client.requests, 3)
}

func TestTerminalHTLCAttempt(t *testing.T) {
	firstShard := &lnrpc.HTLCAttempt{
		Status:        lnrpc.HTLCAttempt_SUCCEEDED,
		AttemptTimeNs: 10,
		ResolveTimeNs: 20,
	}
	lastShard := &lnrpc.HTLCAttempt{
		Status:        lnrpc.HTLCAttempt_SUCCEEDED,
		AttemptTimeNs: 5,
		ResolveTimeNs: 30,
	}
	earlyAttemptLateFailure := &lnrpc.HTLCAttempt{
		Status:        lnrpc.HTLCAttempt_FAILED,
		AttemptTimeNs: 1,
		ResolveTimeNs: 50,
	}
	lateAttemptEarlyFailure := &lnrpc.HTLCAttempt{
		Status:        lnrpc.HTLCAttempt_FAILED,
		AttemptTimeNs: 15,
		ResolveTimeNs: 40,
	}
	inflight := &lnrpc.HTLCAttempt{
		Status:        lnrpc.HTLCAttempt_IN_FLIGHT,
		AttemptTimeNs: 60,
	}
	cases := []struct {
		name     string
		attempts []*lnrpc.HTLCAttempt
		want     *lnrpc.HTLCAttempt
	}{
		{
			name: "multipart payment uses last resolved successful shard",
			attempts: []*lnrpc.HTLCAttempt{
				firstShard, lastShard, earlyAttemptLateFailure,
			},
			want: lastShard,
		},
		{
			name: "failure uses resolution order instead of attempt order",
			attempts: []*lnrpc.HTLCAttempt{
				lateAttemptEarlyFailure, earlyAttemptLateFailure,
			},
			want: earlyAttemptLateFailure,
		},
		{
			name:     "ignore nil and unfinished attempts",
			attempts: []*lnrpc.HTLCAttempt{nil, inflight},
		},
		{
			name: "empty payment has no terminal attempt",
		},
	}
	for _, test := range cases {
		got := terminalHTLCAttempt(test.attempts)
		if got != test.want {
			t.Errorf("%s: got %v, want %v", test.name, got, test.want)
		}
	}
}
