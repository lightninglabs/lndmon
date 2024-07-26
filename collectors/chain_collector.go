package collectors

import (
	"context"
	"fmt"

	"github.com/lightninglabs/lndclient"
	"github.com/prometheus/client_golang/prometheus"
)

// ChainCollector is a collector that keeps track of on-chain information.
type ChainCollector struct {
	bestBlockHeight    *prometheus.Desc
	bestBlockTimestamp *prometheus.Desc
	syncedToChain      *prometheus.Desc
	syncedToGraph      *prometheus.Desc

	lnd lndclient.LightningClient

	// errChan is a channel that we send any errors that we encounter into.
	// This channel should be buffered so that it does not block sends.
	errChan chan<- error
}

// NewChainCollector returns a new instance of the ChainCollector for the target
// lnd client.
func NewChainCollector(lnd lndclient.LightningClient,
	errChan chan<- error) *ChainCollector {

	return &ChainCollector{
		bestBlockHeight: prometheus.NewDesc(
			"lnd_chain_block_height", "best block height from lnd",
			nil, nil,
		),
		bestBlockTimestamp: prometheus.NewDesc(
			"lnd_chain_block_timestamp",
			"best block timestamp from lnd",
			nil, nil,
		),
		syncedToChain: prometheus.NewDesc(
			"lnd_chain_synced",
			"lnd is synced to the chain",
			nil, nil,
		),
		syncedToGraph: prometheus.NewDesc(
			"lnd_graph_synced",
			"lnd is synced to the graph",
			nil, nil,
		),
		lnd:     lnd,
		errChan: errChan,
	}
}

// Describe sends the super-set of all possible descriptors of metrics
// collected by this Collector to the provided channel and returns once the
// last descriptor has been sent.
//
// NOTE: Part of the prometheus.Collector interface.
func (c *ChainCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.bestBlockHeight
	ch <- c.bestBlockTimestamp
	ch <- c.syncedToChain
	ch <- c.syncedToGraph
}

// Collect is called by the Prometheus registry when collecting metrics.
//
// NOTE: Part of the prometheus.Collector interface.
func (c *ChainCollector) Collect(ch chan<- prometheus.Metric) {
	resp, err := c.lnd.GetInfo(context.Background())
	if err != nil {
		errWithContext := fmt.Errorf("ChainCollector GetInfo "+
			"failed with: %w", err)
		Logger.Error(errWithContext)

		// If this isn't just a timeout, we'll want to exit to give the
		// runtime (Docker/k8s/systemd) a chance to restart us, in case
		// something with the lnd connection and/or credentials has
		// changed. We just do this check for the GetInfo call, since
		// that's known to sometimes randomly take way longer than on
		// average (database interactions?).
		if !IsDeadlineExceeded(err) {
			c.errChan <- errWithContext
		}

		return
	}

	ch <- prometheus.MustNewConstMetric(
		c.bestBlockHeight, prometheus.GaugeValue,
		float64(resp.BlockHeight),
	)

	ch <- prometheus.MustNewConstMetric(
		c.bestBlockTimestamp, prometheus.GaugeValue,
		float64(resp.BestHeaderTimeStamp.Unix()),
	)

	ch <- prometheus.MustNewConstMetric(
		c.syncedToChain, prometheus.GaugeValue,
		float64(boolToInt(resp.SyncedToChain)),
	)

	ch <- prometheus.MustNewConstMetric(
		c.syncedToGraph, prometheus.GaugeValue,
		float64(boolToInt(resp.SyncedToGraph)),
	)
}

func boolToInt(arg bool) uint8 {
	if arg {
		return 1
	}
	return 0
}
