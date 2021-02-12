package collectors

import (
	"context"
	"fmt"
	"strconv"

	"github.com/btcsuite/btcutil"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/prometheus/client_golang/prometheus"
)

// ChannelsCollector is a collector that keeps track of channel infromation.
type ChannelsCollector struct {
	channelBalanceDesc        *prometheus.Desc
	pendingChannelBalanceDesc *prometheus.Desc

	incomingChanSatDesc *prometheus.Desc
	outgoingChanSatDesc *prometheus.Desc
	numPendingHTLCsDesc *prometheus.Desc

	numActiveChansDesc   *prometheus.Desc
	numInactiveChansDesc *prometheus.Desc
	numPendingChansDesc  *prometheus.Desc

	satsSentDesc *prometheus.Desc
	satsRecvDesc *prometheus.Desc

	numUpdatesDesc *prometheus.Desc

	channelUptimeDesc *prometheus.Desc

	csvDelayDesc         *prometheus.Desc
	unsettledBalanceDesc *prometheus.Desc
	feePerKwDesc         *prometheus.Desc
	commitWeightDesc     *prometheus.Desc
	commitFeeDesc        *prometheus.Desc

	// inboundFee is a metric that reflects the fee paid by senders on the
	// last hop towards this node.
	inboundFee *prometheus.Desc

	lnd lndclient.LightningClient

	primaryNode *route.Vertex

	// errChan is a channel that we send any errors that we encounter into.
	// This channel should be buffered so that it does not block sends.
	errChan chan<- error
}

// NewChannelsCollector returns a new instance of the ChannelsCollector for the
// target lnd client.
func NewChannelsCollector(lnd lndclient.LightningClient, errChan chan<- error,
	cfg *MonitoringConfig) *ChannelsCollector {

	// Our set of labels, status should either be active or inactive. The
	// initiator is "true" if we are the initiator, and "false" otherwise.
	labels := []string{"chan_id", "status", "initiator"}
	return &ChannelsCollector{
		channelBalanceDesc: prometheus.NewDesc(
			"lnd_channels_open_balance_sat",
			"total balance of channels in satoshis",
			nil, nil,
		),
		pendingChannelBalanceDesc: prometheus.NewDesc(
			"lnd_channels_pending_balance_sat",
			"total balance of all pending channels in satoshis",
			nil, nil,
		),

		incomingChanSatDesc: prometheus.NewDesc(
			"lnd_channels_bandwidth_incoming_sat",
			"total available incoming channel bandwidth within this channel",
			labels, nil,
		),
		outgoingChanSatDesc: prometheus.NewDesc(
			"lnd_channels_bandwidth_outgoing_sat",
			"total available outgoing channel bandwidth within this channel",
			labels, nil,
		),
		numPendingHTLCsDesc: prometheus.NewDesc(
			"lnd_channels_pending_htlc_count",
			"total number of pending active HTLCs within this channel",
			labels, nil,
		),

		numActiveChansDesc: prometheus.NewDesc(
			"lnd_channels_active_total",
			"total number of active channels",
			nil, nil,
		),
		numInactiveChansDesc: prometheus.NewDesc(
			"lnd_channels_inactive_total",
			"total number of inactive channels",
			nil, nil,
		),
		numPendingChansDesc: prometheus.NewDesc(
			"lnd_channels_pending_total",
			"total number of inactive channels",
			[]string{"state"}, nil,
		),
		csvDelayDesc: prometheus.NewDesc(
			"lnd_channels_csv_delay",
			"CSV delay in relative blocks for this channel",
			labels, nil,
		),
		unsettledBalanceDesc: prometheus.NewDesc(
			"lnd_channels_unsettled_balance",
			"unsettled balance in this channel",
			labels, nil,
		),

		feePerKwDesc: prometheus.NewDesc(
			"lnd_channels_fee_per_kw",
			"required number of sat per kiloweight that the "+
				"requester will pay for the funding and "+
				"commitment transaction",
			labels, nil,
		),
		commitWeightDesc: prometheus.NewDesc(
			"lnd_channels_commit_weight",
			"weight of the commitment transaction",
			labels, nil,
		),
		commitFeeDesc: prometheus.NewDesc(
			"lnd_channels_commit_fee",
			"weight of the commitment transaction",
			labels, nil,
		),
		satsSentDesc: prometheus.NewDesc(
			"lnd_channels_sent_sat",
			"total number of satoshis we’ve sent within this channel",
			labels, nil,
		),
		satsRecvDesc: prometheus.NewDesc(
			"lnd_channels_received_sat",
			"total number of satoshis we’ve received within this channel",
			labels, nil,
		),
		numUpdatesDesc: prometheus.NewDesc(
			"lnd_channels_updates_count",
			"total number of updates conducted within this channel",
			labels, nil,
		),
		channelUptimeDesc: prometheus.NewDesc(
			"lnd_channel_uptime_percentage",
			"uptime percentage for channel",
			labels, nil,
		),

		// Use labels for the inbound fee for various amounts.
		inboundFee: prometheus.NewDesc(
			"inbound_fee",
			"fee charged for forwarding to this node",
			[]string{"amount"}, nil,
		),

		lnd:         lnd,
		primaryNode: cfg.PrimaryNode,
		errChan:     errChan,
	}
}

// Describe sends the super-set of all possible descriptors of metrics
// collected by this Collector to the provided channel and returns once the
// last descriptor has been sent.
//
// NOTE: Part of the prometheus.Collector interface.
func (c *ChannelsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.channelBalanceDesc
	ch <- c.pendingChannelBalanceDesc

	ch <- c.incomingChanSatDesc
	ch <- c.outgoingChanSatDesc

	ch <- c.numPendingHTLCsDesc
	ch <- c.unsettledBalanceDesc

	ch <- c.numActiveChansDesc
	ch <- c.numInactiveChansDesc
	ch <- c.numPendingChansDesc

	ch <- c.satsSentDesc
	ch <- c.satsRecvDesc

	ch <- c.numUpdatesDesc

	ch <- c.channelUptimeDesc

	ch <- c.csvDelayDesc

	ch <- c.feePerKwDesc
	ch <- c.commitWeightDesc
	ch <- c.commitFeeDesc

	ch <- c.inboundFee
}

// Collect is called by the Prometheus registry when collecting metrics.
//
// NOTE: Part of the prometheus.Collector interface.
func (c *ChannelsCollector) Collect(ch chan<- prometheus.Metric) {
	// First, based on the channel balance, we'll export the total and
	// pending channel balances.
	chanBalResp, err := c.lnd.ChannelBalance(context.Background())
	if err != nil {
		c.errChan <- fmt.Errorf("ChannelsCollector ChannelBalance "+
			"failed with: %v", err)
		return
	}

	ch <- prometheus.MustNewConstMetric(
		c.channelBalanceDesc, prometheus.GaugeValue,
		float64(chanBalResp.Balance),
	)
	ch <- prometheus.MustNewConstMetric(
		c.pendingChannelBalanceDesc, prometheus.GaugeValue,
		float64(chanBalResp.PendingBalance),
	)

	// Obtain information w.r.t the number of channels we
	// have open.
	getInfoResp, err := c.lnd.GetInfo(context.Background())
	if err != nil {
		c.errChan <- fmt.Errorf("ChannelsCollector GetInfo failed "+
			"with: %v", err)
		return
	}

	ch <- prometheus.MustNewConstMetric(
		c.numActiveChansDesc, prometheus.GaugeValue,
		float64(getInfoResp.ActiveChannels),
	)
	ch <- prometheus.MustNewConstMetric(
		c.numInactiveChansDesc, prometheus.GaugeValue,
		float64(getInfoResp.InactiveChannels),
	)

	// Next, for each channel we'll export the total sum of our balances,
	// as well as the number of pending HTLCs.
	listChannelsResp, err := c.lnd.ListChannels(context.Background())
	if err != nil {
		c.errChan <- fmt.Errorf("ChannelsCollector ListChannels "+
			"failed with: %v", err)
		return
	}

	// statusLabel is a small helper function returns the proper status
	// label for a given channel.
	statusLabel := func(c lndclient.ChannelInfo) string {
		if c.Active {
			return "active"
		}

		return "inactive"
	}

	// initiatorLabel is a small helper function that returns the proper
	// "initiator" label for a given channel.
	initiatorLabel := func(c lndclient.ChannelInfo) string {
		if c.Initiator {
			return "true"
		}

		return "false"
	}

	remoteBalances := make(map[uint64]btcutil.Amount)
	for _, channel := range listChannelsResp {
		status := statusLabel(channel)
		initiator := initiatorLabel(channel)

		chanIdStr := strconv.Itoa(int(channel.ChannelID))

		primaryChannel := c.primaryNode != nil &&
			channel.PubKeyBytes == *c.primaryNode

		// Only record balances for channels that are usable and
		// external.
		if channel.Active && !primaryChannel {
			remoteBalances[channel.ChannelID] = channel.RemoteBalance
		}

		ch <- prometheus.MustNewConstMetric(
			c.incomingChanSatDesc, prometheus.GaugeValue,
			float64(channel.RemoteBalance), chanIdStr, status,
			initiator,
		)
		ch <- prometheus.MustNewConstMetric(
			c.outgoingChanSatDesc, prometheus.GaugeValue,
			float64(channel.LocalBalance), chanIdStr, status,
			initiator,
		)
		ch <- prometheus.MustNewConstMetric(
			c.numPendingHTLCsDesc, prometheus.GaugeValue,
			float64(channel.NumPendingHtlcs), chanIdStr, status,
			initiator,
		)
		ch <- prometheus.MustNewConstMetric(
			c.satsSentDesc, prometheus.GaugeValue,
			float64(channel.TotalSent), chanIdStr, status,
			initiator,
		)
		ch <- prometheus.MustNewConstMetric(
			c.satsRecvDesc, prometheus.GaugeValue,
			float64(channel.TotalReceived), chanIdStr, status,
			initiator,
		)
		ch <- prometheus.MustNewConstMetric(
			c.numUpdatesDesc, prometheus.GaugeValue,
			float64(channel.NumUpdates), chanIdStr, status,
			initiator,
		)
		ch <- prometheus.MustNewConstMetric(
			c.csvDelayDesc, prometheus.GaugeValue,
			float64(channel.LocalConstraints.CsvDelay), chanIdStr,
			status, initiator,
		)
		ch <- prometheus.MustNewConstMetric(
			c.unsettledBalanceDesc, prometheus.GaugeValue,
			float64(channel.UnsettledBalance), chanIdStr, status,
			initiator,
		)
		ch <- prometheus.MustNewConstMetric(
			c.feePerKwDesc, prometheus.GaugeValue,
			float64(channel.FeePerKw), chanIdStr, status, initiator,
		)
		ch <- prometheus.MustNewConstMetric(
			c.commitWeightDesc, prometheus.GaugeValue,
			float64(channel.CommitWeight), chanIdStr, status,
			initiator,
		)
		ch <- prometheus.MustNewConstMetric(
			c.commitFeeDesc, prometheus.GaugeValue,
			float64(channel.CommitFee), chanIdStr, status,
			initiator,
		)

		// Only record uptime if the channel has been monitored.
		if channel.LifeTime != 0 {
			ch <- prometheus.MustNewConstMetric(
				c.channelUptimeDesc, prometheus.GaugeValue,
				float64(channel.Uptime)/float64(channel.LifeTime),
				chanIdStr, status, initiator,
			)
		}
	}

	// Get the list of pending channels
	pendingChannelsResp, err := c.lnd.PendingChannels(context.Background())
	if err != nil {
		c.errChan <- fmt.Errorf("ChannelsCollector PendingChannels "+
			"failed with: %v", err)
		return
	}
	ch <- prometheus.MustNewConstMetric(
		c.numPendingChansDesc, prometheus.GaugeValue,
		float64(len(pendingChannelsResp.PendingForceClose)),
		"pending_force_close",
	)
	ch <- prometheus.MustNewConstMetric(
		c.numPendingChansDesc, prometheus.GaugeValue,
		float64(len(pendingChannelsResp.PendingOpen)),
		"pending_open",
	)
	ch <- prometheus.MustNewConstMetric(
		c.numPendingChansDesc, prometheus.GaugeValue,
		float64(len(pendingChannelsResp.WaitingClose)),
		"waiting_close",
	)

	// Get all remote policies
	remotePolicies, err := c.getRemotePolicies(getInfoResp.IdentityPubkey)
	if err != nil {
		c.errChan <- fmt.Errorf("ChannelsCollector getRemotePolicies "+
			"failed with: %v", err)
		return
	}

	// Export the inbound fee metric for a series of amounts.
	var receiveAmt btcutil.Amount = 100000
	for {
		// For each fee amount, we'll approximate the total routing fee
		// that needs to be paid to pay us.
		inboundFee := approximateInboundFee(
			receiveAmt, remotePolicies, remoteBalances,
		)
		if inboundFee == nil {
			break
		}

		// Calculate the fee proportional to the amount to receive.
		proportionalFee := float64(*inboundFee) / float64(receiveAmt)

		ch <- prometheus.MustNewConstMetric(
			c.inboundFee, prometheus.GaugeValue,
			proportionalFee,
			receiveAmt.String(),
		)

		// Continue the series with double the amount.
		receiveAmt *= 2
	}
}

// approximateInboundFee calculates to forward fee for a specific amount charged by the
// last hop before this node.
func approximateInboundFee(amt btcutil.Amount, remotePolicies map[uint64]*lndclient.RoutingPolicy,
	remoteBalances map[uint64]btcutil.Amount) *btcutil.Amount {

	var fee btcutil.Amount

	// Copy the remote balances so they can be decreased as we find shards.
	remainingBalances := make(map[uint64]btcutil.Amount)
	for ch, balance := range remoteBalances {
		remainingBalances[ch] = balance
	}

	// Assume a perfect mpp splitting algorithm that knows exactly how much
	// can be sent through each channel. This is a simplification, because
	// in reality senders need to trial and error to find a shard amount
	// that works.
	//
	// We'll keep iterating through all channels until we've covered the
	// total amount. Each iteration, the best channel for that shard is
	// selected based on the specific fee.
	amountRemaining := amt
	for amountRemaining > 0 {
		var (
			bestChan        uint64
			bestSpecificFee float64
			bestAmount      btcutil.Amount
			bestFee         btcutil.Amount
		)

		// Find the best channel to send the amount or a part of the
		// amount.
		for ch, balance := range remainingBalances {
			// Skip channels without remote balance.
			if balance == 0 {
				continue
			}

			policy, ok := remotePolicies[ch]
			if !ok {
				continue
			}

			// Cap at the maximum receive amount for this channel.
			amountToSend := amountRemaining
			if amountToSend > balance {
				amountToSend = balance
			}

			// Calculate fee for this amount to send.
			fee := btcutil.Amount(
				policy.FeeBaseMsat/1000 +
					int64(amountToSend)*policy.FeeRateMilliMsat/1000000,
			)

			// Calculate the specific fee for this amount, being the
			// fee per sat sent.
			specificFee := float64(fee) / float64(amountToSend)

			// Select the best channel for this shard based on the
			// lowest specific fee.
			if bestChan == 0 || bestSpecificFee > specificFee {
				bestChan = ch
				bestSpecificFee = specificFee
				bestAmount = amountToSend
				bestFee = fee
			}
		}

		// No liquidity to send the full amount, break.
		if bestChan == 0 {
			return nil
		}

		amountRemaining -= bestAmount
		fee += bestFee
		remainingBalances[bestChan] -= bestAmount
	}

	return &fee
}

// getRemotePolicies gets all the remote policies for enabled channels of this
// node's peers.
func (c *ChannelsCollector) getRemotePolicies(pubkey route.Vertex) (
	map[uint64]*lndclient.RoutingPolicy, error) {

	nodeInfoResp, err := c.lnd.GetNodeInfo(
		context.Background(), pubkey, true,
	)
	if err != nil {
		return nil, err
	}

	policies := make(map[uint64]*lndclient.RoutingPolicy)
	for _, i := range nodeInfoResp.Channels {
		var policy *lndclient.RoutingPolicy
		switch {
		case i.Node1 == pubkey:
			policy = i.Node1Policy

		case i.Node2 == pubkey:
			policy = i.Node1Policy

		default:
			return nil, fmt.Errorf("pubkey not in node info channels")
		}

		// Only record policies for peers that have this channel
		// enabled.
		if policy != nil && !policy.Disabled {
			policies[i.ChannelId] = policy
		}
	}

	return policies, nil
}
