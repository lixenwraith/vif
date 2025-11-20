package engine

import (
	"fmt"
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
	updateMutex      sync.Mutex                // Frame barrier mutex to prevent concurrent updates
	isUpdating       bool                      // Flag indicating if update is in progress
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
// This method ensures all system updates complete before returning,
// providing a frame barrier for safe rendering after updates.
// Only one update cycle can run at a time.
func (w *World) Update(dt time.Duration) {
	// Acquire update mutex to ensure only one update runs at a time
	w.updateMutex.Lock()
	defer w.updateMutex.Unlock()

	w.mu.RLock()
	systems := make([]System, len(w.systems))
	copy(systems, w.systems)
	w.mu.RUnlock()

	for _, system := range systems {
		system.Update(w, dt)
	}
}

// WaitForUpdates waits for all pending updates to complete
// This should be called before rendering to ensure a consistent state
// This is a no-op if Update is called synchronously, but provides
// explicit synchronization for future concurrent update scenarios.
func (w *World) WaitForUpdates() {
	// Try to acquire and immediately release the update mutex
	// This ensures any ongoing Update() has completed
	w.updateMutex.Lock()
	w.updateMutex.Unlock()
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

// MoveEntitySafe safely moves an entity from one position to another using spatial transactions
// This prevents race conditions and ensures atomic spatial index updates
// Returns a CollisionResult indicating if the move succeeded or if there was a collision
func (w *World) MoveEntitySafe(entity Entity, oldX, oldY, newX, newY int) CollisionResult {
	// Begin transaction
	tx := w.BeginSpatialTransaction()

	// Attempt move
	result := tx.Move(entity, oldX, oldY, newX, newY)

	// If no collision, commit the transaction
	if !result.HasCollision {
		tx.Commit()
	}

	return result
}

// ValidateSpatialIndex checks the spatial index for consistency
// Returns a list of inconsistencies found (empty if consistent)
// This is primarily used for debugging and testing
func (w *World) ValidateSpatialIndex() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	inconsistencies := make([]string, 0)

	// Check that all entities in spatial index actually exist
	for y, row := range w.spatialIndex {
		for x, entity := range row {
			if _, ok := w.entities[entity]; !ok {
				inconsistencies = append(inconsistencies,
					fmt.Sprintf("Spatial index at (%d,%d) references non-existent entity %d", x, y, entity))
			}
		}
	}

	// Check that all entities with PositionComponent are in spatial index
	for entity, components := range w.entities {
		for compType, comp := range components {
			if compType.Name() == "PositionComponent" {
				// Use reflection to get X and Y fields
				posVal := reflect.ValueOf(comp)
				if posVal.Kind() == reflect.Struct {
					xField := posVal.FieldByName("X")
					yField := posVal.FieldByName("Y")
					if xField.IsValid() && yField.IsValid() && xField.CanInt() && yField.CanInt() {
						x := int(xField.Int())
						y := int(yField.Int())

						// Check if entity is in spatial index at this position
						if row, ok := w.spatialIndex[y]; ok {
							if indexedEntity, ok := row[x]; ok {
								if indexedEntity != entity {
									inconsistencies = append(inconsistencies,
										fmt.Sprintf("Entity %d has PositionComponent at (%d,%d) but spatial index has entity %d",
											entity, x, y, indexedEntity))
								}
							} else {
								inconsistencies = append(inconsistencies,
									fmt.Sprintf("Entity %d has PositionComponent at (%d,%d) but is not in spatial index",
										entity, x, y))
							}
						} else {
							inconsistencies = append(inconsistencies,
								fmt.Sprintf("Entity %d has PositionComponent at (%d,%d) but row %d is not in spatial index",
									entity, x, y, y))
						}
					}
				}
			}
		}
	}

	return inconsistencies
}

