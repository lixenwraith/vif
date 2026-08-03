package service

import (
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// Hub is the runtime container for service instances
// Manages lifecycle and provides type-safe access
type Hub struct {
	services    map[string]Service
	sorted      []string // Topological order, computed on InitAll
	initialized []string // completed Init(), for teardown
	started     []string // Services that completed Start(), for rollback
	mu          sync.RWMutex
}

// NewHub creates an empty service hub
func NewHub() *Hub {
	return &Hub{
		services: make(map[string]Service),
	}
}

// Register adds a service instance to the hub
// Clears cached sort order to force recomputation
func (h *Hub) Register(svc Service) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	name := svc.Name()
	if _, exists := h.services[name]; exists {
		return fmt.Errorf("service already registered: %s", name)
	}

	h.services[name] = svc
	h.sorted = nil // Invalidate cached order
	return nil
}

// Get retrieves a service by name
func (h *Hub) Get(name string) (Service, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	svc, ok := h.services[name]
	return svc, ok
}

// MustGet retrieves a service and casts to type T
// Panics if service not found or type mismatch
func MustGet[T any](h *Hub, name string) T {
	h.mu.RLock()
	svc, ok := h.services[name]
	h.mu.RUnlock()

	if !ok {
		panic(fmt.Sprintf("service not found: %s", name))
	}

	typed, ok := svc.(T)
	if !ok {
		panic(fmt.Sprintf("service %s: type mismatch, got %T", name, svc))
	}
	return typed
}

// InitAll resolves dependencies and calls Init on all services
// On failure, calls Stop on already-initialized services in reverse order
func (h *Hub) InitAll() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Compute topological order if not cached
	if h.sorted == nil {
		order, err := h.topologicalSort()
		if err != nil {
			return err
		}
		h.sorted = order
	}

	// Initialize in dependency order
	var initialized []string
	for _, name := range h.sorted {
		svc := h.services[name]
		start := time.Now()
		if err := svc.Init(); err != nil {
			vlog.Error("service", "msg", "init failed", "service", name, "error", err.Error())
			// Rollback: stop already-initialized in reverse order
			for i := len(initialized) - 1; i >= 0; i-- {
				h.services[initialized[i]].Stop()
			}
			return fmt.Errorf("service %s init failed: %w", name, err)
		}
		vlog.Info("service", "msg", "init", "service", name, "ms", time.Since(start).Milliseconds())
		initialized = append(initialized, name)
	}
	h.initialized = initialized

	return nil
}

// StartAll calls Start on all services in topological order
// On failure, calls Stop on already-started services in reverse order
func (h *Hub) StartAll() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.started = nil

	for _, name := range h.sorted {
		svc := h.services[name]
		start := time.Now()
		if err := svc.Start(); err != nil {
			vlog.Error("service", "msg", "start failed", "service", name, "error", err.Error())
			for _, serviceID := range slices.Backward(h.started) {
				h.services[serviceID].Stop()
			}
			return fmt.Errorf("service %s start failed: %w", name, err)
		}
		vlog.Info("service", "msg", "start", "service", name, "ms", time.Since(start).Milliseconds())
		h.started = append(h.started, name)
	}

	return nil
}

// StopAll calls Stop on all started services in reverse topological order
// Logs errors but does not fail - ensures all services get Stop called
func (h *Hub) StopAll() {
	h.mu.Lock()
	defer h.mu.Unlock()

	// initialized is a superset of started and Stop is contractually
	// idempotent, so one reverse pass releases everything Init acquired.
	for _, name := range slices.Backward(h.initialized) {
		svc, ok := h.services[name]
		if !ok {
			continue
		}
		if err := svc.Stop(); err != nil {
			vlog.Error("service", "msg", "stop failed", "service", name, "error", err.Error())
			continue
		}
		vlog.Info("service", "msg", "stop", "service", name)
	}
	h.initialized, h.started = nil, nil
}

// topologicalSort computes initialization order using Kahn's algorithm
// Returns error if circular dependency detected
func (h *Hub) topologicalSort() ([]string, error) {
	// Build adjacency list and in-degree map
	inDegree := make(map[string]int)
	dependents := make(map[string][]string) // dep -> services that depend on it

	// 1. Get and sort all names for deterministic processing
	names := make([]string, 0, len(h.services))
	for name := range h.services {
		names = append(names, name)
	}
	sort.Strings(names)

	// 2. Build graph using sorted names
	for _, name := range names {
		svc := h.services[name]
		inDegree[name] = 0 // Initialize entry
		for _, dep := range svc.Dependencies() {
			if _, exists := h.services[dep]; !exists {
				return nil, fmt.Errorf("service %s depends on unregistered service: %s", name, dep)
			}
			inDegree[name]++
			dependents[dep] = append(dependents[dep], name)
		}
	}

	// 3. Seed queue from sorted names to ensure deterministic start
	var queue []string
	for _, name := range names {
		if inDegree[name] == 0 {
			queue = append(queue, name)
		}
	}

	var result []string
	for len(queue) > 0 {
		// Pop front (stable order via append)
		name := queue[0]
		queue = queue[1:]
		result = append(result, name)

		// Sorting dependents here is optional but ensures determinism for siblings in the tree
		deps := dependents[name]
		sort.Strings(deps)

		// Decrement in-degree of dependents
		for _, dependent := range deps {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	if len(result) != len(h.services) {
		return nil, fmt.Errorf("circular dependency detected in services")
	}

	return result, nil
}

// Names returns all registered service names (unordered)
func (h *Hub) Names() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	names := make([]string, 0, len(h.services))
	for name := range h.services {
		names = append(names, name)
	}
	return names
}

// BindResources lets initialized services attach typed resources
func (h *Hub) BindResources(r *engine.Resource) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, name := range h.sorted {
		if c, ok := h.services[name].(ResourceContributor); ok {
			c.Contribute(r)
		}
	}
}
