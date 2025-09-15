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

	// collectors is the exporter's active set of collectors.
	collectors []prometheus.Collector

	// errChan is an error channel that we receive errors from our
	// collectors on.
	errChan <-chan error
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
	quitChan chan struct{}) *PrometheusExporter {

	// We have six collectors and a htlc monitor running, so we buffer our
	// error channel by 8 so that we do not need to consume all errors from
	// this channel (on the first one, we'll start shutting down, but a few
	// could arrive quickly in the case where lnd is shutting down).
	errChan := make(chan error, 8)

	htlcMonitor := newHtlcMonitor(lnd.Router, errChan)

	// Create payments monitor.
	paymentsMonitor := newPaymentsMonitor(lnd, errChan)

	chanCollector := NewChannelsCollector(
		lnd.Client, errChan, quitChan, monitoringCfg,
	)

	collectors := []prometheus.Collector{
		NewChainCollector(lnd.Client, errChan),
		chanCollector,
		NewWalletCollector(lnd, errChan),
		NewPeerCollector(lnd.Client, errChan),
		NewInfoCollector(lnd.Client, errChan),
		NewStateCollector(lnd, errChan, monitoringCfg.ProgramStartTime),
		NewWtClientCollector(lnd, errChan),
	}

	if !monitoringCfg.DisableHtlc {
		collectors = append(collectors, htlcMonitor.collectors()...)
	}

	if !monitoringCfg.DisablePayments {
		collectors = append(
			collectors, paymentsMonitor.collectors()...,
		)
	}

	if !monitoringCfg.DisableGraph {
		collectors = append(
			collectors, NewGraphCollector(lnd.Client, errChan),
		)
	}

	return &PrometheusExporter{
		cfg:             cfg,
		lnd:             lnd,
		monitoringCfg:   monitoringCfg,
		collectors:      collectors,
		htlcMonitor:     htlcMonitor,
		paymentsMonitor: paymentsMonitor,
		errChan:         errChan,
	}
}

// Start registers all relevant metrics with the Prometheus library, then
// launches the HTTP server that Prometheus will hit to scrape our metrics.
func (p *PrometheusExporter) Start() error {
	err := initLogRotator(
		filepath.Join(p.cfg.LogDir, defaultLogFilename),
		defaultLogFileSize,
		defaultMaxLogFile,
	)
	if err != nil {
		return err
	}

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
		errorLogger := log.New(
			os.Stdout, "promhttp",
			log.Ldate|log.Ltime|log.Lshortfile,
		)

		promHandler := promhttp.InstrumentMetricHandler(
			prometheus.DefaultRegisterer,
			promhttp.HandlerFor(
				prometheus.DefaultGatherer,
				promhttp.HandlerOpts{
					ErrorLog:      errorLogger,
					ErrorHandling: promhttp.ContinueOnError,
				},
			),
		)
		http.Handle("/metrics", promHandler)
		Logger.Info(http.ListenAndServe(p.cfg.ListenAddr, nil))
	}()

	Logger.Info("Prometheus active!")

	return nil
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

// registerMetrics iterates through all the registered collectors and attempts
// to register each one. If any of the collectors fail to register, then an
// error will be returned.
func (p *PrometheusExporter) registerMetrics() error {
	for _, collector := range p.collectors {
		err := prometheus.Register(collector)
		if err != nil {
			return err
		}
	}

	return nil
}
