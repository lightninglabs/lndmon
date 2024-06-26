package collectors

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lightninglabs/lndclient"
	"github.com/prometheus/client_golang/prometheus"
)

type StateCollector struct {
	lnd *lndclient.LndServices

	// Use one gauge to track the starting time of LND.
	timeToStartDesc *prometheus.Desc

	// startTime records a best-effort timestamp of when LND was started.
	startTime time.Time

	// endTime records when LND makes a transition from RPC_ACTIVE to
	// SERVER_ACTIVE.
	// If lndmon starts after LND has already reached SERVER_ACTIVE, no
	// startup time metric will be emitted.
	endTime time.Time

	// mutex is a lock for preventing concurrent writes to startTime or
	// endTime.
	mutex sync.RWMutex

	// errChan is a channel that we send any errors that we encounter into.
	// This channel should be buffered so that it does not block sends.
	errChan chan<- error
}

// NewStateCollector returns a new instance of the StateCollector.
func NewStateCollector(lnd *lndclient.LndServices,
	errChan chan<- error) *StateCollector {

	sc := &StateCollector{
		lnd: lnd,
		timeToStartDesc: prometheus.NewDesc(
			"lnd_time_to_start_secs",
			"time to start in seconds",
			nil, nil,
		),
		startTime: time.Now(),
		errChan:   errChan,
	}

	go sc.monitorStateChanges()
	return sc
}

// monitorStateChanges checks the state every second to catch fast transitions.
func (s *StateCollector) monitorStateChanges() {
	var serverActiveReached bool

	for {
		state, err := s.lnd.State.GetState(context.Background())
		if err != nil {
			s.errChan <- fmt.Errorf("StateCollector GetState failed with: %v", err)
			continue
		}

		s.mutex.Lock()
		if state == lndclient.WalletStateRPCActive && !s.startTime.IsZero() {
			s.endTime = time.Now()
			serverActiveReached = true
		}
		s.mutex.Unlock()

		if serverActiveReached {
			break
		}
		time.Sleep(1 * time.Second)
	}
}

// Describe sends the super-set of all possible descriptors of metrics
// collected by this Collector to the provided channel and returns once the
// last descriptor has been sent.
//
// NOTE: Part of the prometheus.Collector interface.
func (s *StateCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- s.timeToStartDesc
}

// Collect is called by the Prometheus registry when collecting metrics.
//
// NOTE: Part of the prometheus.Collector interface.
func (s *StateCollector) Collect(ch chan<- prometheus.Metric) {
	// Lock for read
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	// We have set both a startTime and endTime, calculate the difference and emit a metric.
	if !s.startTime.IsZero() && !s.endTime.IsZero() {
		timeToStartInSecs := s.endTime.Sub(s.startTime).Seconds()
		ch <- prometheus.MustNewConstMetric(
			s.timeToStartDesc, prometheus.GaugeValue, timeToStartInSecs,
		)
	}
}
