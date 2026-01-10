package engine

import (
	"github.com/lixenwraith/vi-fighter/core"
)

// AnyStore provides type-erased operations for lifecycle management
// This interface allows World to manage all stores uniformly
// for operations like entity destruction without knowing the concrete type
type AnyStore interface {
	// RemoveComponent deletes a component from an entity
	RemoveComponent(e core.Entity)

	// HasComponent checks if an entity has this component
	HasComponent(e core.Entity) bool

	// CountEntity returns the number of entities with this component
	CountEntity() int

	// ClearAllComponent removes all components from this store
	ClearAllComponent()
}

// QueryableStore extends AnyStore with query operations needed for
// the query builder to efficiently intersect component sets
type QueryableStore interface {
	AnyStore

	// AllEntity returns all entities that have this component type
	AllEntity() []core.Entity
}