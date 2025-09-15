package collectors

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/lightninglabs/lndclient"
	"github.com/prometheus/client_golang/prometheus"
)

// WtClientCollector is a collector that will export watchtower client-related
// metrics.
type WtClientCollector struct {
	lnd *lndclient.LndServices

	numBackupsDesc        *prometheus.Desc
	numPendingBackupsDesc *prometheus.Desc

	// errChan is a channel that we send any errors that we encounter into.
	// This channel should be buffered so that it does not block sending.
	errChan chan<- error
}

// NewWtClientCollector returns a new instance of the WalletCollector.
func NewWtClientCollector(lnd *lndclient.LndServices,
	errChan chan<- error) *WtClientCollector {

	return &WtClientCollector{
		lnd: lnd,
		numBackupsDesc: prometheus.NewDesc(
			"lnd_wt_client_num_backups",
			"watchtower client number of backups",
			[]string{
				"tower_pubkey",
			}, nil,
		),
		numPendingBackupsDesc: prometheus.NewDesc(
			"lnd_wt_client_num_pending_backups",
			"watchtower client number of pending backups",
			[]string{
				"tower_pubkey",
			}, nil,
		),

		errChan: errChan,
	}
}

// Describe sends the superset of all possible descriptors of metrics
// collected by this Collector to the provided channel and returns once the
// last descriptor has been sent.
//
// NOTE: Part of the prometheus.Collector interface.
func (c *WtClientCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.numBackupsDesc
	ch <- c.numPendingBackupsDesc
}

// Collect is called by the Prometheus registry when collecting metrics.
//
// NOTE: Part of the prometheus.Collector interface.
func (c *WtClientCollector) Collect(ch chan<- prometheus.Metric) {
	towers, err := c.lnd.WtClient.ListTowers(
		context.Background(), true, false,
	)
	if err != nil {
		// If the watchtower client is not active, we'll just return.
		if strings.Contains(
			err.Error(), "watchtower client not active",
		) {

			watchtowerLogger.Debug("Watchtower client not active")
			return
		}

		c.errChan <- fmt.Errorf("WtClientCollector ListTowers failed "+
			"with: %v", err)
		return
	}

	for _, tower := range towers {
		if tower == nil {
			continue
		}
		var (
			pubkey            = hex.EncodeToString(tower.Pubkey)
			numBackups        uint32
			numPendingBackups uint32
		)
		for _, sessionInfo := range tower.SessionInfo {
			for _, session := range sessionInfo.Sessions {
				numBackups += session.NumBackups
				numPendingBackups += session.NumPendingBackups
			}
		}

		ch <- prometheus.MustNewConstMetric(
			c.numBackupsDesc, prometheus.GaugeValue,
			float64(numBackups), pubkey,
		)
		ch <- prometheus.MustNewConstMetric(
			c.numPendingBackupsDesc, prometheus.GaugeValue,
			float64(numPendingBackups), pubkey,
		)
	}
}
