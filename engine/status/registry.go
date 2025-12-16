package status

import "sync/atomic"

// Registry is the central metrics facade
// Systems cache pointers during init; Update loops write directly to atomics
type Registry struct {
	Bools  *MetricMap[atomic.Bool]
	Ints   *MetricMap[atomic.Int64]
	Floats *MetricMap[AtomicFloat]
}

// NewRegistry creates an initialized Registry
func NewRegistry() *Registry {
	return &Registry{
		Bools:  NewMetricMap[atomic.Bool](),
		Ints:   NewMetricMap[atomic.Int64](),
		Floats: NewMetricMap[AtomicFloat](),
	}
}

// TotalCount returns total metrics across all types
func (r *Registry) TotalCount() int {
	return r.Bools.Count() + r.Ints.Count() + r.Floats.Count()
}