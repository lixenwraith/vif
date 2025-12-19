package services

import (
	"fmt"
	"sync"
)

// Hub is the runtime container for service instances
// Manages lifecycle and provides type-safe access
type Hub struct {
	mu       sync.RWMutex
	services map[string]Service
	sorted   []string // Topological order, computed on InitAll
	started  []string // Services that completed Start(), for rollback
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
func (h *Hub) InitAll(world any) error {
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
		if err := svc.Init(world); err != nil {
			// Rollback: stop already-initialized in reverse order
			for i := len(initialized) - 1; i >= 0; i-- {
				h.services[initialized[i]].Stop()
			}
			return fmt.Errorf("service %s init failed: %w", name, err)
		}
		initialized = append(initialized, name)
	}

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
		if err := svc.Start(); err != nil {
			// Rollback: stop already-started in reverse order
			for i := len(h.started) - 1; i >= 0; i-- {
				h.services[h.started[i]].Stop()
			}
			return fmt.Errorf("service %s start failed: %w", name, err)
		}
		h.started = append(h.started, name)
	}

	return nil
}

// StopAll calls Stop on all started services in reverse topological order
// Logs errors but does not fail - ensures all services get Stop called
func (h *Hub) StopAll() {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Stop in reverse order
	for i := len(h.started) - 1; i >= 0; i-- {
		name := h.started[i]
		if svc, ok := h.services[name]; ok {
			svc.Stop() // Errors logged internally by service
		}
	}
	h.started = nil
}

// topologicalSort computes initialization order using Kahn's algorithm
// Returns error if circular dependency detected
func (h *Hub) topologicalSort() ([]string, error) {
	// Build adjacency list and in-degree map
	inDegree := make(map[string]int)
	dependents := make(map[string][]string) // dep -> services that depend on it

	for name := range h.services {
		inDegree[name] = 0
	}

	for name, svc := range h.services {
		for _, dep := range svc.Dependencies() {
			if _, exists := h.services[dep]; !exists {
				return nil, fmt.Errorf("service %s depends on unregistered service: %s", name, dep)
			}
			inDegree[name]++
			dependents[dep] = append(dependents[dep], name)
		}
	}

	// Process nodes with zero in-degree
	var queue []string
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	var result []string
	for len(queue) > 0 {
		// Pop front (stable order via append)
		name := queue[0]
		queue = queue[1:]
		result = append(result, name)

		// Decrement in-degree of dependents
		for _, dependent := range dependents[name] {
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