package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
)

type nodeCollector struct {
	lnd                         *lndClient
	nodeCountDesc               *prometheus.Desc
	nodeAddrCountDesc           *prometheus.Desc
	nodeAddrCountByProtocolDesc *prometheus.Desc
	nodeWithoutAddrCountDesc    *prometheus.Desc
}
