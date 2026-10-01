package collectors

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// erroringResponseWriter is an http.ResponseWriter whose Write always fails,
// used to simulate a scraper that has closed the connection (broken pipe).
type erroringResponseWriter struct {
	header http.Header
	err    error
}

func (w *erroringResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *erroringResponseWriter) WriteHeader(int) {}

func (w *erroringResponseWriter) Write(b []byte) (int, error) {
	return 0, w.err
}

// TestScrapeWriterDetectsFailedWrite verifies the core of the fix: a write that
// fails (as it would when the scraper has closed the connection) flips
// failed=true and records failedAt exactly once, while a successful write
// leaves failed=false.
func TestScrapeWriterDetectsFailedWrite(t *testing.T) {
	// A successful write must not mark the scrape as failed.
	okWriter := httptest.NewRecorder()
	swOK := &scrapeWriter{ResponseWriter: okWriter, start: time.Now()}
	if _, err := swOK.Write([]byte("ok")); err != nil {
		t.Fatalf("unexpected error on successful write: %v", err)
	}
	if swOK.failed {
		t.Fatalf("failed should be false after a successful write")
	}

	// A failing write must mark the scrape as failed and record the elapsed
	// time. start is put in the past so failedAt is unambiguously positive.
	ew := &erroringResponseWriter{err: errors.New("write: broken pipe")}
	sw := &scrapeWriter{
		ResponseWriter: ew,
		start:          time.Now().Add(-50 * time.Millisecond),
	}
	if _, err := sw.Write([]byte("metrics")); err == nil {
		t.Fatalf("expected the write error to propagate")
	}
	if !sw.failed {
		t.Fatalf("expected failed=true after a failing write")
	}
	if sw.failedAt <= 0 {
		t.Fatalf("expected failedAt>0, got %v", sw.failedAt)
	}

	// failedAt is recorded only on the first failure, not overwritten.
	firstAt := sw.failedAt
	if _, err := sw.Write([]byte("more")); err == nil {
		t.Fatalf("expected the second write error to propagate")
	}
	if sw.failedAt != firstAt {
		t.Fatalf("failedAt must be recorded once; changed from %v to %v",
			firstAt, sw.failedAt)
	}

	t.Logf("detected abandoned scrape: failed=%v failedAt=%v",
		sw.failed, sw.failedAt)
}

// slowLargeCollector mimics a slow lnd gather that produces a large response:
// Collect blocks for delay, then emits count series. The delay lets the client
// give up first; the large body guarantees the eventual server-side write hits
// the closed connection (EPIPE) rather than fitting entirely in the socket
// send buffer.
type slowLargeCollector struct {
	delay time.Duration
	count int
	desc  *prometheus.Desc
}

func newSlowLargeCollector(delay time.Duration, count int) *slowLargeCollector {
	return &slowLargeCollector{
		delay: delay,
		count: count,
		desc: prometheus.NewDesc(
			"lnd_slow_repro_metric", "repro", []string{"id"}, nil,
		),
	}
}

func (c *slowLargeCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

func (c *slowLargeCollector) Collect(ch chan<- prometheus.Metric) {
	time.Sleep(c.delay)
	for i := 0; i < c.count; i++ {
		ch <- prometheus.MustNewConstMetric(
			c.desc, prometheus.GaugeValue, float64(i), strconv.Itoa(i),
		)
	}
}

// TestScrapeWriterAbandonedScrape reproduces the real broken-pipe scenario end
// to end: a slow gather plus a client that gives up before it completes (the
// curl -m / scrape_timeout case). It asserts the scrapeWriter detects the
// failed write, which is exactly what drives the new "abandoned after" log.
func TestScrapeWriterAbandonedScrape(t *testing.T) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(newSlowLargeCollector(300*time.Millisecond, 50000))

	promHandler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
	})

	// Wrap exactly like Start() does, capturing the scrapeWriter so the test
	// can inspect whether the abandonment was detected.
	resultCh := make(chan *scrapeWriter, 1)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &scrapeWriter{ResponseWriter: w, start: time.Now()}
		promHandler.ServeHTTP(sw, r)
		resultCh <- sw
	})

	srv := httptest.NewServer(h)
	defer srv.Close()

	// Client gives up at 30ms, long before the 300ms gather completes, and
	// closes the connection (mimics curl -m / Prometheus scrape_timeout).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
		t.Fatalf("expected the client request to be aborted, but it succeeded")
	}

	select {
	case sw := <-resultCh:
		if !sw.failed {
			t.Fatalf("expected scrapeWriter to detect the abandoned scrape " +
				"(failed write), got failed=false")
		}
		t.Logf("reproduced broken pipe: failed=%v abandoned after %v",
			sw.failed, sw.failedAt)
	case <-time.After(10 * time.Second):
		t.Fatal("handler did not return after client disconnect")
	}
}
