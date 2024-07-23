package collectors

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lightninglabs/lndclient"
	"github.com/prometheus/client_golang/prometheus"
)

// StateCollector is a collector that keeps track of LND's state.
type StateCollector struct {
	lnd *lndclient.LndServices

	// timeToStartDesc is a gauge to track time from unlocked to started of LND.
	timeToStartDesc *prometheus.Desc

	// timeToUnlockDesc is a gauge to track the time to unlock of LND.
	timeToUnlockDesc *prometheus.Desc

	// programStartTime records a best-effort timestamp of when lndmon was started.
	programStartTime time.Time

	// unlockTime records a best-effort timestamp of when LND was unlocked.
	unlockTime time.Time

	// endTime records when LND makes a transition from UNLOCKED to
	// SERVER_ACTIVE.
	// If lndmon starts after LND has already reached SERVER_ACTIVE, no
	// startup time metric will be emitted.
	endTime time.Time

	// mutex is a lock for preventing concurrent writes to unlockTime or
	// endTime.
	mutex sync.RWMutex

	// errChan is a channel that we send any errors that we encounter into.
	// This channel should be buffered so that it does not block sends.
	errChan chan<- error
}

// NewStateCollector returns a new instance of the StateCollector.
func NewStateCollector(lnd *lndclient.LndServices,
	errChan chan<- error, programStartTime time.Time) *StateCollector {

	sc := &StateCollector{
		lnd: lnd,
		timeToStartDesc: prometheus.NewDesc(
			"lnd_time_to_start_millisecs",
			"time to start in milliseconds",
			nil, nil,
		),
		timeToUnlockDesc: prometheus.NewDesc(
			"lnd_time_to_unlock_millisecs",
			"time to unlocked in milliseconds",
			nil, nil,
		),
		programStartTime: programStartTime,
		unlockTime:       time.Now(),
		errChan:          errChan,
	}

	go sc.monitorStateChanges()
	return sc
}

// monitorStateChanges checks the state every second to catch fast transitions.
func (s *StateCollector) monitorStateChanges() {
	var serverActiveReached bool

	stateUpdates, errChan, err := s.lnd.State.SubscribeState(context.Background())
	if err != nil {
		s.errChan <- fmt.Errorf("StateCollector SubscribeState failed with: %v", err)
		return
	}

	for {
		select {
		case state := <-stateUpdates:
			s.mutex.Lock()
			if state == lndclient.WalletStateServerActive && !s.unlockTime.IsZero() {
				s.endTime = time.Now()
				serverActiveReached = true
			}
			s.mutex.Unlock()

			if serverActiveReached {
				return
			}

		case err := <-errChan:
			s.errChan <- fmt.Errorf("StateCollector state update failed with: %v", err)
			return
		}
	}
}

// Describe sends the super-set of all possible descriptors of metrics
// collected by this Collector to the provided channel and returns once the
// last descriptor has been sent.
//
// NOTE: Part of the prometheus.Collector interface.
func (s *StateCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- s.timeToStartDesc
	ch <- s.timeToUnlockDesc
}

// Collect is called by the Prometheus registry when collecting metrics.
//
// NOTE: Part of the prometheus.Collector interface.
func (s *StateCollector) Collect(ch chan<- prometheus.Metric) {
	// Lock for read
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	// We have set unlockTime and endTime.
	// Calculate the differences and emit a metric.
	if !s.unlockTime.IsZero() && !s.endTime.IsZero() {
		timeToUnlockInMSecs := s.unlockTime.Sub(s.programStartTime).Milliseconds()
		timeToStartInMSecs := s.endTime.Sub(s.unlockTime).Milliseconds()
		ch <- prometheus.MustNewConstMetric(
			s.timeToStartDesc, prometheus.GaugeValue, float64(timeToStartInMSecs),
		)

		ch <- prometheus.MustNewConstMetric(
			s.timeToUnlockDesc, prometheus.GaugeValue, float64(timeToUnlockInMSecs),
		)
	}
}
