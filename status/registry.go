package status

import (
	"sync"
	"sync/atomic"
)

// Registry is the central metrics facade
// Systems cache pointers during init; Update loops write directly to atomics
// Also owns periodic snapshot emission: see snapshot.go
type Registry struct {
	Bools   *MetricMap[atomic.Bool]
	Ints    *MetricMap[atomic.Int64]
	Floats  *MetricMap[AtomicFloat]
	Strings *MetricMap[AtomicString]

	// Snapshot state
	snapEvery atomic.Uint64 // tick period, 0 disables
	idxMu     sync.Mutex
	idxGen    uint64
	idx       []statGroup
}

// NewRegistry creates an initialized Registry with snapshots disabled
func NewRegistry() *Registry {
	return &Registry{
		Bools:   NewMetricMap[atomic.Bool](),
		Ints:    NewMetricMap[atomic.Int64](),
		Floats:  NewMetricMap[AtomicFloat](),
		Strings: NewMetricMap[AtomicString](),
	}
}

// TotalCount returns total metrics across all types
func (r *Registry) TotalCount() int {
	return r.Bools.Count() + r.Ints.Count() + r.Floats.Count() + r.Strings.Count()
}

