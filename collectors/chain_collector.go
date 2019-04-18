package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
)

type chainCollector struct {
	lnd                      *lndClient
	blockHeightDesc          *prometheus.Desc
	bestHeaderTimestasmpDesc *prometheus.Desc
}
