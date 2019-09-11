package collectors

import (
	"context"

	"github.com/lightninglabs/lndmon/config"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/prometheus/client_golang/prometheus"
)

// ChainCollector is a collector that keeps track of on-chain information.
type ChainCollector struct {
	bestBlockHeight    *prometheus.Desc
	bestBlockTimestamp *prometheus.Desc

	lnd lnrpc.LightningClient
}

// NewChainCollector returns a new instance of the ChainCollector for the target
// lnd client.
func NewChainCollector(lnd lnrpc.LightningClient) *ChainCollector {
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
		lnd: lnd,
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
}

// Collect is called by the Prometheus registry when collecting metrics.
//
// NOTE: Part of the prometheus.Collector interface.
func (c *ChainCollector) Collect(ch chan<- prometheus.Metric) {
	resp, err := c.lnd.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		chainLogger.Error(err)
		return
	}

	ch <- prometheus.MustNewConstMetric(
		c.bestBlockHeight, prometheus.GaugeValue,
		float64(resp.BlockHeight),
	)

	ch <- prometheus.MustNewConstMetric(
		c.bestBlockTimestamp, prometheus.GaugeValue,
		float64(resp.BestHeaderTimestamp),
	)
}

func init() {
	metricsMtx.Lock()
	collectors["chain"] = func(cfg config.Config, lnd lnrpc.LightningClient) prometheus.Collector {
		return NewChainCollector(lnd)
	}
	metricsMtx.Unlock()
}
