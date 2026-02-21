package component

import (
	"github.com/lixenwraith/vi-fighter/core"
)

// Snake trail buffer sizing
const (
	SnakeTrailCapacity = 128 // Ring buffer size for position history
)

// SnakeComponent marks the root snake controller entity (container header)
type SnakeComponent struct {
	HeadEntity core.Entity
	BodyEntity core.Entity

	// Spawn state
	SpawnOriginX, SpawnOriginY int
	SpawnRemaining             int  // Segments left to spawn
	SpawnTickCounter           int  // Ticks since last segment spawn
	SpawnComplete              bool // True when initial spawn finished or interrupted

	// Shield state (head immunity while body alive)
	IsShielded bool
}

// SnakeHeadComponent holds head-specific state, attached to head header entity
type SnakeHeadComponent struct {
	// Movement trail (ring buffer of segment center positions)
	Trail     [SnakeTrailCapacity]core.Point
	TrailHead int // Next write index
	TrailLen  int // Current valid entries

	// Facing direction for perpendicular member placement (Q32.32 normalized)
	FacingX, FacingY int64

	// Growth queue (pending segments from glyph consumption)
	GrowthPending int

	// Last recorded position for trail delta calculation
	LastTrailX, LastTrailY int
}

// SnakeSegment represents a 3-cell wide body segment (metadata only)
// Member entity resolution via HeaderComponent.MemberEntries + SnakeMemberComponent
type SnakeSegment struct {
	// Rest position from trail (Q32.32)
	RestX, RestY int64

	// Connectivity state
	Connected bool
}

// SnakeBodyComponent holds body-specific state, attached to body header entity
type SnakeBodyComponent struct {
	Segments []SnakeSegment // Ordered head→tail
}

// SnakeMemberComponent provides segment and lateral info for each body member
type SnakeMemberComponent struct {
	SegmentIndex  int // Which segment (0 = closest to head)
	LateralOffset int // -1, 0, +1 perpendicular to direction
	MaxHitPoints  int // Initial HP for health ratio calculation
}