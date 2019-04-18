package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
)

type graphCollector struct {
	lnd           *lndClient
	edgeCountDesc *prometheus.Desc
	feesDesc      *prometheus.Desc // histogram of fees
}
