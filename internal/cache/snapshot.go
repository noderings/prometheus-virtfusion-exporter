package cache

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// Snapshot holds a thread-safe set of pre-collected metric families
// that the /metrics handler reads without calling VirtFusion.
type Snapshot struct {
	mu       sync.RWMutex
	families []*dto.MetricFamily
}

func NewSnapshot() *Snapshot {
	return &Snapshot{}
}

// Update replaces the entire snapshot atomically.
func (s *Snapshot) Update(families []*dto.MetricFamily) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.families = families
}

// Gather implements prometheus.Gatherer so the snapshot can be served
// alongside default process/go metrics.
func (s *Snapshot) Gather() ([]*dto.MetricFamily, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*dto.MetricFamily, len(s.families))
	copy(out, s.families)
	return out, nil
}

// Gatherers returns a combined Gatherer that merges the snapshot with
// the default process/go collectors.
func (s *Snapshot) Gatherers() prometheus.Gatherers {
	return prometheus.Gatherers{
		prometheus.DefaultGatherer,
		s,
	}
}
