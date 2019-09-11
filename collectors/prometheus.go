package collectors

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/lightninglabs/lndmon/config"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// collectors is a global variable of registered prometheus.Collectors
	// protected by the mutex below. All new Collectors should add
	// themselves to this map within the init() method of their file.
	collectors = make(map[string]func(config.Config, lnrpc.LightningClient) prometheus.Collector)

	metricsMtx sync.Mutex
)

// PrometheusExporter is a metric exporter that exports relevant lnd metrics
// such as routing policies to track how Lightning fees change over time.
type PrometheusExporter struct {
	cfg config.Config

	lnd lnrpc.LightningClient
}

// NewPrometheusExporter makes a new instance of the PrometheusExporter given
// the address to listen for Prometheus on and an lnd gRPC client.
func NewPrometheusExporter(cfg config.Config, lnd lnrpc.LightningClient) *PrometheusExporter {
	return &PrometheusExporter{
		cfg: cfg,
		lnd: lnd,
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

	// Finally, we'll launch the HTTP server that Prometheus will use to
	// scape our metrics.
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		Logger.Info(http.ListenAndServe(p.cfg.Prometheus.ListenAddr, nil))
	}()

	return nil
}

// registerMetrics iterates through all the registered collectors and attempts
// to register each one. If any of the collectors fail to register, then an
// error will be returned.
func (p *PrometheusExporter) registerMetrics() error {
	metricsMtx.Lock()
	defer metricsMtx.Unlock()

	for _, collectorFunc := range collectors {
		err := prometheus.Register(collectorFunc(p.cfg, p.lnd))
		if err != nil {
			return err
		}
	}

	return nil
}
