package collectors

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/lightninglabs/lndclient"
	"github.com/prometheus/client_golang/prometheus"
)

// PeerCollector is a collector that keeps track of peer information.
type PeerCollector struct {
	peerCountDesc *prometheus.Desc

	pingTimeDesc *prometheus.Desc

	satSentDesc *prometheus.Desc
	satRecvDesc *prometheus.Desc

	bytesSentDesc *prometheus.Desc
	bytesRecvDesc *prometheus.Desc

	lnd lndclient.LightningClient

	// errChan is a channel that we send any errors that we encounter into.
	// This channel should be buffered so that it does not block sends.
	errChan chan<- error
}

// NewPeerCollector returns a new instance of the PeerCollector for the target
// lnd client.
func NewPeerCollector(lnd lndclient.LightningClient,
	errChan chan<- error) *PeerCollector {

	perPeerLabels := []string{"pubkey"}
	return &PeerCollector{
		peerCountDesc: prometheus.NewDesc(
			"lnd_peer_count", "total number of peers",
			nil, nil,
		),
		pingTimeDesc: prometheus.NewDesc(
			"lnd_peer_ping_time_microsecond",
			"ping time for this peer in microseconds",
			perPeerLabels, nil,
		),
		satSentDesc: prometheus.NewDesc(
			"lnd_peer_sent_sat", "satoshis sent to this peer",
			perPeerLabels, nil,
		),
		satRecvDesc: prometheus.NewDesc(
			"lnd_peer_recv_sat", "satoshis received from this peer",
			perPeerLabels, nil,
		),
		bytesSentDesc: prometheus.NewDesc(
			"lnd_peer_sent_byte", "bytes transmitted to this peer",
			perPeerLabels, nil,
		),
		bytesRecvDesc: prometheus.NewDesc(
			"lnd_peer_recv_byte",
			"bytes transmitted from this peer",
			perPeerLabels, nil,
		),
		lnd:     lnd,
		errChan: errChan,
	}
}

// Describe sends the super-set of all possible descriptors of metrics
// collected by this Collector to the provided channel and returns once the
// last descriptor has been sent.
//
// NOTE: Part of the prometheus.Collector interface.
func (p *PeerCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- p.peerCountDesc

	ch <- p.pingTimeDesc

	ch <- p.satSentDesc
	ch <- p.satRecvDesc

	ch <- p.bytesSentDesc
	ch <- p.bytesRecvDesc
}

// Collect is called by the Prometheus registry when collecting metrics.
//
// NOTE: Part of the prometheus.Collector interface.
func (p *PeerCollector) Collect(ch chan<- prometheus.Metric) {
	listPeersResp, err := p.lnd.ListPeers(context.Background())
	if err != nil {
		p.errChan <- fmt.Errorf("PeerCollector ListPeers failed with: "+
			"%v", err)
		return
	}

	ch <- prometheus.MustNewConstMetric(
		p.peerCountDesc, prometheus.CounterValue,
		float64(len(listPeersResp)),
	)

	for _, peer := range listPeersResp {
		pubkeyStr := hex.EncodeToString(peer.Pubkey[:])

		ch <- prometheus.MustNewConstMetric(
			p.pingTimeDesc, prometheus.CounterValue,
			float64(peer.PingTime), pubkeyStr,
		)
		ch <- prometheus.MustNewConstMetric(
			p.satSentDesc, prometheus.GaugeValue,
			float64(peer.Sent), pubkeyStr,
		)
		ch <- prometheus.MustNewConstMetric(
			p.satRecvDesc, prometheus.GaugeValue,
			float64(peer.Received), pubkeyStr,
		)
		ch <- prometheus.MustNewConstMetric(
			p.bytesSentDesc, prometheus.GaugeValue,
			float64(peer.BytesSent), pubkeyStr,
		)
		ch <- prometheus.MustNewConstMetric(
			p.bytesRecvDesc, prometheus.GaugeValue,
			float64(peer.BytesReceived), pubkeyStr,
		)
	}
}
