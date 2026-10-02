package collectors

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

// ChainCollector is a collector that keeps track of on-chain information.
type ChainCollector struct {
	bestBlockHeight    *prometheus.Desc
	bestBlockTimestamp *prometheus.Desc
	syncedToChain      *prometheus.Desc
	syncedToGraph      *prometheus.Desc
}

// NewChainCollector returns a new instance of the ChainCollector.
func NewChainCollector() *ChainCollector {
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

// Name identifies this collector in the scrape outcome log.
func (c *ChainCollector) Name() string {
	return "chain"
}

// Collect gathers the chain metrics using the scrape's context so an abandoned
// scrape cancels the in-flight lnd call. The error (if any) is returned for the
// handler to classify and log.
//
// NOTE: Part of the scrapeCollector interface.
func (c *ChainCollector) Collect(ctx context.Context,
	ch chan<- prometheus.Metric) error {

	// getInfo shares a single GetInfo RPC across the chain, info and channels
	// collectors for this scrape (see infoProvider).
	resp, err := getInfo(ctx)
	if err != nil {
		return fmt.Errorf("ChainCollector GetInfo failed with: %w", err)
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

	return nil
}

func boolToInt(arg bool) uint8 {
	if arg {
		return 1
	}
	return 0
}
