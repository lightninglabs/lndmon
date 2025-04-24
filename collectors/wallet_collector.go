package collectors

import (
	"context"
	"fmt"
	"math"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightninglabs/lndclient"
	"github.com/prometheus/client_golang/prometheus"
)

// WalletCollector is a collector that will export metrics related to lnd's
// on-chain wallet. .
type WalletCollector struct {
	lnd *lndclient.LndServices

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

	// We'll use one counter to keep track of both internal and external key
	// count.
	keyCountDesc *prometheus.Desc

	// errChan is a channel that we send any errors that we encounter into.
	// This channel should be buffered so that it does not block sends.
	errChan chan<- error
}

// NewWalletCollector returns a new instance of the WalletCollector.
func NewWalletCollector(lnd *lndclient.LndServices,
	errChan chan<- error) *WalletCollector {

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
		keyCountDesc: prometheus.NewDesc(
			"lnd_wallet_key_count", "wallet key count",
			[]string{
				"account_name",
				"address_type",
				"derivation_path",
				"key_type",
			}, nil,
		),

		errChan: errChan,
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
	ch <- u.keyCountDesc
}

// Collect is called by the Prometheus registry when collecting metrics.
//
// NOTE: Part of the prometheus.Collector interface.
func (u *WalletCollector) Collect(ch chan<- prometheus.Metric) {
	// First, we'll fetch information w.r.t all UTXOs in the wallet. The
	// large max confs value means we'll capture all the UTXOs.
	utxos, err := u.lnd.WalletKit.ListUnspent(
		context.Background(), 0, math.MaxInt32,
	)
	if err != nil {
		u.errChan <- fmt.Errorf("WalletCollector ListUnspent failed "+
			"with: %v", err)
		return
	}

	var (
		numConf, numUnconf  uint32
		sum, maxAmt, minAmt btcutil.Amount
	)

	// For each UTXO, we'll count the tally of confirmed vs unconfirmed,
	// and also update the largest and smallest UTXO that we know of.
	for _, utxo := range utxos {
		sum += utxo.Value

		switch utxo.Confirmations {
		case 0:
			numUnconf++
		default:
			numConf++
		}

		if utxo.Value > maxAmt {
			maxAmt = utxo.Value
		}
		if utxo.Value < minAmt || minAmt == 0 {
			minAmt = utxo.Value
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
		u.minUtxoSizeDesc, prometheus.GaugeValue, float64(minAmt),
	)

	ch <- prometheus.MustNewConstMetric(
		u.maxUtxoSizeDesc, prometheus.GaugeValue, float64(maxAmt),
	)

	ch <- prometheus.MustNewConstMetric(
		u.avgUtxoSizeDesc, prometheus.GaugeValue, avg,
	)

	// Next, we'll query the wallet to determine our confirmed and unconf
	// balance at this instance.
	walletBal, err := u.lnd.Client.WalletBalance(context.Background())
	if err != nil {
		u.errChan <- fmt.Errorf("WalletCollector WalletBalance "+
			"failed with: %v", err)
		return
	}

	ch <- prometheus.MustNewConstMetric(
		u.confirmedBalanceDesc, prometheus.GaugeValue,
		float64(walletBal.Confirmed),
	)
	ch <- prometheus.MustNewConstMetric(
		u.unconfirmedBalanceDesc, prometheus.GaugeValue,
		float64(walletBal.Unconfirmed),
	)

	accounts, err := u.lnd.WalletKit.ListAccounts(context.Background(), "", 0)
	if err != nil {
		u.errChan <- fmt.Errorf("WalletCollector ListAccounts"+
			"failed with: %v", err)
		return
	}

	for _, account := range accounts {
		name := account.GetName()
		addrType := account.GetAddressType().String()
		path := account.GetDerivationPath()

		// internal key count.
		if account.InternalKeyCount > 0 {
			ch <- prometheus.MustNewConstMetric(
				u.keyCountDesc, prometheus.CounterValue,
				float64(account.InternalKeyCount),
				name, addrType, path, "internal",
			)
		}

		// external key count.
		if account.ExternalKeyCount > 0 {
			ch <- prometheus.MustNewConstMetric(
				u.keyCountDesc, prometheus.CounterValue,
				float64(account.ExternalKeyCount),
				name, addrType, path, "external",
			)
		}
	}
}
