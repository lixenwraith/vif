package status

import (
	"sort"
	"sync"
	"sync/atomic"
)

// MetricMap is a thread-safe registry for metrics of type T
// Registration uses mutex; cached pointer access is lock-free
type MetricMap[T any] struct {
	mu     sync.RWMutex
	items  map[string]*T
	keys   []string // sorted view, rebuilt lazily after registration
	sorted bool

	gen atomic.Uint64 // bumped per new key; invalidates cached views
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
	m.sorted = false
	m.gen.Add(1)
	return ptr
}

// Has returns true if the key exists
func (m *MetricMap[T]) Has(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.items[key]
	return ok
}

// Gen returns the registration counter; a change invalidates cached key views
func (m *MetricMap[T]) Gen() uint64 { return m.gen.Load() }

// Keys returns the sorted key list. The slice is shared: callers must not
// mutate it. A later registration replaces it rather than sorting in place,
// so a previously returned slice stays valid.
func (m *MetricMap[T]) Keys() []string {
	m.mu.RLock()
	if m.sorted {
		keys := m.keys
		m.mu.RUnlock()
		return keys
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.sorted {
		keys := make([]string, 0, len(m.items))
		for k := range m.items {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		m.keys, m.sorted = keys, true
	}
	return m.keys
}

// Range iterates over all metrics in sorted key order
// Callback receives the pointer; caller reads atomic value from it
// fn MUST NOT register metrics: the read lock is held for the whole walk
func (m *MetricMap[T]) Range(fn func(key string, ptr *T)) {
	keys := m.Keys()

	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, k := range keys {
		if ptr, ok := m.items[k]; ok {
			fn(k, ptr)
		}
	}
}

// Count returns the number of registered metrics
func (m *MetricMap[T]) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.items)
}
