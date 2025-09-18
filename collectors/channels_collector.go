package collectors

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/prometheus/client_golang/prometheus"
)

// Cache refresh interval magic number.
const cacheRefreshInterval = 10 * time.Minute

// ChannelsCollector is a collector that keeps track of channel information.
type ChannelsCollector struct {
	channelBalanceDesc           *prometheus.Desc
	pendingChannelBalanceDesc    *prometheus.Desc
	pendingForceCloseBalanceDesc *prometheus.Desc
	waitingCloseBalanceDesc      *prometheus.Desc

	incomingChanSatDesc *prometheus.Desc
	outgoingChanSatDesc *prometheus.Desc
	numPendingHTLCsDesc *prometheus.Desc

	numActiveChansDesc   *prometheus.Desc
	numInactiveChansDesc *prometheus.Desc
	numPendingChansDesc  *prometheus.Desc
	numClosedChannels    *prometheus.Desc

	satsSentDesc *prometheus.Desc
	satsRecvDesc *prometheus.Desc

	numUpdatesDesc *prometheus.Desc

	channelUptimeDesc *prometheus.Desc

	csvDelayDesc         *prometheus.Desc
	unsettledBalanceDesc *prometheus.Desc
	feePerKwDesc         *prometheus.Desc
	commitWeightDesc     *prometheus.Desc
	commitFeeDesc        *prometheus.Desc

	localBaseFeeDesc        *prometheus.Desc
	localFeeRateDesc        *prometheus.Desc
	localInboundBaseFeeDesc *prometheus.Desc
	localInboundFeeRateDesc *prometheus.Desc

	remoteBaseFeeDesc        *prometheus.Desc
	remoteFeeRateDesc        *prometheus.Desc
	remoteInboundBaseFeeDesc *prometheus.Desc
	remoteInboundFeeRateDesc *prometheus.Desc

	// inboundFee is a metric that reflects the fee paid by senders on the
	// last hop towards this node.
	inboundFee *prometheus.Desc

	lnd lndclient.LightningClient

	primaryNode *route.Vertex

	// errChan is a channel that we send any errors that we encounter into.
	// This channel should be buffered so that it does not block sends.
	errChan chan<- error

	// quit is a channel that we use to signal for graceful shutdown.
	quit chan struct{}

	// cache is for storing results from a ticker to reduce grpc server
	// load on lnd.
	closedChannelsCache []lndclient.ClosedChannel
	cacheMutex          sync.RWMutex
}

// NewChannelsCollector returns a new instance of the ChannelsCollector for the
// target lnd client.
func NewChannelsCollector(lnd lndclient.LightningClient, errChan chan<- error,
	quitChan chan struct{}, cfg *MonitoringConfig) *ChannelsCollector {

	// Our set of labels, status should either be active or inactive. The
	// initiator is "true" if we are the initiator, and "false" otherwise.
	labels := []string{"chan_id", "status", "initiator", "peer"}
	collector := &ChannelsCollector{
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
		pendingForceCloseBalanceDesc: prometheus.NewDesc(
			"lnd_channels_pending_force_close_balance_sat",
			"force closed channel balances in satoshis",
			[]string{"status"}, nil,
		),
		waitingCloseBalanceDesc: prometheus.NewDesc(
			"lnd_channels_waiting_close_balance_sat",
			"waiting to close channel balances in satoshis",
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
		numClosedChannels: prometheus.NewDesc(
			"lnd_closed_channels_total",
			"total number of closed channels",
			[]string{"close_type"}, nil,
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

		localBaseFeeDesc: prometheus.NewDesc(
			"lnd_channels_local_base_fee_msat",
			"local base fee in millisatoshis for this channel",
			labels, nil,
		),
		localFeeRateDesc: prometheus.NewDesc(
			"lnd_channels_local_fee_rate",
			"local fee rate in millionths for this channel",
			labels, nil,
		),
		localInboundBaseFeeDesc: prometheus.NewDesc(
			"lnd_channels_local_inbound_base_fee_msat",
			"local inbound base fee in millisatoshis for this channel",
			labels, nil,
		),
		localInboundFeeRateDesc: prometheus.NewDesc(
			"lnd_channels_local_inbound_fee_rate",
			"local inbound fee rate in millionths for this channel",
			labels, nil,
		),
		remoteBaseFeeDesc: prometheus.NewDesc(
			"lnd_channels_remote_base_fee_msat",
			"remote base fee in millisatoshis for this channel",
			labels, nil,
		),
		remoteFeeRateDesc: prometheus.NewDesc(
			"lnd_channels_remote_fee_rate",
			"remote fee rate in millionths for this channel",
			labels, nil,
		),
		remoteInboundBaseFeeDesc: prometheus.NewDesc(
			"lnd_channels_remote_inbound_base_fee_msat",
			"remote inbound base fee in millisatoshis for this channel",
			labels, nil,
		),
		remoteInboundFeeRateDesc: prometheus.NewDesc(
			"lnd_channels_remote_inbound_fee_rate",
			"remote inbound fee rate in millionths for this channel",
			labels, nil,
		),

		lnd:                 lnd,
		primaryNode:         cfg.PrimaryNode,
		closedChannelsCache: nil,
		errChan:             errChan,
		quit:                quitChan,
	}

	// Start a ticker to update the cache once per 10m
	go func() {
		ticker := time.NewTicker(cacheRefreshInterval)
		defer ticker.Stop()

		for {
			err := collector.refreshClosedChannelsCache()
			if err != nil {
				errChan <- err
			}

			select {
			case <-ticker.C:
				continue

			case <-collector.quit:
				return
			}
		}
	}()

	return collector
}

// refreshClosedChannelsCache acquires a mutex write lock to update
// the closedChannelsCache.
func (c *ChannelsCollector) refreshClosedChannelsCache() error {
	data, err := c.lnd.ClosedChannels(context.Background())
	if err != nil {
		return err
	}
	c.cacheMutex.Lock()
	c.closedChannelsCache = data
	c.cacheMutex.Unlock()

	return nil
}

// Describe sends the super-set of all possible descriptors of metrics
// collected by this Collector to the provided channel and returns once the
// last descriptor has been sent.
//
// NOTE: Part of the prometheus.Collector interface.
func (c *ChannelsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.channelBalanceDesc
	ch <- c.pendingChannelBalanceDesc
	ch <- c.pendingForceCloseBalanceDesc
	ch <- c.waitingCloseBalanceDesc

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

	ch <- c.localBaseFeeDesc
	ch <- c.localFeeRateDesc
	ch <- c.localInboundBaseFeeDesc
	ch <- c.localInboundFeeRateDesc

	ch <- c.remoteBaseFeeDesc
	ch <- c.remoteFeeRateDesc
	ch <- c.remoteInboundBaseFeeDesc
	ch <- c.remoteInboundFeeRateDesc
}

func anchorStateToString(state lndclient.ForceCloseAnchorState) string {
	switch state {
	case lndclient.ForceCloseAnchorStateLimbo:
		return "Limbo"

	case lndclient.ForceCloseAnchorStateRecovered:
		return "Recovered"

	case lndclient.ForceCloseAnchorStateLost:
		return "Lost"

	default:
		return "Unknown"
	}
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
		errWithContext := fmt.Errorf("ChannelsCollector GetInfo "+
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
		c.numActiveChansDesc, prometheus.GaugeValue,
		float64(getInfoResp.ActiveChannels),
	)
	ch <- prometheus.MustNewConstMetric(
		c.numInactiveChansDesc, prometheus.GaugeValue,
		float64(getInfoResp.InactiveChannels),
	)

	// Next, for each channel we'll export the total sum of our balances,
	// as well as the number of pending HTLCs.
	listChannelsResp, err := c.lnd.ListChannels(context.Background(), false, false)
	if err != nil {
		c.errChan <- fmt.Errorf("ChannelsCollector ListChannels "+
			"failed with: %v", err)
		return
	}

	nodeInfo, err := c.lnd.GetNodeInfo(context.Background(), getInfoResp.IdentityPubkey, true)
	if err != nil {
		c.errChan <- fmt.Errorf("ChannelsCollector GetNodeInfo "+
			"failed with: %v", err)
		return
	}
	channelInfoMap := make(map[uint64]lndclient.ChannelEdge)
	for _, c := range nodeInfo.Channels {
		channelInfoMap[c.ChannelID] = c
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
		peer := channel.PubKeyBytes.String()

		chanIDStr := strconv.Itoa(int(channel.ChannelID))

		primaryChannel := c.primaryNode != nil &&
			channel.PubKeyBytes == *c.primaryNode

		// Only record balances for channels that are usable and
		// external.
		if channel.Active && !primaryChannel {
			remoteBalances[channel.ChannelID] = channel.RemoteBalance
		}

		ch <- prometheus.MustNewConstMetric(
			c.incomingChanSatDesc, prometheus.GaugeValue,
			float64(channel.RemoteBalance), chanIDStr, status,
			initiator, peer,
		)
		ch <- prometheus.MustNewConstMetric(
			c.outgoingChanSatDesc, prometheus.GaugeValue,
			float64(channel.LocalBalance), chanIDStr, status,
			initiator, peer,
		)
		ch <- prometheus.MustNewConstMetric(
			c.numPendingHTLCsDesc, prometheus.GaugeValue,
			float64(channel.NumPendingHtlcs), chanIDStr, status,
			initiator, peer,
		)
		ch <- prometheus.MustNewConstMetric(
			c.satsSentDesc, prometheus.GaugeValue,
			float64(channel.TotalSent), chanIDStr, status,
			initiator, peer,
		)
		ch <- prometheus.MustNewConstMetric(
			c.satsRecvDesc, prometheus.GaugeValue,
			float64(channel.TotalReceived), chanIDStr, status,
			initiator, peer,
		)
		ch <- prometheus.MustNewConstMetric(
			c.numUpdatesDesc, prometheus.GaugeValue,
			float64(channel.NumUpdates), chanIDStr, status,
			initiator, peer,
		)
		ch <- prometheus.MustNewConstMetric(
			c.csvDelayDesc, prometheus.GaugeValue,
			float64(channel.LocalConstraints.CsvDelay), chanIDStr,
			status, initiator, peer,
		)
		ch <- prometheus.MustNewConstMetric(
			c.unsettledBalanceDesc, prometheus.GaugeValue,
			float64(channel.UnsettledBalance), chanIDStr, status,
			initiator, peer,
		)
		ch <- prometheus.MustNewConstMetric(
			c.feePerKwDesc, prometheus.GaugeValue,
			float64(channel.FeePerKw), chanIDStr, status, initiator,
			peer,
		)
		ch <- prometheus.MustNewConstMetric(
			c.commitWeightDesc, prometheus.GaugeValue,
			float64(channel.CommitWeight), chanIDStr, status,
			initiator, peer,
		)
		ch <- prometheus.MustNewConstMetric(
			c.commitFeeDesc, prometheus.GaugeValue,
			float64(channel.CommitFee), chanIDStr, status,
			initiator, peer,
		)

		if chanInfo, ok := channelInfoMap[channel.ChannelID]; ok {
			localPolicy := chanInfo.Node1Policy
			ch <- prometheus.MustNewConstMetric(
				c.localBaseFeeDesc, prometheus.GaugeValue,
				float64(localPolicy.FeeBaseMsat),
				chanIDStr, status, initiator, peer,
			)
			ch <- prometheus.MustNewConstMetric(
				c.localFeeRateDesc, prometheus.GaugeValue,
				float64(localPolicy.FeeRateMilliMsat),
				chanIDStr, status, initiator, peer,
			)
			ch <- prometheus.MustNewConstMetric(
				c.localInboundBaseFeeDesc, prometheus.GaugeValue,
				float64(localPolicy.InboundBaseFeeMsat),
				chanIDStr, status, initiator, peer,
			)
			ch <- prometheus.MustNewConstMetric(
				c.localInboundFeeRateDesc, prometheus.GaugeValue,
				float64(localPolicy.InboundFeeRatePPM),
				chanIDStr, status, initiator, peer,
			)

			remotePolicy := chanInfo.Node2Policy
			ch <- prometheus.MustNewConstMetric(
				c.remoteBaseFeeDesc, prometheus.GaugeValue,
				float64(remotePolicy.FeeBaseMsat),
				chanIDStr, status, initiator, peer,
			)
			ch <- prometheus.MustNewConstMetric(
				c.remoteFeeRateDesc, prometheus.GaugeValue,
				float64(remotePolicy.FeeRateMilliMsat),
				chanIDStr, status, initiator, peer,
			)
			ch <- prometheus.MustNewConstMetric(
				c.remoteInboundBaseFeeDesc, prometheus.GaugeValue,
				float64(remotePolicy.InboundBaseFeeMsat),
				chanIDStr, status, initiator, peer,
			)
			ch <- prometheus.MustNewConstMetric(
				c.remoteInboundFeeRateDesc, prometheus.GaugeValue,
				float64(remotePolicy.InboundFeeRatePPM),
				chanIDStr, status, initiator, peer,
			)
		}

		// Only record uptime if the channel has been monitored.
		if channel.LifeTime != 0 {
			ch <- prometheus.MustNewConstMetric(
				c.channelUptimeDesc, prometheus.GaugeValue,
				float64(channel.Uptime)/float64(channel.LifeTime),
				chanIDStr, status, initiator, peer,
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

	// Preinitialize the map with all possible anchor state labels to avoid
	// "stuck" values when selecting a longer time range.
	limboState := anchorStateToString(lndclient.ForceCloseAnchorStateLimbo)
	recoveredState := anchorStateToString(
		lndclient.ForceCloseAnchorStateRecovered,
	)
	lostState := anchorStateToString(lndclient.ForceCloseAnchorStateLost)
	forceCloseTotal := map[string]btcutil.Amount{
		limboState:     0,
		recoveredState: 0,
		lostState:      0,
	}
	for _, forceClose := range pendingChannelsResp.PendingForceClose {
		// We use the anchor state names to allocate the different
		// balances to a human-readable state. But those balances
		// already include the anchor output value itself.
		forceCloseTotal[limboState] += forceClose.LimboBalance
		forceCloseTotal[recoveredState] += forceClose.RecoveredBalance

		// If we actually lost the anchor output, this isn't properly
		// reflected in the balances, so we just need to account for the
		// list 330 satoshis.
		if forceClose.AnchorState == lndclient.ForceCloseAnchorStateLost {
			forceCloseTotal[lostState] += 330
		}
	}

	for anchorState, balance := range forceCloseTotal {
		ch <- prometheus.MustNewConstMetric(
			c.pendingForceCloseBalanceDesc, prometheus.GaugeValue,
			float64(balance), anchorState,
		)
	}

	var waitingClosetotal btcutil.Amount
	for _, waitingClose := range pendingChannelsResp.WaitingClose {
		waitingClosetotal += waitingClose.LocalBalance
	}
	ch <- prometheus.MustNewConstMetric(
		c.waitingCloseBalanceDesc, prometheus.GaugeValue,
		float64(waitingClosetotal),
	)

	// Get the list of closed channels.
	c.cacheMutex.RLock()
	closedChannelsResp := c.closedChannelsCache
	c.cacheMutex.RUnlock()
	closeCounts := make(map[string]int)
	for _, channel := range closedChannelsResp {
		typeString, ok := closeTypeLabelMap[channel.CloseType]
		if !ok {
			Logger.Warnf("Unrecognized close type: %v", channel.CloseType)
			continue
		}
		closeCounts[typeString]++
	}
	for _, closeType := range closeTypeLabelMap {
		count := closeCounts[closeType]
		ch <- prometheus.MustNewConstMetric(
			c.numClosedChannels, prometheus.GaugeValue,
			float64(count), closeType,
		)
	}

	// Get all remote policies
	remotePolicies, err := c.getRemotePolicies(getInfoResp.IdentityPubkey, nodeInfo)
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

var closeTypeLabelMap = map[lndclient.CloseType]string{
	lndclient.CloseTypeCooperative:      "cooperative",
	lndclient.CloseTypeLocalForce:       "local_force",
	lndclient.CloseTypeRemoteForce:      "remote_force",
	lndclient.CloseTypeBreach:           "breach",
	lndclient.CloseTypeFundingCancelled: "funding_cancelled",
	lndclient.CloseTypeAbandoned:        "abandoned",
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
func (c *ChannelsCollector) getRemotePolicies(pubkey route.Vertex, nodeInfo *lndclient.NodeInfo) (
	map[uint64]*lndclient.RoutingPolicy, error) {

	policies := make(map[uint64]*lndclient.RoutingPolicy)
	for _, i := range nodeInfo.Channels {
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
			policies[i.ChannelID] = policy
		}
	}

	return policies, nil
}
