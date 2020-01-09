package collectors

import (
	"context"
	"math"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/prometheus/client_golang/prometheus"
)

// WalletCollector is a collector that will export metrics related to lnd's
// on-chain wallet. .
type WalletCollector struct {
	lnd lnrpc.LightningClient

	// We'll use two gauges to keep track of the total number of confirmed
	// and unconfirmed UTXOs.
	numUtxosConfDesc   *prometheus.Desc
	numUtxosUnconfDesc *prometheus.Desc

	// We'll maintain three gauges for the min, max, and average UTXO size.
	minUtxoSizeDesc *prometheus.Desc
	maxUtxoSizeDesc *prometheus.Desc
	avgUtxoSizeDesc *prometheus.Desc

	// Three gauges will be used to keep track of the confirmed, unconfirmed
	// and total balances in the wallet.
	confirmedBalanceDesc   *prometheus.Desc
	unconfirmedBalanceDesc *prometheus.Desc

	// Per-transaction metrics.
	txNumConfsDesc *prometheus.Desc
}

// NewWalletCollector returns a new instance of the WalletCollector.
func NewWalletCollector(lnd lnrpc.LightningClient) *WalletCollector {
	txLabels := []string{"tx_hash"}
	return &WalletCollector{
		lnd: lnd,
		numUtxosConfDesc: prometheus.NewDesc(
			"lnd_utxos_count_confirmed_total",
			"number of all conf utxos", nil, nil,
		),
		numUtxosUnconfDesc: prometheus.NewDesc(
			"lnd_utxos_count_unconfirmed_total",
			"number of all unconf utxos", nil, nil,
		),
		minUtxoSizeDesc: prometheus.NewDesc(
			"lnd_utxos_sizes_min_sat", "smallest UTXO size",
			nil, nil,
		),
		maxUtxoSizeDesc: prometheus.NewDesc(
			"lnd_utxos_sizes_max_sat", "largest UTXO size",
			nil, nil,
		),
		avgUtxoSizeDesc: prometheus.NewDesc(
			"lnd_utxos_sizes_avg_sat", "average UTXO size",
			nil, nil,
		),
		confirmedBalanceDesc: prometheus.NewDesc(
			"lnd_wallet_balance_confirmed_sat",
			"confirmed wallet balance",
			nil, nil,
		),
		unconfirmedBalanceDesc: prometheus.NewDesc(
			"lnd_wallet_balance_unconfirmed_sat",
			"unconfirmed wallet balance",
			nil, nil,
		),
		txNumConfsDesc: prometheus.NewDesc(
			"lnd_tx_num_confs", "number of confs", txLabels, nil,
		),
	}
}

// Describe sends the super-set of all possible descriptors of metrics
// collected by this Collector to the provided channel and returns once the
// last descriptor has been sent.
//
// NOTE: Part of the prometheus.Collector interface.
func (u *WalletCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- u.numUtxosConfDesc
	ch <- u.numUtxosUnconfDesc
	ch <- u.minUtxoSizeDesc
	ch <- u.maxUtxoSizeDesc
	ch <- u.avgUtxoSizeDesc
	ch <- u.confirmedBalanceDesc
	ch <- u.unconfirmedBalanceDesc
	ch <- u.txNumConfsDesc
}

// Collect is called by the Prometheus registry when collecting metrics.
//
// NOTE: Part of the prometheus.Collector interface.
func (u *WalletCollector) Collect(ch chan<- prometheus.Metric) {
	// First, we'll fetch information w.r.t all UTXOs in the wallet. The
	// large max confs value means we'll capture all the UTXOs.
	req := &lnrpc.ListUnspentRequest{
		MaxConfs: math.MaxInt32,
	}
	utxos, err := u.lnd.ListUnspent(context.Background(), req)
	if err != nil {
		walletLogger.Error(err)
		return
	}

	var (
		numConf, numUnconf uint32
		sum, max           int64
		min                int64
	)

	// For each UTXO, we'll count the tally of confirmed vs unconfirmed,
	// and also update the largest and smallest UTXO that we know of.
	for _, utxo := range utxos.Utxos {
		sum += utxo.AmountSat

		switch utxo.Confirmations {
		case 0:
			numUnconf++
		default:
			numConf++
		}

		if utxo.AmountSat > max {
			max = utxo.AmountSat
		}
		if utxo.AmountSat < min || min == 0 {
			min = utxo.AmountSat
		}
	}

	avg := float64(sum) / float64(numConf+numUnconf)

	ch <- prometheus.MustNewConstMetric(
		u.numUtxosConfDesc, prometheus.GaugeValue, float64(numConf),
	)

	ch <- prometheus.MustNewConstMetric(
		u.numUtxosUnconfDesc, prometheus.GaugeValue, float64(numUnconf),
	)

	ch <- prometheus.MustNewConstMetric(
		u.minUtxoSizeDesc, prometheus.GaugeValue, float64(min),
	)

	ch <- prometheus.MustNewConstMetric(
		u.maxUtxoSizeDesc, prometheus.GaugeValue, float64(max),
	)

	ch <- prometheus.MustNewConstMetric(
		u.avgUtxoSizeDesc, prometheus.GaugeValue, float64(avg),
	)

	// Next, we'll query the wallet to determine our confirmed and unconf
	// balance at this instance.
	walletBal, err := u.lnd.WalletBalance(
		context.Background(), &lnrpc.WalletBalanceRequest{},
	)
	if err != nil {
		walletLogger.Error(err)
		return
	}

	ch <- prometheus.MustNewConstMetric(
		u.confirmedBalanceDesc, prometheus.GaugeValue,
		float64(walletBal.ConfirmedBalance),
	)
	ch <- prometheus.MustNewConstMetric(
		u.unconfirmedBalanceDesc, prometheus.GaugeValue,
		float64(walletBal.UnconfirmedBalance),
	)

	getTxsResp, err := u.lnd.GetTransactions(
		context.Background(), &lnrpc.GetTransactionsRequest{},
	)
	if err != nil {
		walletLogger.Error(err)
		return
	}

	for _, tx := range getTxsResp.Transactions {
		ch <- prometheus.MustNewConstMetric(
			u.txNumConfsDesc, prometheus.CounterValue,
			float64(tx.NumConfirmations), tx.TxHash,
		)
	}
}

func init() {
	metricsMtx.Lock()
	collectors["wallet"] = func(lnd lnrpc.LightningClient) prometheus.Collector {
		return NewWalletCollector(lnd)
	}
	metricsMtx.Unlock()
}
