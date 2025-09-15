package collectors

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/htlcswitch"
	"github.com/lightningnetwork/lnd/invoices"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	outcomeLabel        = "outcome"
	outcomeSettledValue = "settled"
	outcomeFailedValue  = "failed"

	typeLabel        = "type"
	typeForwardValue = "forward"
	typeReceiveValue = "receive"
	typeSendValue    = "send"

	chanInLabel  = "chan_in"
	chanOutLabel = "chan_out"

	// failureReasonLabel is the variable label we use for failure reasons
	// for forwards.
	failureReasonLabel = "failure_reason"

	// failureReasonExternal is a special value for the failureReason
	// that we use when a forward is failed back to us and we do not know
	// the exact reason for failure.
	failureReasonExternal = "failed_back"
)

// htlcLabels is the set of labels we use to label htlc events.
var htlcLabels = []string{outcomeLabel, chanInLabel, chanOutLabel, typeLabel}

// htlcMonitor contains the elements required to monitor our htlc stream. Since
// we collect metrics from a stream, rather than a set of custom polled metrics,
// we just use the built-in prometheus types to monitor our htlcs, and do not
// implement the collector interface.
type htlcMonitor struct {
	// router provides us with access to lnd's router rpc.
	router lndclient.RouterClient

	// resolvedCounter is a counter which tracks the number of resolved
	// htlcs.
	resolvedCounter *prometheus.CounterVec

	// activeHtlcs holds a map of our currently active htlcs to their
	// original forward time.
	activeHtlcs map[htlcswitch.HtlcKey]time.Time

	// resolutionTimeHistogram tracks the time it takes our htlcs to
	// resolve.
	resolutionTimeHistogram *prometheus.HistogramVec

	// quit is closed to signal that we need to shutdown.
	quit chan struct{}

	wg sync.WaitGroup

	// errChan is a channel that we send any errors that we encounter into.
	// This channel should be buffered so that it does not block sends.
	errChan chan<- error
}

func newHtlcMonitor(router lndclient.RouterClient,
	errChan chan error) *htlcMonitor {

	return &htlcMonitor{
		router:      router,
		activeHtlcs: make(map[htlcswitch.HtlcKey]time.Time),
		resolvedCounter: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "lnd",
			Subsystem: "htlcs",
			Name:      "resolved_htlcs",
			Help:      "count of resolved htlcs",
		}, append(htlcLabels, failureReasonLabel)),
		resolutionTimeHistogram: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "lnd",
				Subsystem: "htlcs",
				Name:      "resolution_time",
				Help: "the time (in seconds) taken to " +
					"resolve a htlc",
				// Buckets are broken up into 1s/10s/1m/2m/5m/
				// 10m and 1h/5h/1d/7d with the logic that if a
				// payment doesn't resolve quickly, it will
				// likely take very long.
				Buckets: []float64{
					1, 10, 60, 60 * 2, 60 * 5, 60 * 10,
					60 * 60, 60 * 60 * 5, 60 * 60 * 24,
					60 * 60 * 24 * 7,
				},
			},
			htlcLabels,
		),
		quit:    make(chan struct{}),
		errChan: errChan,
	}
}

// start begins the main event loop for the htlc monitor.
func (h *htlcMonitor) start() error {
	htlcLogger.Info("Starting Htlc Monitor")

	return h.consumeHtlcEvents()
}

// stop sends the htlc monitor's goroutines the instruction to shutdown and
// waits for them to exit.
func (h *htlcMonitor) stop() {
	htlcLogger.Info("Stopping Htlc Monitor")

	close(h.quit)
	h.wg.Wait()
}

// collectors returns all of the collectors that the htlc monitor uses.
func (h *htlcMonitor) collectors() []prometheus.Collector {
	return []prometheus.Collector{
		h.resolvedCounter, h.resolutionTimeHistogram,
	}
}

// consumeHtlcEvents subscribes to a stream of htlc events from lnd and records
// running totals for our htlcs.
func (h *htlcMonitor) consumeHtlcEvents() error {
	// Create a context to subscribe to events and cancel it on exit so that
	// lnd can cancel the stream.
	ctx, cancel := context.WithCancel(context.Background())

	htlcEvents, htlcErrChan, err := h.router.SubscribeHtlcEvents(ctx)
	if err != nil {
		cancel()
		return err
	}

	h.wg.Add(1)
	go func() {
		defer func() {
			cancel()
			h.wg.Done()
		}()

		for {
			select {
			case event, ok := <-htlcEvents:
				if !ok {
					h.errChan <- errors.New("htlc event " +
						"stream termianted")
					return
				}

				err := h.processHtlcEvent(event)
				if err != nil {
					h.errChan <- err
					return
				}

			case err, ok := <-htlcErrChan:
				h.errChan <- fmt.Errorf("htlc stream "+
					"exited: %v, closed: %v", err, ok)
				return

			case <-h.quit:
				h.errChan <- errors.New("htlc collector " +
					"shutting down")
				return
			}
		}
	}()

	return nil
}

// getKeyAndTimestamp is a helper function that extracts the key and timestamp from an event.
func getKeyAndTimestamp(event *routerrpc.HtlcEvent) (htlcswitch.HtlcKey, time.Time) {
	key := htlcswitch.HtlcKey{
		IncomingCircuit: invoices.CircuitKey{
			ChanID: lnwire.NewShortChanIDFromInt(
				event.IncomingChannelId,
			),
			HtlcID: event.IncomingHtlcId,
		},
		OutgoingCircuit: invoices.CircuitKey{
			ChanID: lnwire.NewShortChanIDFromInt(
				event.OutgoingChannelId,
			),
			HtlcID: event.OutgoingHtlcId,
		},
	}

	ts := time.Unix(0, int64(event.TimestampNs))

	return key, ts
}

// processHtlcEvent processes all the htlc events we consume from our stream.
func (h *htlcMonitor) processHtlcEvent(event *routerrpc.HtlcEvent) error {
	switch e := event.Event.(type) {
	// If we have received a forwarding event, we add it to our map if it
	// is not already present. We are ok with duplicate events, because
	// htlcs are sometimes replayed by the switch, but we want to keep our
	// earliest timestamp for stats.
	case *routerrpc.HtlcEvent_ForwardEvent:
		key, ts := getKeyAndTimestamp(event)
		if _, ok := h.activeHtlcs[key]; ok {
			htlcLogger.Infof("htlc: %v replayed", key)
			return nil
		}

		// Add to our set of known active htlcs.
		h.activeHtlcs[key] = ts

	case *routerrpc.HtlcEvent_SettleEvent:
		key, ts := getKeyAndTimestamp(event)
		err := h.recordResolution(key, event.EventType, ts, "")
		if err != nil {
			return err
		}

	case *routerrpc.HtlcEvent_ForwardFailEvent:
		key, ts := getKeyAndTimestamp(event)
		err := h.recordResolution(
			key, event.EventType, ts, failureReasonExternal,
		)
		if err != nil {
			return err
		}

	case *routerrpc.HtlcEvent_LinkFailEvent:
		key, ts := getKeyAndTimestamp(event)
		err := h.recordResolution(
			key, event.EventType, ts, e.LinkFailEvent.FailureDetail.Enum().String(),
		)
		if err != nil {
			return err
		}

	case *routerrpc.HtlcEvent_SubscribedEvent:
		return nil

	case *routerrpc.HtlcEvent_FinalHtlcEvent:
		return nil

	default:
		err := h.recordResolution(
			key, event.EventType, ts, "unknown",
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// recordResolution records the outcome of a htlc resolution (settle/fail) in
// our metrics. The failure reason string should be empty for all successful
// forwards, and populated for all failures.
func (h *htlcMonitor) recordResolution(key htlcswitch.HtlcKey,
	eventType routerrpc.HtlcEvent_EventType, ts time.Time,
	failureReason string) error {

	// Create the set of labels we want to track this resolution. Remove
	// spaces from our failure reason so that it can be used as a prometheus
	// label.
	labels := map[string]string{
		outcomeLabel: outcomeFailedValue,
		chanInLabel: strconv.FormatUint(
			key.IncomingCircuit.ChanID.ToUint64(), 10,
		),
		chanOutLabel: strconv.FormatUint(
			key.OutgoingCircuit.ChanID.ToUint64(), 10,
		),
		failureReasonLabel: strings.ToLower(strings.ReplaceAll(
			failureReason, " ", "_",
		)),
	}
	if failureReason == "" {
		labels[outcomeLabel] = outcomeSettledValue
	}

	switch eventType {
	case routerrpc.HtlcEvent_FORWARD:
		labels[typeLabel] = typeForwardValue

	case routerrpc.HtlcEvent_RECEIVE:
		labels[typeLabel] = typeReceiveValue

	case routerrpc.HtlcEvent_SEND:
		labels[typeLabel] = typeSendValue

	default:
		return fmt.Errorf("unknown event type: %v", eventType)
	}

	h.resolvedCounter.With(labels).Add(1)

	// If this HTLC was a receive, we have no originally forwarded htlc
	// tracked so we can return early.
	if labels[typeLabel] == typeReceiveValue {
		return nil
	}

	// Lookup the original forward for our own sends and forwards. We don't
	// worry if we can't find it because htlcs are only tracked in memory
	// (we might have restarted after we forwarded it, so would not have it
	// tracked).
	fwdTs, ok := h.activeHtlcs[key]
	if !ok {
		htlcLogger.Infof("resolved htlc: %v original forward "+
			"not found", key)

		return nil
	}

	// We make a copy of our labels rather than delete the unneeded failure
	// reason label so that we don't run into any unexpected behaviour from
	// map references.
	histogramLabels := make(map[string]string, len(htlcLabels))
	for _, label := range htlcLabels {
		histogramLabels[label] = labels[label]
	}

	// Add the amount of time the htlc took to resolve to our histogram.
	h.resolutionTimeHistogram.With(
		histogramLabels,
	).Observe(ts.Sub(fwdTs).Seconds())

	// Delete the htlc from our set of active htlcs.
	delete(h.activeHtlcs, key)
	return nil
}
