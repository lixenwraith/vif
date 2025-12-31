package registry

import (
	"sync"

	"github.com/lixenwraith/vi-fighter/render"
)

// Forward declarations to avoid import cycles
// Actual types resolved at registration time via interface{}

// SystemFactory creates a System from a World
// Returns engine.System interface
type SystemFactory func(world any) any

// RendererFactory creates a SystemRenderer from a GameContext
type RendererFactory func(ctx any) any

// ServiceFactory creates a Service
type ServiceFactory func() any

// RendererEntry holds factory and priority metadata
type RendererEntry struct {
	Factory  RendererFactory
	Priority render.RenderPriority
}

var (
	systemsMu   sync.RWMutex
	systems     = make(map[string]SystemFactory)
	renderersMu sync.RWMutex
	renderers   = make(map[string]RendererEntry)
	servicesMu  sync.RWMutex
	services    = make(map[string]ServiceFactory)
)

// RegisterSystem adds a system factory by name
func RegisterSystem(name string, factory SystemFactory) {
	systemsMu.Lock()
	defer systemsMu.Unlock()
	systems[name] = factory
}

// GetSystem retrieves a system factory by name
func GetSystem(name string) (SystemFactory, bool) {
	systemsMu.RLock()
	defer systemsMu.RUnlock()
	f, ok := systems[name]
	return f, ok
}

// SystemNames returns all registered system names
func SystemNames() []string {
	systemsMu.RLock()
	defer systemsMu.RUnlock()
	names := make([]string, 0, len(systems))
	for name := range systems {
		names = append(names, name)
	}
	return names
}

// RegisterRenderer adds a renderer factory with priority
func RegisterRenderer(name string, factory RendererFactory, priority render.RenderPriority) {
	renderersMu.Lock()
	defer renderersMu.Unlock()
	renderers[name] = RendererEntry{Factory: factory, Priority: priority}
}

// GetRenderer retrieves a renderer entry by name
func GetRenderer(name string) (RendererEntry, bool) {
	renderersMu.RLock()
	defer renderersMu.RUnlock()
	e, ok := renderers[name]
	return e, ok
}

// RendererNames returns all registered renderer names
func RendererNames() []string {
	renderersMu.RLock()
	defer renderersMu.RUnlock()
	names := make([]string, 0, len(renderers))
	for name := range renderers {
		names = append(names, name)
	}
	return names
}

// RegisterService adds a service factory by name
func RegisterService(name string, factory ServiceFactory) {
	servicesMu.Lock()
	defer servicesMu.Unlock()
	services[name] = factory
}

// GetService retrieves a service factory by name
func GetService(name string) (ServiceFactory, bool) {
	servicesMu.RLock()
	defer servicesMu.RUnlock()
	f, ok := services[name]
	return f, ok
}

// ServiceNames returns all registered service names
func ServiceNames() []string {
	servicesMu.RLock()
	defer servicesMu.RUnlock()
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	return names
}