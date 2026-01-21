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
	BehaviorBoss // Future
)

// HeaderComponent is on Phantom Head entity, which is invisible, protected and manages composite lifecycle
type HeaderComponent struct {
	Behavior Behavior

	// Contiguous slice for cache-friendly iteration
	MemberEntries []MemberEntry

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
	OffsetX int   // Relative to Phantom Head
	OffsetY int   // Relative to Phantom Head
	Layer   uint8 // LayerGlyph, LayerShield, LayerEffect
}