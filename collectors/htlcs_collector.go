package collectors

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/htlcswitch"
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

	// activeHtlcs holds a map of our currently active htlcs.
	activeHtlcs map[htlcswitch.HtlcKey]struct{}

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
		activeHtlcs: make(map[htlcswitch.HtlcKey]struct{}),
		resolvedCounter: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "lnd",
			Subsystem: "htlcs",
			Name:      "resolved_htlcs",
			Help:      "count of resolved htlcs",
		}, htlcLabels),
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
		h.resolvedCounter,
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

// processHtlcEvent processes all the htlc events we consume from our stream.
func (h *htlcMonitor) processHtlcEvent(event *routerrpc.HtlcEvent) error {
	key := htlcswitch.HtlcKey{
		IncomingCircuit: channeldb.CircuitKey{
			ChanID: lnwire.NewShortChanIDFromInt(
				event.IncomingChannelId,
			),
			HtlcID: event.IncomingHtlcId,
		},
		OutgoingCircuit: channeldb.CircuitKey{
			ChanID: lnwire.NewShortChanIDFromInt(
				event.OutgoingChannelId,
			),
			HtlcID: event.OutgoingHtlcId,
		},
	}

	switch event.Event.(type) {
	// If we have received a forwarding event, we add it to our map if it
	// is not already present. We are ok with duplicate events, because
	// htlcs are sometimes replayed by the switch, but we want to keep our
	// earliest timestamp for stats.
	case *routerrpc.HtlcEvent_ForwardEvent:
		if _, ok := h.activeHtlcs[key]; ok {
			htlcLogger.Infof("htlc: %v replayed", key)
			return nil
		}

		// Add to our set of known active htlcs.
		h.activeHtlcs[key] = struct{}{}

	case *routerrpc.HtlcEvent_SettleEvent:
		err := h.recordResolution(key, event.EventType, true)
		if err != nil {
			return err
		}

	case *routerrpc.HtlcEvent_ForwardFailEvent:
		err := h.recordResolution(key, event.EventType, false)
		if err != nil {
			return err
		}

	case *routerrpc.HtlcEvent_LinkFailEvent:
		err := h.recordResolution(key, event.EventType, false)
		if err != nil {
			return err
		}

	default:
		return fmt.Errorf("unknown htlc event type: %T", event)
	}

	return nil
}

// recordResolution records the outcome of a htlc resolution (settle/fail) in
// our metrics.
func (h *htlcMonitor) recordResolution(key htlcswitch.HtlcKey,
	eventType routerrpc.HtlcEvent_EventType, success bool) error {

	// Create the set of labels we want to track this resolution.
	labels := map[string]string{
		outcomeLabel: outcomeFailedValue,
		chanInLabel: strconv.FormatUint(
			key.IncomingCircuit.ChanID.ToUint64(), 10,
		),
		chanOutLabel: strconv.FormatUint(
			key.OutgoingCircuit.ChanID.ToUint64(), 10,
		),
	}
	if success {
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
	_, ok := h.activeHtlcs[key]
	if !ok {
		htlcLogger.Infof("resolved htlc: %v original forward "+
			"not found", key)

		return nil
	}

	// Delete the htlc from our set of active htlcs.
	delete(h.activeHtlcs, key)
	return nil
}
