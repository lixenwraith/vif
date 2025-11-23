package engine

import "github.com/lixenwraith/vi-fighter/components"

// EntityBuilder provides a fluent, type-safe interface for constructing entities with components.
// It reserves an entity ID upfront and allows components to be added transactionally before
// committing the entity to the world via Build().
//
// Example usage:
//   entity := world.NewEntity().
//       With(world.Characters, characterComponent).
//       WithPosition(world.Positions, positionComponent).
//       Build()
type EntityBuilder struct {
	world  *WorldGeneric
	entity Entity
	built  bool
}

// NewEntity creates a new EntityBuilder with a reserved entity ID.
// The entity ID is allocated immediately, but components are not added to stores
// until Build() is called. This ensures atomic entity creation.
//
// Returns a new EntityBuilder ready for component assignment via With() methods.
func (w *WorldGeneric) NewEntity() *EntityBuilder {
	return &EntityBuilder{
		world:  w,
		entity: w.reserveEntityID(),
		built:  false,
	}
}

// With adds a component of type T to the entity being built.
// This is a generic function that provides compile-time type safety - the store
// type must match the component type.
//
// Parameters:
//   - eb: The EntityBuilder (receiver-style parameter for chaining)
//   - store: The generic store for component type T
//   - component: The component value to add
//
// Returns the EntityBuilder for method chaining.
// Panics if called after Build().
//
// Example:
//   builder.With(world.Characters, components.CharacterComponent{Rune: 'A'})
func With[T any](eb *EntityBuilder, store *Store[T], component T) *EntityBuilder {
	if eb.built {
		panic("entity already built - cannot add components after Build()")
	}
	store.Add(eb.entity, component)
	return eb
}

// WithPosition adds a position component to the entity being built.
// This is a specialized function for PositionStore because it requires
// spatial index updates and collision detection.
//
// Parameters:
//   - eb: The EntityBuilder (receiver-style parameter for chaining)
//   - store: The PositionStore
//   - component: The position component to add
//
// Returns the EntityBuilder for method chaining.
// Panics if called after Build().
//
// Example:
//   builder.WithPosition(world.Positions, components.PositionComponent{X: 10, Y: 5})
func WithPosition(eb *EntityBuilder, store *PositionStore, component components.PositionComponent) *EntityBuilder {
	if eb.built {
		panic("entity already built - cannot add components after Build()")
	}
	store.Add(eb.entity, component)
	return eb
}

// Build finalizes entity construction and returns the entity ID.
// After calling Build(), no more components can be added to this builder.
// The entity is now fully committed to the world with all specified components.
//
// Returns the entity ID that was reserved when NewEntity() was called.
func (eb *EntityBuilder) Build() Entity {
	eb.built = true
	return eb.entity
}

// reserveEntityID atomically allocates a new entity ID from the world's counter.
// This is used internally by NewEntity() to reserve an ID before components are added.
//
// Returns a unique entity ID that has never been used before in this world.
func (w *WorldGeneric) reserveEntityID() Entity {
	w.mu.Lock()
	defer w.mu.Unlock()

	id := w.nextEntityID
	w.nextEntityID++
	return id
}
