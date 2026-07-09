package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/core"
)

// TrailCapacity is ring buffer size for trail particles
const TrailCapacity = 24

// MissileTrailPoint stores position snapshot for trail rendering
type MissileTrailPoint struct {
	X, Y int64         // Q32.32 precise position
	Age  time.Duration // Time since creation
}

// MissileComponent holds missile entity state (pure data)
type MissileComponent struct {
	Owner  core.Entity // Cursor that fired
	Origin core.Entity // Orb entity (visual origin)

	// Target assignment
	TargetEntity core.Entity // Header for composite, entity for single
	HitEntity    core.Entity // Specific member to hit (same as Target for non-composite)

	// Timing
	Lifetime      time.Duration // Time since spawn
	LastTrailEmit time.Duration // Lifetime at last trail emission

	// Trail ring buffer
	Trail     [TrailCapacity]MissileTrailPoint
	TrailHead int // Next write index
	TrailLen  int // Current valid entries (0..TrailCapacity))
}
