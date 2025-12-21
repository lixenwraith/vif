package component

import "github.com/lixenwraith/vi-fighter/core"

// BehaviorID routes composite events to behavior-specific systems
type BehaviorID uint8

const (
	BehaviorNone BehaviorID = iota
	BehaviorGold
	BehaviorBubble
	BehaviorBoss
	BehaviorShield
)

// CompositeLayer defines member interactability
const (
	LayerCore   uint8 = 0 // Typeable members
	LayerShield uint8 = 1 // Protective, non-typeable
	LayerEffect uint8 = 2 // Visual only, non-interactable
)

// MemberEntry represents a single member in a composite group
type MemberEntry struct {
	Entity  core.Entity
	OffsetX int8  // Relative to Phantom Head
	OffsetY int8  // Relative to Phantom Head
	Layer   uint8 // LayerCore, LayerShield, LayerEffect
}

// CompositeHeaderComponent resides on the Phantom Head (controller) entity
// The Phantom Head is invisible, protected, and manages group lifecycle
type CompositeHeaderComponent struct {
	GroupID    uint64
	BehaviorID BehaviorID

	// Contiguous slice for cache-friendly iteration
	Members []MemberEntry

	// Fixed-Point (16.16) sub-pixel movement
	// 1.0 velocity = 1 << 16 = 65536
	VelX, VelY int32 // Velocity in fixed-point units per tick
	AccX, AccY int32 // Fractional accumulation

	// Hierarchy support (0 if root composite)
	ParentAnchor core.Entity

	// Compaction flag - set when member dies
	Dirty bool
}

// MemberComponent provides O(1) anchor resolution from any child entity
type MemberComponent struct {
	AnchorID core.Entity // Points to Phantom Head
}