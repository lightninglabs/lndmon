package collectors

import (
	"context"
	"strconv"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/prometheus/client_golang/prometheus"
)

// GraphCollector is a collector that keeps track of graph information.
type GraphCollector struct {
	numEdgesDesc *prometheus.Desc
	numNodesDesc *prometheus.Desc

	timelockDeltaDesc *prometheus.Desc
	minHtlcMsatDesc   *prometheus.Desc
	feeBaseMsatDesc   *prometheus.Desc
	feeRateMsatDesc   *prometheus.Desc
	maxHtlcMsatDesc   *prometheus.Desc

	lnd lnrpc.LightningClient
}

// NewGraphCollector returns a new instance of the GraphCollector for the target
// lnd client.
func NewGraphCollector(lnd lnrpc.LightningClient) *GraphCollector {
	labels := []string{"chan_id", "pubkey"}
	return &GraphCollector{
		numEdgesDesc: prometheus.NewDesc(
			"lnd_graph_edges_count",
			"total number of edges in the graph",
			nil, nil,
		),
		numNodesDesc: prometheus.NewDesc(
			"lnd_graph_nodes_count",
			"total number of nodes in the graph",
			nil, nil,
		),

		timelockDeltaDesc: prometheus.NewDesc(
			"lnd_channels_timelock_delta",
			"time lock delta for a channel routing policy",
			labels, nil,
		),
		minHtlcMsatDesc: prometheus.NewDesc(
			"lnd_channels_min_htlc_msat",
			"min htlc for a channel routing policy in msat",
			labels, nil,
		),
		feeBaseMsatDesc: prometheus.NewDesc(
			"lnd_channels_fee_base_msat",
			"base fee for a channel routing policy in msat",
			labels, nil,
		),
		feeRateMsatDesc: prometheus.NewDesc(
			"lnd_channels_fee_rate_msat",
			"fee rate for a channel routing policy in msat",
			labels, nil,
		),
		maxHtlcMsatDesc: prometheus.NewDesc(
			"lnd_channels_max_htlc_msat",
			"max htlc for a channel routing policy in msat",
			labels, nil,
		),

		lnd: lnd,
	}
}

// Describe sends the super-set of all possible descriptors of metrics
// collected by this Collector to the provided channel and returns once the
// last descriptor has been sent.
//
// NOTE: Part of the prometheus.Collector interface.
func (g *GraphCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- g.numEdgesDesc
	ch <- g.numNodesDesc

	ch <- g.timelockDeltaDesc
	ch <- g.minHtlcMsatDesc
	ch <- g.feeBaseMsatDesc
	ch <- g.feeRateMsatDesc
	ch <- g.maxHtlcMsatDesc
}

// Collect is called by the Prometheus registry when collecting metrics.
//
// NOTE: Part of the prometheus.Collector interface.
func (g *GraphCollector) Collect(ch chan<- prometheus.Metric) {
	resp, err := g.lnd.DescribeGraph(
		context.Background(), &lnrpc.ChannelGraphRequest{},
	)
	if err != nil {
		graphLogger.Error(err)
		return
	}

	ch <- prometheus.MustNewConstMetric(
		g.numEdgesDesc, prometheus.GaugeValue,
		float64(len(resp.Edges)),
	)

	ch <- prometheus.MustNewConstMetric(
		g.numNodesDesc, prometheus.GaugeValue,
		float64(len(resp.Nodes)),
	)

	for _, edge := range resp.Edges {
		g.collectRoutingPolicyMetrics(ch, edge)
	}
}

func (g *GraphCollector) collectRoutingPolicyMetrics(
	ch chan<- prometheus.Metric, edge *lnrpc.ChannelEdge) {

	pubkeys := []string{edge.Node1Pub, edge.Node2Pub}
	policies := []*lnrpc.RoutingPolicy{edge.Node1Policy, edge.Node2Policy}
	for i, policy := range policies {
		if policy == nil {
			continue
		}

		ch <- prometheus.MustNewConstMetric(
			g.timelockDeltaDesc, prometheus.GaugeValue,
			float64(policy.TimeLockDelta),
			strconv.Itoa(int(edge.ChannelId)), pubkeys[i],
		)
		ch <- prometheus.MustNewConstMetric(
			g.minHtlcMsatDesc, prometheus.GaugeValue,
			float64(policy.MinHtlc),
			strconv.Itoa(int(edge.ChannelId)), pubkeys[i],
		)
		ch <- prometheus.MustNewConstMetric(
			g.feeBaseMsatDesc, prometheus.GaugeValue,
			float64(policy.FeeBaseMsat),
			strconv.Itoa(int(edge.ChannelId)), pubkeys[i],
		)
		ch <- prometheus.MustNewConstMetric(
			g.feeRateMsatDesc, prometheus.GaugeValue,
			float64(policy.FeeRateMilliMsat),
			strconv.Itoa(int(edge.ChannelId)), pubkeys[i],
		)
		ch <- prometheus.MustNewConstMetric(
			g.maxHtlcMsatDesc, prometheus.GaugeValue,
			float64(policy.MaxHtlcMsat),
			strconv.Itoa(int(edge.ChannelId)), pubkeys[i],
		)
	}
}

func init() {
	metricsMtx.Lock()
	collectors["graph"] = func(lnd lnrpc.LightningClient) prometheus.Collector {
		return NewGraphCollector(lnd)
	}
	metricsMtx.Unlock()
}
