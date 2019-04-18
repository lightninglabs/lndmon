package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
)

type invoicesCollector struct {
	lnd               *lndClient
	invoicesCountDesc *prometheus.Desc
	pendingCountDesc  *prometheus.Desc
	settledCountDesc  *prometheus.Desc
}
