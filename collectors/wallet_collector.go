package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
)

type walletCollector struct {
	lnd                      *lndClient
	totalBalanceDesc         *prometheus.Desc
	confirmedBalanceDesc     *prometheus.Desc
	wellConfirmedBalanceDesc *prometheus.Desc

	txCountDesc       *prometheus.Desc
	feeTotalDesc      *prometheus.Desc
	satsRecvTotalDesc *prometheus.Desc
	satsSentTotalDesc *prometheus.Desc
}
