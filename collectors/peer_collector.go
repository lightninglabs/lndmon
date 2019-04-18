package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
)

type peerCollector struct {
	lnd           *lndClient
	peerCountDesc *prometheus.Desc
	pingTimeDesc  *prometheus.Desc
	satSentDesc   *prometheus.Desc
	bytesSentDesc *prometheus.Desc
	bytesRecvDesc *prometheus.Desc
}
