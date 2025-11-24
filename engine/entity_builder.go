package engine

import "github.com/lixenwraith/vi-fighter/components"

// EntityBuilder provides transactional entity creation with type-safe component assignment.
//
// Example:
//
//	entity := With(
//	    WithPosition(world.NewEntity(), world.Positions, posComponent),
//	    world.Characters, charComponent,
//	).Build()
type EntityBuilder struct {
	world  *World
	entity Entity
	built  bool
}

// NewEntity reserves an entity ID and returns a builder for component assignment.
// Components are committed atomically when Build() is called.
func (w *World) NewEntity() *EntityBuilder {
	return &EntityBuilder{
		world:  w,
		entity: w.reserveEntityID(),
		built:  false,
	}
}

// With adds a component to the entity. Type safety ensures store matches component type.
// Panics if called after Build().
func With[T any](eb *EntityBuilder, store *Store[T], component T) *EntityBuilder {
	if eb.built {
		panic("entity already built - cannot add components after Build()")
	}
	store.Add(eb.entity, component)
	return eb
}

// WithPosition adds a position component with spatial index update.
// Panics if called after Build().
func WithPosition(eb *EntityBuilder, store *PositionStore, component components.PositionComponent) *EntityBuilder {
	if eb.built {
		panic("entity already built - cannot add components after Build()")
	}
	store.Add(eb.entity, component)
	return eb
}

// Build commits all components and returns the entity ID.
func (eb *EntityBuilder) Build() Entity {
	eb.built = true
	return eb.entity
}

// reserveEntityID atomically allocates a new unique entity ID.
func (w *World) reserveEntityID() Entity {
	w.mu.Lock()
	defer w.mu.Unlock()

	id := w.nextEntityID
	w.nextEntityID++
	return id
}
