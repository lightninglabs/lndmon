package collectors

import (
	"context"
	"fmt"
	"sync"

	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/prometheus/client_golang/prometheus"
)

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
)

// paymentsMonitor listens for payments and updates Prometheus metrics.
type paymentsMonitor struct {
	client routerrpc.RouterClient

	lnd *lndclient.LndServices

	errChan chan error

	// quit is closed to signal that we need to shutdown.
	quit chan struct{}

	wg sync.WaitGroup
}

// newPaymentsMonitor creates a new payments monitor and ensures the context
// includes macaroon authentication.
func newPaymentsMonitor(lnd *lndclient.LndServices,
	errChan chan error) *paymentsMonitor {

	return &paymentsMonitor{
		client:  routerrpc.NewRouterClient(lnd.ClientConn),
		lnd:     lnd,
		errChan: errChan,
		quit:    make(chan struct{}),
	}
}

// start subscribes to `TrackPayments` and updates Prometheus metrics.
func (p *paymentsMonitor) start() error {
	paymentLogger.Info("Starting payments monitor...")

	// Attach macaroon authentication for the router service.
	ctx, cancel := context.WithCancel(context.Background())
	ctx, err := p.lnd.WithMacaroonAuthForService(
		ctx, lndclient.RouterServiceMac,
	)
	if err != nil {
		cancel()

		return fmt.Errorf("failed to get macaroon-authenticated "+
			"context: %w", err)
	}

	stream, err := p.client.TrackPayments(
		ctx, &routerrpc.TrackPaymentsRequest{
			// NOTE: We only need to know the final result of the
			// payment and all attempts.
			NoInflightUpdates: true,
		},
	)
	if err != nil {
		paymentLogger.Errorf("Failed to subscribe to TrackPayments: %v",
			err)

		cancel()

		return err
	}

	p.wg.Add(1)
	go func() {
		defer func() {
			cancel()
			p.wg.Done()
		}()

		for {
			select {
			case <-p.quit:
				return

			default:
				payment, err := stream.Recv()
				if err != nil {
					paymentLogger.Errorf("Error receiving "+
						"payment update: %v", err)

					p.errChan <- err
					return
				}
				processPaymentUpdate(payment)
			}
		}
	}()

	return nil
}

// stop cancels the payments monitor subscription.
func (p *paymentsMonitor) stop() {
	paymentLogger.Info("Stopping payments monitor...")

	close(p.quit)
	p.wg.Wait()
}

// collectors returns all of the collectors that the htlc monitor uses.
func (p *paymentsMonitor) collectors() []prometheus.Collector {
	return []prometheus.Collector{
		totalPayments, totalHTLCAttempts, paymentAttempts,
	}
}

// processPaymentUpdate updates Prometheus metrics based on received payments.
//
// NOTE: It is expected that this receive the *final* payment update with the
// complete list of all htlc attempts made for this payment.
func processPaymentUpdate(payment *lnrpc.Payment) {
	var status string

	switch payment.Status {
	case lnrpc.Payment_SUCCEEDED:
		status = "succeeded"
	case lnrpc.Payment_FAILED:
		status = "failed"
	default:
		// We don't expect this given that this should be a terminal
		// payment update.
		status = "unknown"
	}

	// Increment metrics with proper label.
	totalPayments.WithLabelValues(status).Inc()

	attemptCount := len(payment.Htlcs)
	totalHTLCAttempts.WithLabelValues(status).Add(float64(attemptCount))

	paymentAttempts.Observe(float64(attemptCount))
	paymentLogger.Debugf("Payment %s updated: status=%s, %d attempts",
		payment.PaymentHash, status, attemptCount)
}
