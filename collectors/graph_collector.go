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
	avgOutDegreeDesc  *prometheus.Desc
	maxOutDegreeDesc  *prometheus.Desc
	graphDiameterDesc *prometheus.Desc

	networkCapacityDesc *prometheus.Desc

	avgChanSizeDesc    *prometheus.Desc
	minChanSizeDesc    *prometheus.Desc
	maxChanSizeDesc    *prometheus.Desc
	medianChanSizeDesc *prometheus.Desc

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
			"lnd_graph_timelock_delta",
			"time lock delta for a channel routing policy",
			labels, nil,
		avgOutDegreeDesc: prometheus.NewDesc(
			"lnd_graph_outdegree_avg",
			"avg out degree of nodes in the network",
			nil, nil,
		),
		maxOutDegreeDesc: prometheus.NewDesc(
			"lnd_graph_outdegree_max",
			"max out degree of nodes in the network",
			nil, nil,
		),
		graphDiameterDesc: prometheus.NewDesc(
			"lnd_graph_diameter",
			"diameter of current network graph",
			nil, nil,
		),

		networkCapacityDesc: prometheus.NewDesc(
			"lnd_graph_chan_capacity_sat",
			"total network capacity",
			nil, nil,
		),

		),
		minHtlcMsatDesc: prometheus.NewDesc(
			"lnd_graph_min_htlc_msat",
			"min htlc for a channel routing policy in msat",
			labels, nil,
		),
		feeBaseMsatDesc: prometheus.NewDesc(
			"lnd_graph_fee_base_msat",
			"base fee for a channel routing policy in msat",
			labels, nil,
		),
		feeRateMsatDesc: prometheus.NewDesc(
			"lnd_graph_fee_rate_msat",
			"fee rate for a channel routing policy in msat",
			labels, nil,
		),
		maxHtlcMsatDesc: prometheus.NewDesc(
			"lnd_graph_max_htlc_msat",
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
	ch <- g.avgOutDegreeDesc
	ch <- g.maxOutDegreeDesc
	ch <- g.graphDiameterDesc

	ch <- g.networkCapacityDesc

	ch <- g.avgChanSizeDesc
	ch <- g.minChanSizeDesc
	ch <- g.maxChanSizeDesc
	ch <- g.medianChanSizeDesc
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
	networkInfo, err := g.lnd.GetNetworkInfo(
		context.Background(), &lnrpc.NetworkInfoRequest{},
	)
	if err != nil {
		graphLogger.Error(err)
		return
	}

	ch <- prometheus.MustNewConstMetric(
		g.avgOutDegreeDesc, prometheus.GaugeValue,
		float64(networkInfo.AvgOutDegree),
	)
	ch <- prometheus.MustNewConstMetric(
		g.maxOutDegreeDesc, prometheus.GaugeValue,
		float64(networkInfo.MaxOutDegree),
	)

	ch <- prometheus.MustNewConstMetric(
		g.graphDiameterDesc, prometheus.GaugeValue,
		float64(networkInfo.GraphDiameter),
	)

	ch <- prometheus.MustNewConstMetric(
		g.networkCapacityDesc, prometheus.GaugeValue,
		float64(networkInfo.TotalNetworkCapacity),
	)

	ch <- prometheus.MustNewConstMetric(
		g.avgChanSizeDesc, prometheus.GaugeValue,
		float64(networkInfo.AvgChannelSize),
	)
	ch <- prometheus.MustNewConstMetric(
		g.minChanSizeDesc, prometheus.GaugeValue,
		float64(networkInfo.MinChannelSize),
	)
	ch <- prometheus.MustNewConstMetric(
		g.maxChanSizeDesc, prometheus.GaugeValue,
		float64(networkInfo.MaxChannelSize),
	)
	ch <- prometheus.MustNewConstMetric(
		g.medianChanSizeDesc, prometheus.GaugeValue,
		float64(networkInfo.MedianChannelSizeSat),
	)
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
