## Chain Metrics
* `lnd_chain_block_height`: best block height from lnd
* `lnd_chain_block_timestamp`: best block timestamp from lnd
* `lnd_synced_to_chain`: whether lnd is synced to chain
* `lnd_synced_to_graph`: whether lnd is synced to graph

## Channel Metrics
* `lnd_channels_open_balance_sat`: total balance of channels in satoshis
* `lnd_channels_pending_balance_sat`: total balance of all pending channels in satoshis
* `lnd_channels_bandwidth_incoming_sat`: total available incoming channel bandwidth within this channel
* `lnd_channels_bandwidth_outgoing_sat`: total available outgoing channel bandwidth within this channel
* `lnd_channels_pending_htlc_count`: total number of pending active HTLCs within this channel
* `lnd_channels_active_total`: total number of active channels
* `lnd_channels_inactive_total`: total number of inactive channels
* `lnd_channels_pending_total`: total number of inactive channels
* `lnd_channels_csv_delay`: CSV delay in relative blocks for this channel
* `lnd_channels_unsettled_balance`: unsettled balance in this channel
* `lnd_channels_fee_per_kw`: required number of sat per kiloweight that the requester will pay for the funding and commitment transaction
* `lnd_channels_commit_weight`: weight of the commitment transaction
* `lnd_channels_commit_fee`: weight of the commitment transaction
* `lnd_channels_sent_sat`: total number of satoshis we’ve sent within this channel
* `lnd_channels_received_sat`: total number of satoshis we’ve received within this channel
* `lnd_channels_updates_count`: total number of updates conducted within this channel
  
## Graph Metrics
* `lnd_graph_edges_count`: total number of edges in the graph
* `lnd_graph_nodes_count`: total number of nodes in the graph
* `lnd_graph_outdegree_avg`: the avg out degreee of nodes in the network
* `lnd_graph_outdegree_max`: the max out degree of nodes in the network
* `lnd_graph_chan_capacity_sat`: the total capacity of the network in satoshis
* `lnd_graph_chan_size_avg`: the avg channel size in the network
* `lnd_graph_chan_size_min`: the smallest channel in the network
* `lnd_graph_chan_size_max`: the largest channel in the network
* `lnd_graph_chan_size_median`: the median channel size in the network
* `lnd_graph_timelock_delta_{min, max, avg, median}`: the min/max/avg/median time lock delta across all chanenls
* `lnd_graph_min_htlc_msat_{min, max, avg, median}`: the min/max/avg/median min htlc across all channels
* `lnd_graph_fee_base_msat_{min, max, avg, median}`: the min/max/avg/median base fee across all channels
* `lnd_graph_fee_rate_msat_{min, max, avg, median}`: the min/max/avg/median fee rate across all channels
* `lnd_graph_max_htlc_msat_{min, max, avg, median}`: the min/max/avg/median max htlc across all channels
 
## Peer Metrics
* `lnd_peer_count`: total number of peers
* `lnd_peer_ping_time_microsecond`: ping time for this peer in microseconds
* `lnd_peer_sent_sat`: satoshis sent to this peer
* `lnd_peer_recv_sat`: satoshis received from this peer
* `lnd_peer_sent_byte`: bytes transmitted to this peer
* `lnd_peer_recv_byte`: bytes transmitted from this peer
  
  
## Payment Metrics
* `lnd_total_payments`: total number of payments sent, labeled by final status (`succeeded`/`failed`)
* `lnd_total_payments_sat`: total volume of payments sent in satoshis, labeled by final status (`succeeded`/`failed`)
* `lnd_total_payments_fees_sat`: total routing fees paid for payments sent, in satoshis, labeled by final status (only `succeeded` accrues a non-zero fee in practice)
* `lnd_total_htlc_attempts`: total number of HTLC attempts across all payments, labeled by final payment status
* `lnd_payment_attempts_per_payment`: histogram of the number of attempts per payment
* `lnd_payment_duration_seconds`: histogram of the time taken for a payment to reach a terminal state, labeled by final status; use `histogram_quantile` for median/percentile speed, or `rate(..._sum)/rate(..._count)` for average speed
* `lnd_payment_num_hops`: histogram of the number of hops in the route of a payment's terminal HTLC attempt, labeled by final status; use `histogram_quantile` for median hop count, or `rate(..._sum)/rate(..._count)` for average hop count

With `--seedmetrics`, payment volume, fees, duration, and hop histograms include
retained terminal payments from before exporter startup. Live updates do not
count seeded payments twice. Deleted records and overwritten failed retries
cannot be restored. Existing payment count and attempt metrics remain live-only.
Payment volumes and fees preserve fractional satoshis from LND's millisatoshi
fields. Duration uses the last resolved successful shard for multipart payments.
Hop count describes that selected shard, not the sum of all shard routes.

## Invoice Metrics

* `lnd_invoices`: number of stored invoices, grouped by LND's reported current state (`open`/`accepted`/`settled`/`canceled`/`unknown`).
* `lnd_invoices_sat`: amount of stored invoices in satoshis, grouped by current state. Settled invoices use the amount received. Other states use the requested amount.

These metrics are gauges. Each invoice contributes to one state per refresh.
They load before the metrics endpoint starts and refresh every 60 seconds.
Complete scans replace the previous values together. An RPC failure stops the
exporter instead of publishing a partial scan. A scan contains invoices through
a fixed add index; state changes during pagination appear on the next refresh.

Gauges can decrease when invoices change state or records are deleted. Do not
use `rate()` or `increase()` as invoice throughput queries. LND's global invoice
subscription does not emit accepted or canceled transitions, and stored records
cannot reconstruct all historical state transitions.

An Atomic Multi-Path (AMP) invoice counts once, including when it receives
multiple payments. Its settled amount includes the aggregate amount received.
LND reports it as settled after a set settles, although the invoice may accept
additional payments. Fractional satoshi amounts are preserved.

## Wallet Metrics
* `lnd_utxos_count_confirmed_total`: number of all conf utxos
* `lnd_utxos_count_unconfirmed_total`: number of all unconf utxos
* `lnd_utxos_sizes_min_sat`: smallest UTXO size
* `lnd_utxos_sizes_max_sat`: largest UTXO size
* `lnd_utxos_sizes_avg_sat`: average UTXO size
* `lnd_wallet_balance_confirmed_sat`: confirmed wallet balance
* `lnd_wallet_balance_unconfirmed_sat`: unconfirmed wallet balance
* `lnd_tx_num_confs`: number of confs
