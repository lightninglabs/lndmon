package collectors

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btclog/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// discardLogger is a std logger that drops everything, used as the promhttp
// ErrorLog in tests.
func discardLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

// TestBrokenPipeFilter checks that the promhttp ErrorLog filter drops the
// per-metric-family write-failure spam while letting genuine gather errors
// through.
func TestBrokenPipeFilter(t *testing.T) {
	var buf bytes.Buffer
	f := brokenPipeFilter{w: &buf}

	// Write-failure lines (one per metric family on an abandoned scrape) must
	// be dropped, and the filter must report them as fully written so the
	// logger does not treat the drop as an error.
	for _, line := range []string{
		"error encoding and sending metric family: write tcp " +
			"127.0.0.1:8990->127.0.0.1:33398: write: broken pipe\n",
		"write tcp ...: write: connection reset by peer\n",
	} {
		n, err := f.Write([]byte(line))
		if err != nil {
			t.Fatalf("Write(%q) returned error: %v", line, err)
		}
		if n != len(line) {
			t.Fatalf("Write(%q) = %d, want %d", line, n, len(line))
		}
	}
	if buf.Len() != 0 {
		t.Fatalf("broken-pipe lines were not dropped: %q", buf.String())
	}

	// A genuine gather error must pass through unchanged.
	real := "error gathering metrics: collected metric ... was collected " +
		"before with the same name and label values\n"
	if _, err := f.Write([]byte(real)); err != nil {
		t.Fatalf("Write(real) returned error: %v", err)
	}
	if buf.String() != real {
		t.Fatalf("genuine error not forwarded: got %q, want %q",
			buf.String(), real)
	}
}

// TestIsCanceled mirrors TestIsDeadlineExceeded for the new IsCanceled helper,
// covering bare, wrapped, and gRPC-status forms, and confirming unrelated codes
// (notably Unavailable and DeadlineExceeded) are not treated as canceled.
func TestIsCanceled(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"bare context.Canceled", context.Canceled, true},
		{
			"wrapped context.Canceled",
			fmt.Errorf("collect: %w", context.Canceled), true,
		},
		{
			"grpc Canceled",
			status.Error(codes.Canceled, "context canceled"), true,
		},
		{
			"grpc DeadlineExceeded",
			status.Error(codes.DeadlineExceeded, "deadline"), false,
		},
		{"bare context.DeadlineExceeded", context.DeadlineExceeded, false},
		{
			"grpc Unavailable",
			status.Error(codes.Unavailable, "transport is closing"), false,
		},
		{"unrelated", errors.New("boom"), false},
	}

	for _, tt := range tests {
		if got := IsCanceled(tt.err); got != tt.want {
			t.Errorf("%s: IsCanceled(%v) = %v, want %v",
				tt.name, tt.err, got, tt.want)
		}
	}
}

// TestServeScrapeErrorRouting drives serveScrape end to end and asserts that
// only a real collector error (and only when the scraper did NOT disconnect) is
// forwarded to the error channel that triggers an lndmon restart.
func TestServeScrapeErrorRouting(t *testing.T) {
	Logger = btclog.Disabled

	tests := []struct {
		name string

		// err is the error the single collector returns.
		err error

		// canceledReq makes the request context already canceled, simulating a
		// scraper that disconnected during the scrape.
		canceledReq bool

		// wantForward is whether the error should be forwarded to errChan.
		wantForward bool
	}{
		{
			name:        "real error forwarded",
			err:         errors.New("boom"),
			wantForward: true,
		},
		{
			name:        "canceled error not forwarded",
			err:         context.Canceled,
			wantForward: false,
		},
		{
			name:        "lnd timeout not forwarded",
			err:         status.Error(codes.DeadlineExceeded, "deadline"),
			wantForward: false,
		},
		{
			// DR1: a disconnect-induced error that does not carry a Canceled
			// code (here Unavailable) must not restart lndmon.
			name:        "real error during disconnect not forwarded",
			err:         status.Error(codes.Unavailable, "transport closing"),
			canceledReq: true,
			wantForward: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errChan := make(chan error, 8)
			p := &PrometheusExporter{
				scrapeCollectors: []scrapeCollector{
					newFixedScrapeCollector("x", tt.err),
				},
				errChan: errChan,
			}

			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			if tt.canceledReq {
				ctx, cancel := context.WithCancel(req.Context())
				cancel()
				req = req.WithContext(ctx)
			}

			p.serveScrape(httptest.NewRecorder(), req, discardLogger())

			select {
			case <-errChan:
				if !tt.wantForward {
					t.Fatal("error was forwarded to errChan but " +
						"should not have been")
				}
			default:
				if tt.wantForward {
					t.Fatal("expected error on errChan, got none")
				}
			}
		})
	}
}

// TestServeScrapeCancellationPropagates confirms that canceling the request
// context (a disconnecting scraper) unblocks an in-flight collector through the
// threaded context and that the resulting canceled outcome is not forwarded to
// the restart channel.
func TestServeScrapeCancellationPropagates(t *testing.T) {
	Logger = btclog.Disabled

	errChan := make(chan error, 8)
	c := newBlockingScrapeCollector()
	p := &PrometheusExporter{
		scrapeCollectors: []scrapeCollector{c},
		errChan:          errChan,
	}

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil).WithContext(ctx)

	done := make(chan struct{})
	go func() {
		p.serveScrape(httptest.NewRecorder(), req, discardLogger())
		close(done)
	}()

	// Wait until the collector is actually blocked inside Collect, then cancel
	// the request context the way a disconnecting scraper would.
	select {
	case <-c.started:
	case <-time.After(5 * time.Second):
		t.Fatal("collector never started")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("serveScrape did not return after the request context was " +
			"canceled; cancellation did not propagate into the collector")
	}

	// A canceled collector is a benign abandon and must not restart lndmon.
	select {
	case err := <-errChan:
		t.Fatalf("canceled scrape forwarded to errChan: %v", err)
	default:
	}
}

// TestServeScrapeLogsRealError pins the DR8 behavior: a real collector error on
// a scrape the client did NOT abandon is logged at scrape time (not only when
// the process later exits via the error channel).
func TestServeScrapeLogsRealError(t *testing.T) {
	var buf bytes.Buffer
	prev := Logger
	Logger = btclog.NewSLogger(btclog.NewDefaultHandler(&buf))
	Logger.SetLevel(btclog.LevelDebug)
	defer func() { Logger = prev }()

	errChan := make(chan error, 8)
	p := &PrometheusExporter{
		scrapeCollectors: []scrapeCollector{
			newFixedScrapeCollector(
				"x", errors.New("distinct-collector-error"),
			),
		},
		errChan: errChan,
	}

	p.serveScrape(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/metrics", nil),
		discardLogger(),
	)

	if !strings.Contains(buf.String(), "distinct-collector-error") {
		t.Fatalf("real collector error was not logged at scrape time; "+
			"log was: %q", buf.String())
	}
}
