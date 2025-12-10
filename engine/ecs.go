package engine

import (
	"time"
)

// Entity is defined in core package to avoid cyclic dependency

// Components are handled in Store

// System is an interface that all systems must implement
type System interface {
	Update(world *World, dt time.Duration)
	Priority() int // Lower values run first
}