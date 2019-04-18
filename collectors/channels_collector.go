package collectors

import (
	"fmt"
	"log"

	"github.com/prometheus/client_golang/prometheus"
)

type channelsCollector struct {
	lnd *lndClient

	openBalanceDesc        *prometheus.Desc
	pendingOpenBalanceDesc *prometheus.Desc

	numActiveDesc   *prometheus.Desc
	numInactiveDesc *prometheus.Desc
	numPendingDesc  *prometheus.Desc
	newChansDesc    *prometheus.Desc // a counter

	localBalanceDesc      *prometheus.Desc
	remoteBalanceDesc     *prometheus.Desc
	commitWeightDesc      *prometheus.Desc
	externalCommitFeeDesc *prometheus.Desc
	capacityDesc          *prometheus.Desc
	feePerKwDesc          *prometheus.Desc
	satsSentTotalDesc     *prometheus.Desc
	satsRecvTotalDesc     *prometheus.Desc
	updatesCountDesc      *prometheus.Desc
	pendingHtlcsCountDesc *prometheus.Desc
	csvDelayDesc          *prometheus.Desc
}

type channelMetadata struct {
	channelPoint string
	private      bool
}

func newChannelsCollector(lnd *lndClient) MetricGroup {
	individualChanLabels := []string{"channel_point", "public_or_private"}
	return &channelsCollector{
		lnd: lnd,
		openBalanceDesc: prometheus.NewDesc(
			"channels_open_balance_total_sat",
			"Sum of channel balances denominated in satoshis.",
			nil, nil,
		),
		pendingOpenBalanceDesc: prometheus.NewDesc(
			"channels_pending_open_balance_total_sat",
			"Sum of channels pending balances denominated in satoshis.",
			nil, nil,
		),

		numActiveDesc: prometheus.NewDesc(
			"channels_active_total",
			"Total number of active channels",
			nil, nil,
		),
		numInactiveDesc: prometheus.NewDesc(
			"channels_inactive_total",
			"Total number of inactive channels",
			nil, nil,
		),
		numPendingDesc: prometheus.NewDesc(
			"channels_pending_total",
			"Total number of inactive channels",
			nil, nil,
		),

		localBalanceDesc: prometheus.NewDesc(
			"channels_local_balance_sat",
			"This node’s current balance in this channel",
			individualChanLabels, nil,
		),
		remoteBalanceDesc: prometheus.NewDesc(
			"channels_remote_balance_sat",
			"The counterparty’s current balance in this channel",
			individualChanLabels, nil,
		),
		commitWeightDesc: prometheus.NewDesc(
			"channels_commit_weight",
			"The weight of the commitment transaction",
			individualChanLabels, nil,
		),
		externalCommitFeeDesc: prometheus.NewDesc(
			"channels_external_commit_fee_sat",
			"External commit fee in satoshis",
			individualChanLabels, nil,
		),
		capacityDesc: prometheus.NewDesc(
			"channels_capacity_sat",
			"The total amount of funds held in this channel",
			individualChanLabels, nil,
		),
		feePerKwDesc: prometheus.NewDesc(
			"channels_fee_per_kw_sat",
			"The required number of satoshis per kilo-weight that the "+
				"requester will pay at all times, for both the funding "+
				"transaction and commitment transaction. This value can "+
				"later be updated once the channel is open.",
			individualChanLabels, nil,
		),
		satsSentTotalDesc: prometheus.NewDesc(
			"channels_sent_sat",
			"The total number of satoshis we’ve sent within this channel.",
			individualChanLabels, nil,
		),
		satsRecvTotalDesc: prometheus.NewDesc(
			"channels_received_sat",
			"The total number of satoshis we’ve received within this channel.",
			individualChanLabels, nil,
		),
		updatesCountDesc: prometheus.NewDesc(
			"channels_updates_count",
			"The total number of updates conducted within this channel.",
			individualChanLabels, nil,
		),
		pendingHtlcsCountDesc: prometheus.NewDesc(
			"channels_pending_htlcs_count",
			"The list of active, uncleared HTLCs currently pending within "+
				"the channel.",
			individualChanLabels, nil,
		),
		csvDelayDesc: prometheus.NewDesc(
			"channels_csv_delay_blocks",
			"The CSV delay expressed in relative blocks.",
			individualChanLabels, nil,
		),
	}
}

// Name is the name of the metric group. When exported to prometheus, it's
// expected that all metric under this group have the same prefix.
//
// NOTE: Part of the MetricGroup interface.
func (l *channelsCollector) Name() string {
	return "channels"
}

func (c *channelsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.openBalanceDesc
	ch <- c.pendingOpenBalanceDesc

	ch <- c.numActiveDesc
	ch <- c.numInactiveDesc
	ch <- c.numPendingDesc

	ch <- c.localBalanceDesc
	ch <- c.remoteBalanceDesc
	ch <- c.commitWeightDesc
	ch <- c.externalCommitFeeDesc
	ch <- c.capacityDesc
	ch <- c.feePerKwDesc
	ch <- c.satsSentTotalDesc
	ch <- c.satsRecvTotalDesc
	ch <- c.updatesCountDesc
	ch <- c.pendingHtlcsCountDesc
	ch <- c.csvDelayDesc
}

func (c *channelsCollector) Collect(ch chan<- prometheus.Metric) {
	if err := c.collectChanBalances(ch); err != nil {
		log.Printf("errored in channelsCollector's Collect: %v", err)
	}

	if err := c.collectChanStatuses(ch); err != nil {
		log.Printf("errored in channelsCollector's Collect: %v", err)
	}

	if err := c.collectOpenChannels(ch); err != nil {
		log.Printf("errored in channelsCollector's Collect: %v", err)
	}
}

func (c *channelsCollector) collectChanBalances(ch chan<- prometheus.Metric) error {
	channelBalanceResp, err := c.lnd.ChannelBalance()
	if err != nil {
		return fmt.Errorf("ChannelBalance gRPC error: %v", err)
	}

	ch <- prometheus.MustNewConstMetric(
		c.openBalanceDesc,
		prometheus.GaugeValue,
		float64(channelBalanceResp.Balance),
	)
	ch <- prometheus.MustNewConstMetric(
		c.pendingOpenBalanceDesc,
		prometheus.GaugeValue,
		float64(channelBalanceResp.PendingOpenBalance),
	)

	return nil
}

func (c *channelsCollector) collectChanStatuses(ch chan<- prometheus.Metric) error {
	getInfoResp, err := c.lnd.GetInfo()
	if err != nil {
		return fmt.Errorf("GetInfo gRPC error: %v", err)
	}

	ch <- prometheus.MustNewConstMetric(
		c.numActiveDesc, prometheus.GaugeValue,
		float64(getInfoResp.NumActiveChannels),
	)
	ch <- prometheus.MustNewConstMetric(
		c.numInactiveDesc, prometheus.GaugeValue,
		float64(getInfoResp.NumInactiveChannels),
	)
	ch <- prometheus.MustNewConstMetric(
		c.numActiveDesc, prometheus.GaugeValue,
		float64(getInfoResp.NumPendingChannels),
	)

	return nil
}

func (c *channelsCollector) collectOpenChannels(metrics chan<- prometheus.Metric) error {
	listChansResp, err := c.lnd.ListChannels()
	if err != nil {
		return fmt.Errorf("ListChannels gRPC error: %v", err)
	}

	for _, ch := range listChansResp.Channels {
		md := channelMetadata{
			channelPoint: ch.ChannelPoint,
			private:      ch.Private,
		}
		metrics <- c.gauge(c.localBalanceDesc, float64(ch.LocalBalance), md)
		metrics <- c.gauge(c.remoteBalanceDesc, float64(ch.RemoteBalance), md)
		metrics <- c.gauge(c.commitWeightDesc, float64(ch.CommitWeight), md)
		metrics <- c.gauge(c.externalCommitFeeDesc, float64(ch.CommitFee), md)
		metrics <- c.gauge(c.capacityDesc, float64(ch.Capacity), md)
		metrics <- c.gauge(c.feePerKwDesc, float64(ch.FeePerKw), md)
		metrics <- c.gauge(c.updatesCountDesc, float64(ch.NumUpdates), md)
		metrics <- c.gauge(c.csvDelayDesc, float64(ch.CsvDelay), md)
		metrics <- c.gauge(
			c.satsSentTotalDesc, float64(ch.TotalSatoshisSent), md)
		metrics <- c.gauge(
			c.satsRecvTotalDesc, float64(ch.TotalSatoshisReceived), md)
		metrics <- c.gauge(
			c.pendingHtlcsCountDesc, float64(len(ch.PendingHtlcs)), md)
	}

	return nil
}

func (c *channelsCollector) gauge(
	desc *prometheus.Desc, value float64, metadata channelMetadata) prometheus.Metric {
	log.Printf("gauge value: %v\n", value)

	publicOrPrivateLabel := "public"
	if metadata.private {
		publicOrPrivateLabel = "private"
	}
	return prometheus.MustNewConstMetric(
		desc,
		prometheus.GaugeValue,
		value,
		metadata.channelPoint,
		publicOrPrivateLabel,
	)
}

func (c *channelsCollector) RegisterMetricFuncs() error {
	if err := prometheus.Register(c); err != nil {
		return err
	}

	return nil
}

// A compile time flag to ensure the channelsCollector satisfies the MetricGroup
// interface.
var _ MetricGroup = (*channelsCollector)(nil)

func init() {
	metricsMtx.Lock()
	metricGroups["channels"] = func(lnd *lndClient) (
		MetricGroup, error) {

		return newChannelsCollector(lnd), nil
	}
	metricsMtx.Unlock()
}
