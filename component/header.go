package component

import "github.com/lixenwraith/vi-fighter/core"

// BehaviorID routes composite events to behavior-specific systems
type BehaviorID uint8

const (
	BehaviorNone BehaviorID = iota
	BehaviorGold
	BehaviorQuasar
	BehaviorBubble
	BehaviorBoss
	BehaviorShield
)

// HeaderComponent resides on the Phantom Head (controller) entity
// The Phantom Head is invisible, protected, and manages group lifecycle
type HeaderComponent struct {
	BehaviorID BehaviorID

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