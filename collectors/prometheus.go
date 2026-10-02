package collectors

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// log configuration defaults.
	defaultLogFilename = "lndmon.log"
	defaultLogFileSize = 10
	defaultMaxLogFile  = 10
	defaultLndmonDir   = btcutil.AppDataDir("lndmon", false)
)

// PrometheusExporter is a metric exporter that exports relevant lnd metrics
// such as routing policies to track how Lightning fees change over time.
type PrometheusExporter struct {
	cfg *PrometheusConfig

	lnd *lndclient.LndServices

	monitoringCfg *MonitoringConfig

	htlcMonitor     *htlcMonitor
	paymentsMonitor *paymentsMonitor

	// scrapeCollectors is the set of context-aware collectors that query lnd
	// on each scrape. They are registered into a fresh per-request registry
	// so the scrape's context can be threaded into their lnd calls.
	scrapeCollectors []scrapeCollector

	// eventCollectors is the set of collectors that are updated by background
	// subscriptions (htlcs, payments) rather than by querying lnd on scrape.
	// They are registered once with the default registry.
	eventCollectors []prometheus.Collector

	// errChan is an error channel that we receive errors from our collectors
	// on, and that the scrape handler sends real collector errors into.
	errChan chan error
}

// PrometheusConfig is the set of configuration data that specifies the
// listening address of the Prometheus server and configuration for lndmon logs.
type PrometheusConfig struct {
	// ListenAddr is the listening address that we should use to allow the
	// main Prometheus server to scrape our metrics.
	ListenAddr string `long:"listenaddr" description:"the interface we should listen on for prometheus"`

	// LogDir is the directory to log lndmon output.
	LogDir string `long:"logdir" description:"Directory to log output"`

	// MaxLogFiles is the maximum number of log files to keep (0 for no
	// rotation).
	MaxLogFiles int `long:"maxlogfiles" description:"Maximum log files to keep (0 for no rotation)"`

	// MaxLogFileSize is the maximum log file size in MB.
	MaxLogFileSize int `long:"maxlogfilesize" description:"Maximum log file size in MB"`
}

// MonitoringConfig contains information that specifies how to monitor the node.
type MonitoringConfig struct {
	// PrimaryNode is the pubkey of the primary node in primary-gateway
	// setups.
	PrimaryNode *route.Vertex

	// DisableGraph disables collection of graph metrics
	DisableGraph bool

	// DisableHtlc disables collection of HTLCs metrics
	DisableHtlc bool

	// DisablePayments disables collection of payment metrics
	DisablePayments bool

	// ProgramStartTime stores a best-effort estimate of when lnd/lndmon was
	// started.
	ProgramStartTime time.Time
}

func DefaultConfig() *PrometheusConfig {
	return &PrometheusConfig{
		ListenAddr:     "localhost:9092",
		LogDir:         filepath.Join(defaultLndmonDir, "logs"),
		MaxLogFiles:    3,
		MaxLogFileSize: 10,
	}
}

// NewPrometheusExporter makes a new instance of the PrometheusExporter given
// the address to listen for Prometheus on and an lnd gRPC client.
func NewPrometheusExporter(cfg *PrometheusConfig, lnd *lndclient.LndServices,
	monitoringCfg *MonitoringConfig,
	quitChan chan struct{}) (*PrometheusExporter, error) {

	// Initialize logging before constructing any collectors. Some collectors
	// launch background goroutines that log on failure, so the package-global
	// Logger must be ready beforehand. A failure here is fatal: we return the
	// error so lndmon exits instead of running with collectors that would
	// dereference a nil Logger.
	if err := initLogRotator(
		filepath.Join(cfg.LogDir, defaultLogFilename),
		defaultLogFileSize, defaultMaxLogFile,
	); err != nil {
		return nil, err
	}

	// Our scrape collectors and the htlc and payments monitors can all send
	// errors here, so we buffer the error channel by 8 so that we do not need
	// to consume all errors from this channel (on the first one, we'll start
	// shutting down, but a few could arrive quickly in the case where lnd is
	// shutting down).
	errChan := make(chan error, 8)

	htlcMonitor := newHtlcMonitor(lnd.Router, errChan)

	// Create payments monitor.
	paymentsMonitor := newPaymentsMonitor(lnd, errChan)

	chanCollector := NewChannelsCollector(
		lnd.Client, errChan, quitChan, monitoringCfg,
	)

	// The pull collectors query lnd on each scrape. They are context-aware so
	// an abandoned scrape cancels their in-flight lnd calls.
	scrapeCollectors := []scrapeCollector{
		NewChainCollector(),
		chanCollector,
		NewWalletCollector(lnd, errChan),
		NewPeerCollector(lnd.Client, errChan),
		NewInfoCollector(),
		NewStateCollector(lnd, errChan, monitoringCfg.ProgramStartTime),
		NewWtClientCollector(lnd, errChan),
	}

	if !monitoringCfg.DisableGraph {
		scrapeCollectors = append(
			scrapeCollectors, NewGraphCollector(lnd.Client, errChan),
		)
	}

	// The event collectors are fed by background subscriptions and don't
	// query lnd on scrape, so they're registered once with the default
	// registry.
	var eventCollectors []prometheus.Collector

	if !monitoringCfg.DisableHtlc {
		eventCollectors = append(
			eventCollectors, htlcMonitor.collectors()...,
		)
	}

	if !monitoringCfg.DisablePayments {
		eventCollectors = append(
			eventCollectors, paymentsMonitor.collectors()...,
		)
	}

	return &PrometheusExporter{
		cfg:              cfg,
		lnd:              lnd,
		monitoringCfg:    monitoringCfg,
		scrapeCollectors: scrapeCollectors,
		eventCollectors:  eventCollectors,
		htlcMonitor:      htlcMonitor,
		paymentsMonitor:  paymentsMonitor,
		errChan:          errChan,
	}, nil
}

// scrapeWriter wraps the http.ResponseWriter used to serve a Prometheus
// scrape so we can record how long the scrape had been running when the
// first write to the connection failed. A deliberate scraper disconnect
// (e.g. a scrape exceeding its scrape_timeout) is detected separately via
// the request context; this recorded write failure is what lets serveScrape
// flag a write that broke without such a disconnect - most often a network
// blip - apart from a slow-lnd scrape.
type scrapeWriter struct {
	http.ResponseWriter

	// start is the time the scrape began, set before the metric handler
	// runs.
	start time.Time

	// failed is set once the first write to the connection fails.
	failed bool

	// failedAt is how long after start the first write failure occurred.
	failedAt time.Duration
}

// Write forwards to the embedded ResponseWriter, recording the elapsed time of
// the first write that fails.
func (w *scrapeWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	if err != nil && !w.failed {
		w.failed = true
		w.failedAt = time.Since(w.start)
	}

	return n, err
}

// Flush forwards to the embedded ResponseWriter's Flush if it implements
// http.Flusher, so that wrapping it does not disable any streaming the
// Prometheus handler relies on.
func (w *scrapeWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Start registers all relevant metrics with the Prometheus library, then
// launches the HTTP server that Prometheus will hit to scrape our metrics.
func (p *PrometheusExporter) Start() error {
	Logger.Info("Starting Prometheus exporter...")
	if p.lnd == nil {
		return fmt.Errorf("cannot start PrometheusExporter without " +
			"backing Lightning node to pull metrics from")
	}

	// Next, we'll attempt to register all our metrics. If we fail to
	// register ANY metric, then we'll fail all together.
	if err := p.registerMetrics(); err != nil {
		return err
	}

	// Start the htlc monitor goroutine. This will subscribe to htlcs and
	// update all of our routing-related metrics.
	if !p.monitoringCfg.DisableHtlc {
		if err := p.htlcMonitor.start(); err != nil {
			return err
		}
	}

	// Start the payment monitor goroutine. This will subscribe to receive
	// update for all payments made by lnd and update our payments related
	// metrics.
	if !p.monitoringCfg.DisablePayments {
		if err := p.paymentsMonitor.start(); err != nil {
			return err
		}
	}

	// Finally, we'll launch the HTTP server that Prometheus will use to
	// scrape our metrics.
	go func() {
		// promhttp logs a line for every metric family whose write
		// fails, so a disconnecting scraper produces one "broken pipe"
		// line per family (dozens per abandoned scrape). Filter those
		// out - serveScrape reports the disconnect once, cleanly - while
		// still surfacing genuine gather errors.
		errorLogger := log.New(
			brokenPipeFilter{w: os.Stdout}, "promhttp",
			log.Ldate|log.Ltime|log.Lshortfile,
		)

		// We gather a fresh per-request registry (so each scrape's
		// context can be bound into the collectors), which rules out
		// InstrumentMetricHandler (it registers its metrics per call).
		// Instead register the same promhttp_metric_handler_* metrics
		// once and wrap the handler with them, so existing scrapes and
		// dashboards see unchanged metric names.
		requestsTotal := prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "promhttp_metric_handler_requests_total",
				Help: "Total number of scrapes by HTTP status " +
					"code.",
			},
			[]string{"code"},
		)
		inFlight := prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "promhttp_metric_handler_requests_in_flight",
			Help: "Current number of scrapes being served.",
		})
		prometheus.MustRegister(requestsTotal, inFlight)

		// Initialize the status codes InstrumentMetricHandler reports so
		// they appear at 0 before the first scrape of each kind, matching
		// the previous handler's output.
		for _, code := range []string{"200", "500", "503"} {
			requestsTotal.WithLabelValues(code)
		}

		handler := http.HandlerFunc(func(w http.ResponseWriter,
			r *http.Request) {

			p.serveScrape(w, r, errorLogger)
		})

		http.Handle("/metrics", promhttp.InstrumentHandlerInFlight(
			inFlight,
			promhttp.InstrumentHandlerCounter(requestsTotal, handler),
		))
		Logger.Info(http.ListenAndServe(p.cfg.ListenAddr, nil))
	}()

	Logger.Info("Prometheus active!")

	return nil
}

// serveScrape handles a single Prometheus scrape. It binds the request's
// context into each pull collector - so the scraper disconnecting cancels the
// in-flight lnd queries - gathers a fresh per-request registry alongside the
// default one, and logs a single outcome line for any problem scrape (and
// nothing for a clean one).
func (p *PrometheusExporter) serveScrape(w http.ResponseWriter,
	r *http.Request, errorLogger *log.Logger) {

	start := time.Now()

	// Build a fresh registry for this scrape, binding each context-aware
	// collector to this request's context. A fresh registry and binding per
	// request keeps concurrent scrapes isolated.
	results := make([]*collectorResult, len(p.scrapeCollectors))

	// Build one GetInfo provider for this scrape and inject it into the
	// context every bound collector runs under, so the chain, info and
	// channels collectors share a single GetInfo RPC instead of each issuing
	// their own. It must be allocated here, per scrape, so concurrent scrapes
	// never share a cached result.
	scrapeCtx := withInfoProvider(
		r.Context(), newInfoProvider(p.lnd.Client),
	)

	reqReg := prometheus.NewRegistry()
	for i, sc := range p.scrapeCollectors {
		results[i] = &collectorResult{name: sc.Name()}
		reqReg.MustRegister(&boundCollector{
			sc:     sc,
			ctx:    scrapeCtx,
			result: results[i],
		})
	}

	promHandler := promhttp.HandlerFor(
		prometheus.Gatherers{prometheus.DefaultGatherer, reqReg},
		promhttp.HandlerOpts{
			ErrorLog:      errorLogger,
			ErrorHandling: promhttp.ContinueOnError,
		},
	)

	sw := &scrapeWriter{ResponseWriter: w, start: start}
	promHandler.ServeHTTP(sw, r)

	// net/http cancels r.Context() when the client disconnects while we're
	// serving; it is not canceled by our own return until after this handler
	// exits. So a non-nil error here means the scraper went away mid-scrape.
	disconnected := r.Context().Err() != nil

	// Forward real collector errors to the error channel so the existing
	// shutdown path can restart lndmon, and log the error text so the cause is
	// visible immediately rather than only when the process exits. Canceled
	// and lnd-timeout outcomes are expected transient conditions and are
	// skipped.
	//
	// When the scraper disconnected we skip this entirely: an abandoned scrape
	// cancels the in-flight lnd calls, and that cancellation does not always
	// surface as a Canceled code (a transport teardown can appear as
	// Unavailable, EOF, etc.), which would otherwise be misclassified as a
	// real error and needlessly restart lndmon. A genuine persistent lnd
	// failure fails fast without a disconnect and is still caught on the next
	// scrape. The send is non-blocking so a full buffer can never stall a
	// scrape.
	if !disconnected {
		for _, res := range results {
			if res.status == statusError && res.err != nil {
				Logger.Error(res.err)

				select {
				case p.errChan <- res.err:
				default:
				}
			}
		}
	}

	if logit, msg := scrapeOutcome(
		r.RemoteAddr, time.Since(start), disconnected, sw.failed,
		sw.failedAt, results,
	); logit {
		Logger.Warn(msg)
	}
}

// Stop shuts down the prometheus exporter, waiting for all goroutines to exit
// before returning.
func (p *PrometheusExporter) Stop() {
	log.Println("Stopping Prometheus Exporter")
	if !p.monitoringCfg.DisableHtlc {
		p.htlcMonitor.stop()
	}

	if !p.monitoringCfg.DisablePayments {
		p.paymentsMonitor.stop()
	}
}

// Errors returns an error channel that any failures experienced by its
// collectors experience.
func (p *PrometheusExporter) Errors() <-chan error {
	return p.errChan
}

// registerMetrics registers the event collectors with the default registry.
// The pull (scrape) collectors are not registered here; they are registered
// into a fresh per-request registry in serveScrape so each scrape's context
// can be bound into their lnd calls. If any collector fails to register, an
// error is returned.
func (p *PrometheusExporter) registerMetrics() error {
	for _, collector := range p.eventCollectors {
		err := prometheus.Register(collector)
		if err != nil {
			return err
		}
	}

	return nil
}
