package engine

import (
	"reflect"
	"sync"
	"time"
)

// Entity is a unique identifier for an entity
type Entity uint64

// Component is a marker interface for all components
type Component interface{}

// System is an interface that all systems must implement
type System interface {
	Update(world *World, dt time.Duration)
	Priority() int // Lower values run first
}

// World contains all entities and their components
type World struct {
	mu               sync.RWMutex
	nextEntityID     Entity
	entities         map[Entity]map[reflect.Type]Component
	systems          []System
	spatialIndex     map[int]map[int]Entity // [y][x] -> Entity for position queries
	positionType     reflect.Type
	componentsByType map[reflect.Type][]Entity // Reverse index: component type -> entities
}

// NewWorld creates a new ECS world
func NewWorld() *World {
	return &World{
		nextEntityID:     1,
		entities:         make(map[Entity]map[reflect.Type]Component),
		systems:          make([]System, 0),
		spatialIndex:     make(map[int]map[int]Entity),
		componentsByType: make(map[reflect.Type][]Entity),
	}
}

// CreateEntity creates a new entity and returns its ID
func (w *World) CreateEntity() Entity {
	w.mu.Lock()
	defer w.mu.Unlock()

	id := w.nextEntityID
	w.nextEntityID++
	w.entities[id] = make(map[reflect.Type]Component)
	return id
}

// DestroyEntity removes an entity and all its components
// Note: Prefer using SafeDestroyEntity which handles spatial index cleanup atomically
func (w *World) DestroyEntity(entity Entity) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Remove from component type index
	if components, ok := w.entities[entity]; ok {
		for compType := range components {
			w.removeFromTypeIndex(entity, compType)
		}
	}

	delete(w.entities, entity)

	// Clean up spatial index
	for y := range w.spatialIndex {
		for x, e := range w.spatialIndex[y] {
			if e == entity {
				delete(w.spatialIndex[y], x)
			}
		}
	}
}

// SafeDestroyEntity safely removes an entity by atomically:
// 1. Removing from spatial index first (if entity has PositionComponent)
// 2. Then destroying the entity and cleaning up all component indices
// This ensures spatial index consistency and prevents race conditions.
// All operations are performed under a single lock for atomicity.
func (w *World) SafeDestroyEntity(entity Entity) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Check if entity exists
	components, ok := w.entities[entity]
	if !ok {
		return // Entity doesn't exist
	}

	// First, remove from spatial index if entity has a position
	// We need to find PositionComponent type from the components
	for compType, comp := range components {
		// Check if this is a PositionComponent by type name
		if compType.Name() == "PositionComponent" {
			// Use reflection to get X and Y fields
			posVal := reflect.ValueOf(comp)
			if posVal.Kind() == reflect.Struct {
				xField := posVal.FieldByName("X")
				yField := posVal.FieldByName("Y")
				if xField.IsValid() && yField.IsValid() && xField.CanInt() && yField.CanInt() {
					x := int(xField.Int())
					y := int(yField.Int())
					if row, exists := w.spatialIndex[y]; exists {
						delete(row, x)
					}
				}
			}
			break // Only one PositionComponent per entity
		}
	}

	// Remove from component type index
	for compType := range components {
		w.removeFromTypeIndex(entity, compType)
	}

	// Delete entity
	delete(w.entities, entity)
}

// AddComponent adds a component to an entity
func (w *World) AddComponent(entity Entity, component Component) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, ok := w.entities[entity]; !ok {
		return // Entity doesn't exist
	}

	compType := reflect.TypeOf(component)
	w.entities[entity][compType] = component

	// Add to component type index
	w.addToTypeIndex(entity, compType)
}

// GetComponent retrieves a component from an entity
func (w *World) GetComponent(entity Entity, componentType reflect.Type) (Component, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if components, ok := w.entities[entity]; ok {
		if comp, ok := components[componentType]; ok {
			return comp, true
		}
	}
	return nil, false
}

// RemoveComponent removes a component from an entity
func (w *World) RemoveComponent(entity Entity, componentType reflect.Type) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if components, ok := w.entities[entity]; ok {
		delete(components, componentType)
		w.removeFromTypeIndex(entity, componentType)
	}
}

// HasComponent checks if an entity has a specific component
func (w *World) HasComponent(entity Entity, componentType reflect.Type) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if components, ok := w.entities[entity]; ok {
		_, ok := components[componentType]
		return ok
	}
	return false
}

// GetEntitiesWith returns all entities that have the specified component types
func (w *World) GetEntitiesWith(componentTypes ...reflect.Type) []Entity {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if len(componentTypes) == 0 {
		return nil
	}

	// Start with entities that have the first component type
	candidates := w.componentsByType[componentTypes[0]]
	if candidates == nil {
		return nil
	}

	result := make([]Entity, 0)
	for _, entity := range candidates {
		hasAll := true
		for _, compType := range componentTypes {
			if !w.hasComponentUnsafe(entity, compType) {
				hasAll = false
				break
			}
		}
		if hasAll {
			result = append(result, entity)
		}
	}

	return result
}

// hasComponentUnsafe checks for component without locking (assumes lock is held)
func (w *World) hasComponentUnsafe(entity Entity, componentType reflect.Type) bool {
	if components, ok := w.entities[entity]; ok {
		_, ok := components[componentType]
		return ok
	}
	return false
}

// AddSystem adds a system to the world and sorts by priority
func (w *World) AddSystem(system System) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.systems = append(w.systems, system)

	// Sort systems by priority (bubble sort is fine for small number of systems)
	for i := 0; i < len(w.systems)-1; i++ {
		for j := 0; j < len(w.systems)-i-1; j++ {
			if w.systems[j].Priority() > w.systems[j+1].Priority() {
				w.systems[j], w.systems[j+1] = w.systems[j+1], w.systems[j]
			}
		}
	}
}

// Update runs all systems
func (w *World) Update(dt time.Duration) {
	w.mu.RLock()
	systems := make([]System, len(w.systems))
	copy(systems, w.systems)
	w.mu.RUnlock()

	for _, system := range systems {
		system.Update(w, dt)
	}
}

// UpdateSpatialIndex updates the spatial index for an entity with a position
func (w *World) UpdateSpatialIndex(entity Entity, x, y int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.spatialIndex[y] == nil {
		w.spatialIndex[y] = make(map[int]Entity)
	}
	w.spatialIndex[y][x] = entity
}

// GetEntityAtPosition returns the entity at a given position (0 if none)
func (w *World) GetEntityAtPosition(x, y int) Entity {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if row, ok := w.spatialIndex[y]; ok {
		if entity, ok := row[x]; ok {
			return entity
		}
	}
	return 0
}

// RemoveFromSpatialIndex removes an entity from a position in the spatial index
func (w *World) RemoveFromSpatialIndex(x, y int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if row, ok := w.spatialIndex[y]; ok {
		delete(row, x)
	}
}

// addToTypeIndex adds entity to the component type index
func (w *World) addToTypeIndex(entity Entity, componentType reflect.Type) {
	entities := w.componentsByType[componentType]

	// Check if already in list
	for _, e := range entities {
		if e == entity {
			return
		}
	}

	w.componentsByType[componentType] = append(entities, entity)
}

// removeFromTypeIndex removes entity from the component type index
func (w *World) removeFromTypeIndex(entity Entity, componentType reflect.Type) {
	entities := w.componentsByType[componentType]
	for i, e := range entities {
		if e == entity {
			// Remove by swapping with last element and truncating
			w.componentsByType[componentType][i] = w.componentsByType[componentType][len(entities)-1]
			w.componentsByType[componentType] = w.componentsByType[componentType][:len(entities)-1]
			return
		}
	}
}

// EntityCount returns the number of entities in the world
func (w *World) EntityCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.entities)
}
