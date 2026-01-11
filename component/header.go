package component

import "github.com/lixenwraith/vi-fighter/core"

// Behavior routes composite events to behavior-specific systems
type Behavior uint8

const (
	BehaviorNone Behavior = iota
	BehaviorGold
	BehaviorQuasar
	BehaviorBubble
	BehaviorBoss
	BehaviorShield
)

// HeaderComponent is on Phantom Head entity, which is invisible, protected and manages composite lifecycle
type HeaderComponent struct {
	Behavior Behavior

	// Contiguous slice for cache-friendly iteration
	MemberEntries []MemberEntry

	// Fixed-Point (16.16) sub-pixel movement
	// 1.0 velocity = 1 << 16 = 65536
	VelX, VelY int64 // Velocity in fixed-point units per tick
	AccX, AccY int64 // Fractional accumulation

	// Hierarchy support (0 if root composite)
	ParentHeader core.Entity

	// Compaction flag - set when member dies
	Dirty bool
}

// CompositeLayer defines member interactability
const (
	LayerGlyph  uint8 = 0 // Typeable members
	LayerShield uint8 = 1 // Protective
	LayerEffect uint8 = 2 // Visual only, non-interactable
)

// MemberEntry represents a single member in a composite group
type MemberEntry struct {
	Entity  core.Entity
	OffsetX int8  // Relative to Phantom Head
	OffsetY int8  // Relative to Phantom Head
	Layer   uint8 // LayerGlyph, LayerShield, LayerEffect
}