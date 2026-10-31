package collectors

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// scrapeCollector is a context-aware metrics collector. Unlike
// prometheus.Collector, its Collect takes the scrape request's context so that
// a canceled scrape (the scraper disconnecting) cancels the in-flight lnd gRPC
// queries, and it returns the error it hit so the handler can report a
// per-scrape outcome.
type scrapeCollector interface {
	// Name is a short identifier used in the scrape outcome log.
	Name() string

	// Describe sends the collector's metric descriptors, like
	// prometheus.Collector.Describe.
	Describe(ch chan<- *prometheus.Desc)

	// Collect gathers metrics using the provided context. A canceled context
	// means the scrape was abandoned; the returned error is surfaced by the
	// handler (and, if it is a real error, used to restart lndmon).
	Collect(ctx context.Context, ch chan<- prometheus.Metric) error
}

// collectorStatus is the outcome of a single collector during one scrape.
type collectorStatus int

const (
	// statusOK means the collector finished normally.
	statusOK collectorStatus = iota

	// statusCanceled means the scrape was abandoned (the request context was
	// canceled, i.e. the scraper disconnected) while this collector was still
	// running. Its elapsed time is a floor, not the true duration.
	statusCanceled

	// statusLndTimeout means the collector's lnd call hit the lnd RPC timeout
	// (--lnd.rpctimeout) rather than being canceled. Its elapsed time is the
	// full RPC timeout.
	statusLndTimeout

	// statusError means the collector hit a real error, which the handler
	// forwards to the error channel to restart lndmon.
	statusError
)

// collectorResult records how a single collector fared during one scrape.
type collectorResult struct {
	name    string
	elapsed time.Duration
	status  collectorStatus
	err     error
}

// boundCollector adapts a context-aware scrapeCollector to the stock
// prometheus.Collector interface by binding it to a single scrape's context
// and recording its timing and outcome. A fresh boundCollector is created per
// scrape, so concurrent scrapes never share mutable state.
type boundCollector struct {
	sc     scrapeCollector
	ctx    context.Context
	result *collectorResult
}

// Describe forwards to the wrapped collector.
//
// NOTE: Part of the prometheus.Collector interface.
func (b *boundCollector) Describe(ch chan<- *prometheus.Desc) {
	b.sc.Describe(ch)
}

// Collect runs the wrapped collector with the bound context, timing it and
// classifying the outcome into the result.
//
// NOTE: Part of the prometheus.Collector interface.
func (b *boundCollector) Collect(ch chan<- prometheus.Metric) {
	start := time.Now()
	err := b.sc.Collect(b.ctx, ch)

	status := statusOK
	switch {
	case err == nil:
		status = statusOK

	// A canceled context means the scrape was abandoned mid-gather. Check
	// this before the lnd timeout, since an abandoned scrape cancels the call
	// before it can reach its own deadline. Any Canceled error (whether the
	// scraper disconnected or lnd itself canceled the call, e.g. during its
	// own shutdown) is treated as benign and never restarts lndmon; a
	// persistent lnd failure instead surfaces as a non-Canceled error that the
	// handler forwards to the restart path.
	case IsCanceled(err):
		status = statusCanceled

	// A deadline exceeded means the lnd call reached the lnd RPC timeout.
	case IsDeadlineExceeded(err):
		status = statusLndTimeout

	default:
		status = statusError
	}

	*b.result = collectorResult{
		name:    b.sc.Name(),
		elapsed: time.Since(start),
		status:  status,
		err:     err,
	}
}

// formatSeconds renders a duration as seconds with millisecond precision
// (three decimal places), so every entry in a breakdown carries the same
// number of decimal places, e.g. "0.020s" or "9.995s".
func formatSeconds(d time.Duration) string {
	ms := d.Milliseconds()
	return fmt.Sprintf("%d.%03ds", ms/1000, ms%1000)
}

// formatBreakdown renders the collectors as a comma-separated "name=seconds"
// list, each time formatted by formatSeconds so every entry has the same
// number of decimal places. A status marker is added: a ">" floor prefix for
// canceled collectors (we cut them short, so their time is only a lower
// bound), a "(lnd-timeout)" suffix for a collector that ran to the lnd RPC
// timeout, and "(error)" for a real error.
func formatBreakdown(results []*collectorResult) string {
	parts := make([]string, 0, len(results))
	for _, r := range results {
		if r == nil {
			continue
		}

		secs := formatSeconds(r.elapsed)
		switch r.status {
		case statusCanceled:
			parts = append(parts, fmt.Sprintf("%s=>%s", r.name, secs))

		case statusLndTimeout:
			parts = append(parts, fmt.Sprintf(
				"%s=%s(lnd-timeout)", r.name, secs,
			))

		case statusError:
			parts = append(parts, fmt.Sprintf(
				"%s=%s(error)", r.name, secs,
			))

		default:
			parts = append(parts, fmt.Sprintf("%s=%s", r.name, secs))
		}
	}

	return strings.Join(parts, ", ")
}

// scrapeOutcome decides whether (and what) to log for a completed scrape. It
// never logs a clean, fully-successful scrape. For a problem scrape it returns
// the single most relevant line with a per-collector breakdown.
func scrapeOutcome(remoteAddr string, elapsed time.Duration, disconnected,
	writeFailed bool, failedAt time.Duration,
	results []*collectorResult) (bool, string) {

	var anyCanceled, anyProblem bool
	for _, r := range results {
		if r == nil {
			continue
		}
		switch r.status {
		case statusCanceled:
			anyCanceled = true
			anyProblem = true
		case statusLndTimeout, statusError:
			anyProblem = true
		}
	}

	breakdown := formatBreakdown(results)
	elapsedStr := formatSeconds(elapsed)

	switch {
	// The scrape was abandoned mid-gather: at least one collector was cut
	// short by the canceled context. This is the common slow-lnd case (e.g.
	// GetInfo stalling its callers past the Prometheus scrape_timeout), and
	// canceling here stops lndmon from hammering an already-slow lnd.
	case anyCanceled:
		return true, fmt.Sprintf("Prometheus scrape from %s abandoned "+
			"after %s (Prometheus HTTP client disconnected "+
			"during lnd RPC gather). collectors: %s", remoteAddr,
			elapsedStr, breakdown)

	// The client disconnected, but the gather had already completed (no
	// collector was canceled). The response write may also have failed as a
	// direct result of that disconnect, so this is checked before writeFailed:
	// a client that walked away is an abandon, not a network blip.
	case disconnected:
		return true, fmt.Sprintf("Prometheus scrape from %s abandoned "+
			"after %s (Prometheus HTTP client disconnected). "+
			"collectors: %s", remoteAddr, elapsedStr, breakdown)

	// No disconnect, but a collector hit its lnd RPC timeout or errored, so
	// Prometheus received a partial scrape. --lnd.rpctimeout is the ceiling
	// here; raise it (to at least the Prometheus scrape_timeout) for full
	// data on slow nodes. Checked before writeFailed so a slow-lnd partial is
	// not misattributed to the network if the write also happens to fail.
	case anyProblem:
		return true, fmt.Sprintf("Prometheus scrape to %s served partial "+
			"in %s. collectors: %s", remoteAddr, elapsedStr, breakdown)

	// The gather finished cleanly and the client did not disconnect, yet the
	// response write failed: the connection broke mid-response, which with no
	// disconnect and no collector problem is most often a genuine network
	// blip.
	case writeFailed:
		return true, fmt.Sprintf("Prometheus scrape to %s failed after %s "+
			"(connection broke mid-response - network blip?). "+
			"collectors: %s", remoteAddr,
			formatSeconds(failedAt), breakdown)

	default:
		return false, ""
	}
}

// brokenPipeFilter is an io.Writer that drops promhttp log lines about write
// failures (broken pipe / connection reset). Those are an expected side-effect
// of a scraper disconnecting, and promhttp logs one per metric family (dozens
// per abandoned scrape); the scrape outcome log reports the disconnect once,
// cleanly. Genuine gather errors are passed through.
type brokenPipeFilter struct {
	w io.Writer
}

// Write drops write-failure lines and forwards everything else.
func (f brokenPipeFilter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("broken pipe")) ||
		bytes.Contains(p, []byte("connection reset")) {

		// Report success so the logger doesn't treat the drop as a write
		// error.
		return len(p), nil
	}

	return f.w.Write(p)
}
