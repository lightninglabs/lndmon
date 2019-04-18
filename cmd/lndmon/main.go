package main

import (
	"fmt"
	"log"
	"net/http"
	"sync"

	"lndmon/lndclient"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// metricGroups is a global variable of all registered metrics
	// projected by the mutex below. All new MetricGroups should add
	// themselves to this map within the init() method of their file.
	metricGroups = make(map[string]MetricGroupCreator)

	// activeGroups is a global map of all active metric groups. This can
	// be used by some of the "static' package level methods to look up the
	// target metric group to export observations.
	activeGroups = make(map[string]MetricGroup)

	metricsMtx sync.Mutex

	lnd *lndclient.LndClient
)

// MetricGroupCreator is a factory method that given the primary prometheus
// config, will create a new MetricGroup that will be managed by the main
// PrometheusExporter.
type MetricGroupCreator func(*lndclient.LndClient) (MetricGroup, error)

// MetricGroup is the primary interface of this package. The main exporter (in
// this case the PrometheusExporter), will manage these directly, ensuring that
// all MetricGroups are registered before the main prometheus exporter starts
// and any additional tracing is added.
type MetricGroup interface {
	// Name is the name of the metric group. When exported to prometheus,
	// it's expected that all metric under this group have the same prefix.
	Name() string

	// RegisterMetricFuncs signals to the underlying hybrid collector that
	// it should register all metrics that it aims to export with the
	// global Prometheus registry. Rather than using the series of
	// "MustRegister" directives, implementers of this interface should
	// instead propagate back any errors related to metric registration.
	RegisterMetricFuncs() error
}

func main() {
	var err error
	lnd, err = lndclient.New()
	if err != nil {
		log.Printf("NewLndClient errored with: %v", err)
		return
	}

	if err := registerMetrics(); err != nil {
		log.Printf("registerMetrics errored with: %v", err)
		return
	}

	http.Handle("/metrics", promhttp.Handler())
	fmt.Println(http.ListenAndServe("0.0.0.0:9092", nil))
}

func registerMetrics() error {
	metricsMtx.Lock()
	defer metricsMtx.Unlock()

	for _, metricGroupFunc := range metricGroups {
		metricGroup, err := metricGroupFunc(lnd)
		if err != nil {
			return err
		}

		if err := metricGroup.RegisterMetricFuncs(); err != nil {
			return err
		}

		activeGroups[metricGroup.Name()] = metricGroup
	}

	return nil
}
