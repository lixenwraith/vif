// FILE: component/missile.go
package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/core"
)

// MissilePhase represents lifecycle state
type MissilePhase uint8

const (
	MissilePhaseFlying   MissilePhase = iota // Parent ascending
	MissilePhaseSeeking                      // Child homing toward target
	MissilePhaseImpacted                     // Terminal, pending cleanup
)

// MissileType distinguishes parent from children
type MissileType uint8

const (
	MissileTypeClusterParent MissileType = iota
	MissileTypeClusterChild
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
	Type   MissileType
	Phase  MissilePhase
	Owner  core.Entity // Cursor that fired
	Origin core.Entity // Orb entity (visual origin)

	// Target assignment
	TargetEntity core.Entity // Header for composite, entity for single
	HitEntity    core.Entity // Specific member to hit (same as Target for non-composite)

	// Timing
	Lifetime      time.Duration // Time since spawn
	LastTrailEmit time.Duration // Lifetime at last trail emission

	// Parent-specific
	ChildCount     int           // Number of children to spawn
	Targets        []core.Entity // Pre-assigned targets for children
	HitEntities    []core.Entity // Corresponding hit entities
	OriginalDistSq int64         // Squared distance to target at spawn (for split calculation)

	// Trail ring buffer
	Trail     [TrailCapacity]MissileTrailPoint
	TrailHead int // Next write index
	TrailLen  int // Current valid entries (0..TrailCapacity))
}