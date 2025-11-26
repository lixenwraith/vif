package engine

import (
	"time"
)

// Entity is a unique identifier for an entity
type Entity uint64

// Component are handled in Store

// System is an interface that all systems must implement
type System interface {
	Update(world *World, dt time.Duration)
	Priority() int // Lower values run first
}