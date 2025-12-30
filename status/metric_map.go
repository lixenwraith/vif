// @lixen: #dev{feature[shield(render,system)],feature[spirit(render,system)]}
package status

import (
	"sort"
	"sync"
)

// MetricMap is a thread-safe registry for metrics of type T
// Registration uses mutex; cached pointer access is lock-free
type MetricMap[T any] struct {
	mu    sync.RWMutex
	items map[string]*T
}

// NewMetricMap creates an initialized MetricMap
func NewMetricMap[T any]() *MetricMap[T] {
	return &MetricMap[T]{
		items: make(map[string]*T),
	}
}

// Get returns the metric pointer for key, creating if absent
// First call for a key allocates; subsequent calls return cached pointer
func (m *MetricMap[T]) Get(key string) *T {
	// Fast path: RLock check
	m.mu.RLock()
	if ptr, ok := m.items[key]; ok {
		m.mu.RUnlock()
		return ptr
	}
	m.mu.RUnlock()

	// Slow path: Lock and create
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if ptr, ok := m.items[key]; ok {
		return ptr
	}

	ptr := new(T)
	m.items[key] = ptr
	return ptr
}

// Has returns true if the key exists
func (m *MetricMap[T]) Has(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.items[key]
	return ok
}

// Range iterates over all metrics in sorted key order
// Callback receives the pointer; caller reads atomic value from it
func (m *MetricMap[T]) Range(fn func(key string, ptr *T)) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.items) == 0 {
		return
	}

	// Collect and sort keys for deterministic iteration
	keys := make([]string, 0, len(m.items))
	for k := range m.items {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		fn(k, m.items[k])
	}
}

// Count returns the number of registered metrics
func (m *MetricMap[T]) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.items)
}