package engine

import (
	"time"
)

// Entity is a unique identifier for an entity
type Entity uint64

// DEPRECATED: Replaced by Component Store
// // Component is a marker interface for all components
// type Component any

// System is an interface that all systems must implement
type System interface {
	Update(world *World, dt time.Duration)
	Priority() int // Lower values run first
}