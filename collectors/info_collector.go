package collectors

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

// InfoCollector is a collector that keeps track of node information.
type InfoCollector struct {
	info *prometheus.Desc
}

// NewInfoCollector returns a new instance of the InfoCollector.
func NewInfoCollector() *InfoCollector {
	labels := []string{"version", "alias", "pubkey"}
	return &InfoCollector{
		info: prometheus.NewDesc(
			"lnd_info", "lnd node info", labels, nil,
		),
	}
}

// Describe sends the super-set of all possible descriptors of metrics
// collected by this Collector to the provided channel and returns once the
// last descriptor has been sent.
//
// NOTE: Part of the prometheus.Collector interface.
func (c *InfoCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.info
}

// Name identifies this collector in the scrape outcome log.
func (c *InfoCollector) Name() string {
	return "info"
}

// Collect gathers the node info metric using the scrape's context so an
// abandoned scrape cancels the in-flight lnd call. The error (if any) is
// returned for the handler to classify and log.
//
// NOTE: Part of the scrapeCollector interface.
func (c *InfoCollector) Collect(ctx context.Context,
	ch chan<- prometheus.Metric) error {

	// getInfo shares a single GetInfo RPC across the chain, info and channels
	// collectors for this scrape (see infoProvider).
	resp, err := getInfo(ctx)
	if err != nil {
		return fmt.Errorf("InfoCollector GetInfo failed with: %w", err)
	}

	ch <- prometheus.MustNewConstMetric(
		c.info, prometheus.GaugeValue, 0, resp.Version,
		resp.Alias, hex.EncodeToString(resp.IdentityPubkey[:]),
	)

	return nil
}
