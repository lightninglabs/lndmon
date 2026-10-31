package collectors

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fixedScrapeCollector is a scrapeCollector whose Collect immediately returns a
// fixed error, used to test outcome classification.
type fixedScrapeCollector struct {
	name string
	err  error
	desc *prometheus.Desc
}

func newFixedScrapeCollector(name string, err error) *fixedScrapeCollector {
	return &fixedScrapeCollector{
		name: name,
		err:  err,
		desc: prometheus.NewDesc("test_fixed_"+name, "", nil, nil),
	}
}

func (c *fixedScrapeCollector) Name() string { return c.name }

func (c *fixedScrapeCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

func (c *fixedScrapeCollector) Collect(_ context.Context,
	_ chan<- prometheus.Metric) error {

	return c.err
}

// blockingScrapeCollector blocks in Collect until its context is canceled, then
// returns the context error. It is used to test that a scrape's cancellation
// propagates into a collector.
type blockingScrapeCollector struct {
	started chan struct{}
	desc    *prometheus.Desc
}

func newBlockingScrapeCollector() *blockingScrapeCollector {
	return &blockingScrapeCollector{
		started: make(chan struct{}),
		desc:    prometheus.NewDesc("test_blocking", "", nil, nil),
	}
}

func (c *blockingScrapeCollector) Name() string { return "blocking" }

func (c *blockingScrapeCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

func (c *blockingScrapeCollector) Collect(ctx context.Context,
	_ chan<- prometheus.Metric) error {

	close(c.started)
	<-ctx.Done()

	return ctx.Err()
}

// TestBoundCollectorStatus checks that boundCollector classifies the error a
// collector returns into the right status.
func TestBoundCollectorStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want collectorStatus
	}{
		{"ok", nil, statusOK},
		{"ctx-canceled", context.Canceled, statusCanceled},
		{"ctx-deadline", context.DeadlineExceeded, statusLndTimeout},
		{
			"grpc-canceled",
			status.Error(codes.Canceled, "canceled"),
			statusCanceled,
		},
		{
			"grpc-deadline",
			status.Error(codes.DeadlineExceeded, "deadline"),
			statusLndTimeout,
		},
		{"real-error", fmt.Errorf("boom"), statusError},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			res := &collectorResult{}
			b := &boundCollector{
				sc:     newFixedScrapeCollector(tc.name, tc.err),
				ctx:    context.Background(),
				result: res,
			}

			ch := make(chan prometheus.Metric, 8)
			b.Collect(ch)

			if res.status != tc.want {
				t.Fatalf("err=%v: got status %d, want %d",
					tc.err, res.status, tc.want)
			}
		})
	}
}

// TestBoundCollectorCancellation checks that canceling the bound context while
// a collector is running surfaces as statusCanceled.
func TestBoundCollectorCancellation(t *testing.T) {
	sc := newBlockingScrapeCollector()
	res := &collectorResult{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg := prometheus.NewRegistry()
	reg.MustRegister(&boundCollector{sc: sc, ctx: ctx, result: res})

	gatherDone := make(chan struct{})
	go func() {
		_, _ = reg.Gather()
		close(gatherDone)
	}()

	// Wait for the collector to start, then cancel the scrape.
	select {
	case <-sc.started:
	case <-time.After(5 * time.Second):
		t.Fatal("collector did not start")
	}
	cancel()

	select {
	case <-gatherDone:
	case <-time.After(5 * time.Second):
		t.Fatal("gather did not return after cancel")
	}

	if res.status != statusCanceled {
		t.Fatalf("got status %d, want statusCanceled (%d)",
			res.status, statusCanceled)
	}
}

// TestScrapeOutcome checks the branches of the per-scrape outcome logic and the
// breakdown notation.
func TestScrapeOutcome(t *testing.T) {
	ok := &collectorResult{
		name: "info", elapsed: 5 * time.Millisecond, status: statusOK,
	}
	canceled := &collectorResult{
		name: "graph", elapsed: 10 * time.Second, status: statusCanceled,
	}
	lndTimeout := &collectorResult{
		name: "graph", elapsed: 30 * time.Second, status: statusLndTimeout,
	}
	collectorErr := &collectorResult{
		name: "chain", elapsed: time.Second, status: statusError,
	}

	// A clean, fully-successful scrape must be silent.
	if logit, _ := scrapeOutcome(
		"x", 20*time.Millisecond, false, false, 0,
		[]*collectorResult{ok},
	); logit {
		t.Fatal("clean scrape should not be logged")
	}

	// An abandoned scrape (a collector canceled) is logged, uses the floor
	// notation, and takes priority over everything else.
	logit, msg := scrapeOutcome(
		"1.2.3.4", 10*time.Second, true, true, time.Second,
		[]*collectorResult{canceled, ok},
	)
	if !logit {
		t.Fatal("abandoned scrape should be logged")
	}
	if !strings.Contains(msg, "abandoned") ||
		!strings.Contains(msg, "disconnected during lnd RPC gather") {

		t.Fatalf("unexpected abandoned message: %s", msg)
	}
	if !strings.Contains(msg, "graph=>10.000s") {
		t.Fatalf("expected floor notation graph=>10.000s in: %s", msg)
	}
	if !strings.Contains(msg, "graph=>10.000s, info=0.005s") {
		t.Fatalf("expected comma-joined breakdown in: %s", msg)
	}
	if !strings.Contains(msg, "after 10.000s") {
		t.Fatalf("expected top-level duration after 10.000s in: %s", msg)
	}

	// A write failure with no cancellation is reported as a network blip.
	logit, msg = scrapeOutcome(
		"x", time.Second, false, true, 500*time.Millisecond,
		[]*collectorResult{ok},
	)
	if !logit || !strings.Contains(msg, "network blip") {
		t.Fatalf("expected network blip message, got: %s", msg)
	}
	if !strings.Contains(msg, "failed after 0.500s") {
		t.Fatalf("expected failedAt 0.500s in: %s", msg)
	}

	// An lnd RPC timeout with no disconnect is reported as a partial scrape,
	// with the exact (non-floor) time and the lnd-timeout marker.
	logit, msg = scrapeOutcome(
		"x", 30*time.Second, false, false, 0,
		[]*collectorResult{lndTimeout, ok},
	)
	if !logit || !strings.Contains(msg, "partial") {
		t.Fatalf("expected partial message, got: %s", msg)
	}
	if !strings.Contains(msg, "graph=30.000s(lnd-timeout)") {
		t.Fatalf("expected graph=30.000s(lnd-timeout) in: %s", msg)
	}

	// A real collector error is also a partial scrape and carries the error
	// marker.
	logit, msg = scrapeOutcome(
		"x", time.Second, false, false, 0,
		[]*collectorResult{collectorErr},
	)
	if !logit || !strings.Contains(msg, "chain=1.000s(error)") {
		t.Fatalf("expected chain=1.000s(error) in: %s", msg)
	}

	// A disconnect with no canceled collector, no write failure, and no
	// partial data still reports the client leaving.
	logit, msg = scrapeOutcome(
		"x", 2*time.Second, true, false, 0,
		[]*collectorResult{ok},
	)
	if !logit ||
		!strings.Contains(msg, "Prometheus HTTP client disconnected") {

		t.Fatalf("expected client disconnected message, got: %s", msg)
	}

	// DR4: a disconnect during the response WRITE (gather finished, nothing
	// canceled, but the write to the gone client failed) must be reported as a
	// client disconnect, NOT as a network blip. This pins the
	// disconnected-before-writeFailed ordering; the old order mislabeled it.
	logit, msg = scrapeOutcome(
		"x", 2*time.Second, true, true, 500*time.Millisecond,
		[]*collectorResult{ok},
	)
	if !logit || !strings.Contains(msg, "client disconnected") {
		t.Fatalf("disconnect-during-write should report a client "+
			"disconnect, got: %s", msg)
	}
	if strings.Contains(msg, "network blip") {
		t.Fatalf("disconnect-during-write must not be labeled a network "+
			"blip, got: %s", msg)
	}

	// DR9: a partial scrape (an lnd timeout) whose response write also fails,
	// with no disconnect, must be reported as partial, NOT as a network blip.
	// This pins the anyProblem-before-writeFailed ordering.
	logit, msg = scrapeOutcome(
		"x", 30*time.Second, false, true, 500*time.Millisecond,
		[]*collectorResult{lndTimeout, ok},
	)
	if !logit || !strings.Contains(msg, "partial") {
		t.Fatalf("partial-with-write-failure should report partial, "+
			"got: %s", msg)
	}
	if strings.Contains(msg, "network blip") {
		t.Fatalf("partial-with-write-failure must not be labeled a network "+
			"blip, got: %s", msg)
	}
}
