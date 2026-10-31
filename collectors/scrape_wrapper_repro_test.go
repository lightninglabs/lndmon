package collectors

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// TestScrapeWrapperRepro checks whether the scrapeWriter wrapper used in
// Start() itself breaks metric serving, compared to serving the plain
// promhttp handler. It exercises both a plain GET and a gzip-accepting GET
// (mimicking Prometheus) against real httptest servers.
func TestScrapeWrapperRepro(t *testing.T) {
	// 1. Fresh registry with a known metric set to 42.
	reg := prometheus.NewRegistry()
	g := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "lnd_repro_metric",
		Help: "repro metric",
	})
	g.Set(42)
	if err := reg.Register(g); err != nil {
		t.Fatalf("register gauge: %v", err)
	}

	// 2. H_plain: the instrumented metric handler, exactly as Start()
	// builds it (minus the ErrorLog, which is irrelevant to serving).
	hPlain := promhttp.InstrumentMetricHandler(
		reg,
		promhttp.HandlerFor(reg, promhttp.HandlerOpts{
			ErrorHandling: promhttp.ContinueOnError,
		}),
	)

	// 3. H_wrapped: wrap H_plain exactly like Start() does, using the REAL
	// unexported scrapeWriter type from this package.
	hWrapped := http.HandlerFunc(func(w http.ResponseWriter,
		r *http.Request) {

		sw := &scrapeWriter{
			ResponseWriter: w,
			start:          time.Now(),
		}
		hPlain.ServeHTTP(sw, r)
	})

	// 4. Serve each with a real httptest server.
	plainSrv := httptest.NewServer(hPlain)
	defer plainSrv.Close()
	wrappedSrv := httptest.NewServer(hWrapped)
	defer wrappedSrv.Close()

	const want = "lnd_repro_metric 42"

	// doGet performs a GET (optionally accepting gzip) and returns the
	// status code, decoded body, and whether the body was gzip-encoded.
	doGet := func(url string, gz bool) (int, string, bool, error) {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return 0, "", false, err
		}
		if gz {
			req.Header.Set("Accept-Encoding", "gzip")
		}

		// Prevent the transport from transparently decompressing so we
		// exercise the real gzip path the way Prometheus would.
		client := &http.Client{
			Transport: &http.Transport{DisableCompression: true},
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, "", false, err
		}
		defer resp.Body.Close()

		var reader io.Reader = resp.Body
		wasGzip := resp.Header.Get("Content-Encoding") == "gzip"
		if wasGzip {
			zr, err := gzip.NewReader(resp.Body)
			if err != nil {
				return resp.StatusCode, "", true, err
			}
			defer zr.Close()
			reader = zr
		}

		body, err := io.ReadAll(reader)
		if err != nil {
			return resp.StatusCode, "", wasGzip, err
		}

		return resp.StatusCode, string(body), wasGzip, nil
	}

	type result struct {
		label     string
		status    int
		bodyLen   int
		hasMetric bool
		wasGzip   bool
	}

	run := func(label, url string, gz bool) result {
		status, body, wasGzip, err := doGet(url, gz)
		if err != nil {
			t.Fatalf("%s: request failed: %v", label, err)
		}
		return result{
			label:     label,
			status:    status,
			bodyLen:   len(body),
			hasMetric: strings.Contains(body, want),
			wasGzip:   wasGzip,
		}
	}

	plainNoGz := run("PLAIN / no-gzip", plainSrv.URL, false)
	plainGz := run("PLAIN / gzip", plainSrv.URL, true)
	wrappedNoGz := run("WRAPPED / no-gzip", wrappedSrv.URL, false)
	wrappedGz := run("WRAPPED / gzip", wrappedSrv.URL, true)

	all := []result{plainNoGz, plainGz, wrappedNoGz, wrappedGz}

	fmt.Printf("\n===== SCRAPE WRAPPER REPRO RESULTS =====\n")
	fmt.Printf("%-22s %-7s %-9s %-9s %-9s\n",
		"case", "status", "bodyLen", "gzip", "hasMetric")
	for _, r := range all {
		fmt.Printf("%-22s %-7d %-9d %-9v %-9v\n",
			r.label, r.status, r.bodyLen, r.wasGzip, r.hasMetric)
	}
	fmt.Printf("========================================\n")

	// Decisive verdict lines.
	fmt.Printf("VERDICT plainServesMetric(no-gzip)=%v gzip=%v\n",
		plainNoGz.hasMetric, plainGz.hasMetric)
	fmt.Printf("VERDICT wrappedServesMetric(no-gzip)=%v gzip=%v\n",
		wrappedNoGz.hasMetric, wrappedGz.hasMetric)

	plainOK := plainNoGz.hasMetric && plainGz.hasMetric
	wrappedOK := wrappedNoGz.hasMetric && wrappedGz.hasMetric
	wrapperBreaks := plainOK && !wrappedOK
	fmt.Printf("VERDICT plainOK=%v wrappedOK=%v wrapperBreaksOutput=%v\n",
		plainOK, wrappedOK, wrapperBreaks)

	if !wrappedOK {
		t.Errorf("wrapped handler did not serve the metric in all "+
			"cases: no-gzip=%v gzip=%v", wrappedNoGz.hasMetric,
			wrappedGz.hasMetric)
	}
}
