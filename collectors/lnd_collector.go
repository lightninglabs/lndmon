package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
)

type lndCollector struct {
	lnd               *lndClient
	versionDesc       *prometheus.Desc
	uptimeDesc        *prometheus.Desc
	syncedToChainDesc *prometheus.Desc
}
