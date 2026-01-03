package status

import "sync/atomic"

// Registry is the central metrics facade
// Systems cache pointers during init; Update loops write directly to atomics
type Registry struct {
	Bools   *MetricMap[atomic.Bool]
	Ints    *MetricMap[atomic.Int64]
	Floats  *MetricMap[AtomicFloat]
	Strings *MetricMap[AtomicString]
}

// NewRegistry creates an initialized Registry
func NewRegistry() *Registry {
	return &Registry{
		Bools:   NewMetricMap[atomic.Bool](),
		Ints:    NewMetricMap[atomic.Int64](),
		Floats:  NewMetricMap[AtomicFloat](),
		Strings: NewMetricMap[AtomicString](),
	}
}

// TotalCount returns total metrics across all types
// TODO: integrate with overlay, show the count
func (r *Registry) TotalCount() int {
	return r.Bools.Count() + r.Ints.Count() + r.Floats.Count() + r.Strings.Count()
}