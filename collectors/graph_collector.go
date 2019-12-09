package collectors

import (
	"context"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/prometheus/client_golang/prometheus"
)

// GraphCollector is a collector that keeps track of graph information.
type GraphCollector struct {
	numEdgesDesc   *prometheus.Desc
	numNodesDesc   *prometheus.Desc
	numZombiesDesc *prometheus.Desc

	avgOutDegreeDesc  *prometheus.Desc
	maxOutDegreeDesc  *prometheus.Desc
	graphDiameterDesc *prometheus.Desc

	networkCapacityDesc *prometheus.Desc

	avgChanSizeDesc    *prometheus.Desc
	minChanSizeDesc    *prometheus.Desc
	maxChanSizeDesc    *prometheus.Desc
	medianChanSizeDesc *prometheus.Desc

	avgTimelockDeltaDesc    *prometheus.Desc
	minTimelockDeltaDesc    *prometheus.Desc
	maxTimelockDeltaDesc    *prometheus.Desc
	medianTimelockDeltaDesc *prometheus.Desc

	// TODO(roasbeef): make these into summaries instead?
	medianMinHtlcMsatDesc *prometheus.Desc
	maxMinHtlcMsatDesc    *prometheus.Desc
	minMinHtlcMsatDesc    *prometheus.Desc
	avgMinHtlcMsatDesc    *prometheus.Desc

	medianFeeBaseMsatDesc *prometheus.Desc
	minFeeBaseMsatDesc    *prometheus.Desc
	maxFeeBaseMsatDesc    *prometheus.Desc
	avgFeeBaseMsatDesc    *prometheus.Desc

	medianFeeRateMsatDesc *prometheus.Desc
	maxFeeRateMsatDesc    *prometheus.Desc
	minFeeRateMsatDesc    *prometheus.Desc
	avgFeeRateMsatDesc    *prometheus.Desc

	medianMaxHtlcMsatDesc *prometheus.Desc
	maxMaxHtlcMsatDesc    *prometheus.Desc
	minMaxHtlcMsatDesc    *prometheus.Desc
	avgMaxHtlcMsatDesc    *prometheus.Desc

	lnd lnrpc.LightningClient
}

// NewGraphCollector returns a new instance of the GraphCollector for the target
// lnd client.
func NewGraphCollector(lnd lnrpc.LightningClient) *GraphCollector {
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
		numZombiesDesc: prometheus.NewDesc(
			"lnd_graph_zombies_count",
			"total number of zombies in the graph",
			nil, nil,
		),

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

		avgChanSizeDesc: prometheus.NewDesc(
			"lnd_graph_chan_size_avg",
			"avg channel size in the network",
			nil, nil,
		),
		minChanSizeDesc: prometheus.NewDesc(
			"lnd_graph_chan_size_min",
			"min channel size in the network",
			nil, nil,
		),
		maxChanSizeDesc: prometheus.NewDesc(
			"lnd_graph_chan_size_max",
			"max channel size in the network",
			nil, nil,
		),
		medianChanSizeDesc: prometheus.NewDesc(
			"lnd_graph_chan_size_median",
			"median channel size in the network",
			nil, nil,
		),

		minTimelockDeltaDesc: prometheus.NewDesc(
			"lnd_graph_timelock_delta_min",
			"min time lock delta for a channel routing policy",
			nil, nil,
		),
		maxTimelockDeltaDesc: prometheus.NewDesc(
			"lnd_graph_timelock_delta_max",
			"max time lock delta for a channel routing policy",
			nil, nil,
		),
		avgTimelockDeltaDesc: prometheus.NewDesc(
			"lnd_graph_timelock_delta_avg",
			"avg time lock delta for a channel routing policy",
			nil, nil,
		),
		medianTimelockDeltaDesc: prometheus.NewDesc(
			"lnd_graph_timelock_delta_median",
			"median time lock delta for a channel routing policy",
			nil, nil,
		),

		medianMinHtlcMsatDesc: prometheus.NewDesc(
			"lnd_graph_min_htlc_msat_median",
			"median min htlc for a channel routing policy in msat",
			nil, nil,
		),
		avgMinHtlcMsatDesc: prometheus.NewDesc(
			"lnd_graph_min_htlc_msat_avg",
			"avg min htlc for a channel routing policy in msat",
			nil, nil,
		),
		minMinHtlcMsatDesc: prometheus.NewDesc(
			"lnd_graph_min_htlc_msat_min",
			"min min htlc for a channel routing policy in msat",
			nil, nil,
		),
		maxMinHtlcMsatDesc: prometheus.NewDesc(
			"lnd_graph_min_htlc_msat_max",
			"max min htlc for a channel routing policy in msat",
			nil, nil,
		),

		medianFeeBaseMsatDesc: prometheus.NewDesc(
			"lnd_graph_fee_base_msat_median",
			"median base fee for a channel routing policy in msat",
			nil, nil,
		),
		avgFeeBaseMsatDesc: prometheus.NewDesc(
			"lnd_graph_fee_base_msat_avg",
			"avg base fee for a channel routing policy in msat",
			nil, nil,
		),
		maxFeeBaseMsatDesc: prometheus.NewDesc(
			"lnd_graph_fee_base_msat_max",
			"max base fee for a channel routing policy in msat",
			nil, nil,
		),
		minFeeBaseMsatDesc: prometheus.NewDesc(
			"lnd_graph_fee_base_msat_min",
			"min base fee for a channel routing policy in msat",
			nil, nil,
		),

		medianFeeRateMsatDesc: prometheus.NewDesc(
			"lnd_graph_fee_rate_msat_median",
			"median fee rate for a channel routing policy in msat",
			nil, nil,
		),
		avgFeeRateMsatDesc: prometheus.NewDesc(
			"lnd_graph_fee_rate_msat_avg",
			"avg fee rate for a channel routing policy in msat",
			nil, nil,
		),
		maxFeeRateMsatDesc: prometheus.NewDesc(
			"lnd_graph_fee_rate_msat_max",
			"max fee rate for a channel routing policy in msat",
			nil, nil,
		),
		minFeeRateMsatDesc: prometheus.NewDesc(
			"lnd_graph_fee_rate_msat_min",
			"min fee rate for a channel routing policy in msat",
			nil, nil,
		),

		medianMaxHtlcMsatDesc: prometheus.NewDesc(
			"lnd_graph_max_htlc_msat_median",
			"median max htlc for a channel routing policy in msat",
			nil, nil,
		),
		avgMaxHtlcMsatDesc: prometheus.NewDesc(
			"lnd_graph_max_htlc_msat_avg",
			"avg max htlc for a channel routing policy in msat",
			nil, nil,
		),
		maxMaxHtlcMsatDesc: prometheus.NewDesc(
			"lnd_graph_max_htlc_msat_max",
			"max max htlc for a channel routing policy in msat",
			nil, nil,
		),
		minMaxHtlcMsatDesc: prometheus.NewDesc(
			"lnd_graph_max_htlc_msat_min",
			"min max htlc for a channel routing policy in msat",
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
func (g *GraphCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- g.numEdgesDesc
	ch <- g.numNodesDesc
	ch <- g.numZombiesDesc

	ch <- g.avgOutDegreeDesc
	ch <- g.maxOutDegreeDesc
	ch <- g.graphDiameterDesc

	ch <- g.networkCapacityDesc

	ch <- g.avgChanSizeDesc
	ch <- g.minChanSizeDesc
	ch <- g.maxChanSizeDesc
	ch <- g.medianChanSizeDesc

	ch <- g.minTimelockDeltaDesc
	ch <- g.maxTimelockDeltaDesc
	ch <- g.avgTimelockDeltaDesc
	ch <- g.medianTimelockDeltaDesc

	ch <- g.minMinHtlcMsatDesc
	ch <- g.maxMinHtlcMsatDesc
	ch <- g.avgMinHtlcMsatDesc
	ch <- g.medianMinHtlcMsatDesc

	ch <- g.minFeeBaseMsatDesc
	ch <- g.maxFeeBaseMsatDesc
	ch <- g.avgFeeBaseMsatDesc
	ch <- g.medianFeeBaseMsatDesc

	ch <- g.minFeeRateMsatDesc
	ch <- g.maxFeeRateMsatDesc
	ch <- g.avgFeeRateMsatDesc
	ch <- g.medianFeeRateMsatDesc

	ch <- g.minMaxHtlcMsatDesc
	ch <- g.maxMaxHtlcMsatDesc
	ch <- g.avgMaxHtlcMsatDesc
	ch <- g.medianMaxHtlcMsatDesc
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

	g.collectRoutingPolicyMetrics(ch, resp.Edges)

	networkInfo, err := g.lnd.GetNetworkInfo(
		context.Background(), &lnrpc.NetworkInfoRequest{},
	)
	if err != nil {
		graphLogger.Error(err)
		return
	}

	ch <- prometheus.MustNewConstMetric(
		g.numZombiesDesc, prometheus.GaugeValue,
		float64(networkInfo.NumZombieChans),
	)

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
	ch chan<- prometheus.Metric, edges []*lnrpc.ChannelEdge) {

	// To compute the upper limit on the total number of edges, we multiply
	// by two since we can have an edge in each direction.
	numEdges := uint32(len(edges)) * 2

	var (
		timelockStats = newStatsCompiler(numEdges)

		minHTLCStats = newStatsCompiler(numEdges)
		maxHTLCStats = newStatsCompiler(numEdges)

		feeBaseStats = newStatsCompiler(numEdges)
		feeRateStats = newStatsCompiler(numEdges)
	)

	for _, edge := range edges {
		policies := []*lnrpc.RoutingPolicy{edge.Node1Policy, edge.Node2Policy}
		for _, policy := range policies {
			if policy == nil {
				continue
			}

			timelockStats.Observe(float64(policy.TimeLockDelta))

			minHTLCStats.Observe(float64(policy.MinHtlc))
			maxHTLCStats.Observe(float64(policy.MaxHtlcMsat))

			feeBaseStats.Observe(float64(policy.FeeBaseMsat))
			feeRateStats.Observe(float64(policy.FeeRateMilliMsat))
		}
	}

	timelockReport := timelockStats.Report()
	minHTLCReport := minHTLCStats.Report()
	maxHTLCReport := maxHTLCStats.Report()
	feeBaseReport := feeBaseStats.Report()
	feeRateReport := feeRateStats.Report()

	ch <- prometheus.MustNewConstMetric(
		g.minTimelockDeltaDesc, prometheus.GaugeValue,
		timelockReport.min,
	)
	ch <- prometheus.MustNewConstMetric(
		g.maxTimelockDeltaDesc, prometheus.GaugeValue,
		timelockReport.max,
	)
	ch <- prometheus.MustNewConstMetric(
		g.avgTimelockDeltaDesc, prometheus.GaugeValue,
		timelockReport.avg,
	)
	ch <- prometheus.MustNewConstMetric(
		g.medianTimelockDeltaDesc, prometheus.GaugeValue,
		timelockReport.median,
	)

	ch <- prometheus.MustNewConstMetric(
		g.minMinHtlcMsatDesc, prometheus.GaugeValue,
		minHTLCReport.min,
	)
	ch <- prometheus.MustNewConstMetric(
		g.maxMinHtlcMsatDesc, prometheus.GaugeValue,
		minHTLCReport.max,
	)
	ch <- prometheus.MustNewConstMetric(
		g.avgMinHtlcMsatDesc, prometheus.GaugeValue,
		minHTLCReport.avg,
	)
	ch <- prometheus.MustNewConstMetric(
		g.medianMinHtlcMsatDesc, prometheus.GaugeValue,
		minHTLCReport.median,
	)

	ch <- prometheus.MustNewConstMetric(
		g.minMaxHtlcMsatDesc, prometheus.GaugeValue,
		maxHTLCReport.min,
	)
	ch <- prometheus.MustNewConstMetric(
		g.maxMaxHtlcMsatDesc, prometheus.GaugeValue,
		maxHTLCReport.max,
	)
	ch <- prometheus.MustNewConstMetric(
		g.avgMaxHtlcMsatDesc, prometheus.GaugeValue,
		maxHTLCReport.avg,
	)
	ch <- prometheus.MustNewConstMetric(
		g.medianMaxHtlcMsatDesc, prometheus.GaugeValue,
		maxHTLCReport.median,
	)

	ch <- prometheus.MustNewConstMetric(
		g.minFeeBaseMsatDesc, prometheus.GaugeValue,
		feeBaseReport.min,
	)
	ch <- prometheus.MustNewConstMetric(
		g.maxFeeBaseMsatDesc, prometheus.GaugeValue,
		feeBaseReport.max,
	)
	ch <- prometheus.MustNewConstMetric(
		g.avgFeeBaseMsatDesc, prometheus.GaugeValue,
		feeBaseReport.avg,
	)
	ch <- prometheus.MustNewConstMetric(
		g.medianFeeBaseMsatDesc, prometheus.GaugeValue,
		feeBaseReport.median,
	)

	ch <- prometheus.MustNewConstMetric(
		g.minFeeRateMsatDesc, prometheus.GaugeValue,
		feeRateReport.min,
	)
	ch <- prometheus.MustNewConstMetric(
		g.maxFeeRateMsatDesc, prometheus.GaugeValue,
		feeRateReport.max,
	)
	ch <- prometheus.MustNewConstMetric(
		g.avgFeeRateMsatDesc, prometheus.GaugeValue,
		feeRateReport.avg,
	)
	ch <- prometheus.MustNewConstMetric(
		g.medianFeeRateMsatDesc, prometheus.GaugeValue,
		feeRateReport.median,
	)
}

func init() {
	metricsMtx.Lock()
	collectors["graph"] = func(lnd lnrpc.LightningClient) prometheus.Collector {
		return NewGraphCollector(lnd)
	}
	metricsMtx.Unlock()
}
