package engine
// @lixen: #dev{feature[drain(render,system)],feature[dust(render,system)],feature[quasar(render,system)]}

import (
	"github.com/lixenwraith/vi-fighter/core"
)

// AnyStore provides type-erased operations for lifecycle management
// This interface allows World to manage all stores uniformly
// for operations like entity destruction without knowing the concrete type
type AnyStore interface {
	// Remove deletes a component from an entity
	Remove(e core.Entity)

	// Has checks if an entity has this component
	Has(e core.Entity) bool

	// Count returns the number of entities with this component
	Count() int

	// Clear removes all components from this store
	Clear()
}

// QueryableStore extends AnyStore with query operations needed for
// the query builder to efficiently intersect component sets
type QueryableStore interface {
	AnyStore

	// All returns all entities that have this component type
	All() []core.Entity
}