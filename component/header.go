package component

import "github.com/lixenwraith/vi-fighter/core"

// Behavior routes composite events to behavior-specific systems
type Behavior uint8

const (
	BehaviorNone Behavior = iota
	BehaviorGold
	BehaviorQuasar
	BehaviorSwarm
	BehaviorStorm
	BehaviorPylon
	BehaviorSnake
	BehaviorBoss // Future
)

// CompositeType defines how the composite handles damage and lifecycle
type CompositeType uint8

const (
	// CompositeTypeUnit: Damage applied to Header. Members are just hitboxes. (e.g. Swarm, Quasar)
	CompositeTypeUnit CompositeType = iota
	// CompositeTypeAblative: Damage applied to individual Members. Header is anchor. (e.g. Storm Circle)
	CompositeTypeAblative
	// CompositeTypeContainer: Logic grouper only. No direct combat interaction. (e.g. Storm Root)
	CompositeTypeContainer
)

// HeaderComponent is on Phantom Head entity, which is invisible, protected and manages composite lifecycle
type HeaderComponent struct {
	Behavior Behavior

	// Explicit type to control combat/targeting logic
	Type CompositeType

	// Contiguous slice for cache-friendly iteration
	MemberEntries []MemberEntry

	// Hierarchy support (0 if root composite)
	ParentHeader core.Entity

	// Compaction flag - set when member dies
	Dirty bool

	// SkipPositionSync: owner system manages member positions directly.
	// CompositeSystem validates liveness but skips offset-based position propagation.
	SkipPositionSync bool
}

// MemberEntry represents a single member in a composite group
type MemberEntry struct {
	Entity  core.Entity
	OffsetX int // Relative to Phantom Head
	OffsetY int // Relative to Phantom Head
}