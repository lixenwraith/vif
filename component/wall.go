package component

import (
	"github.com/lixenwraith/vi-fighter/terminal"
)

// WallBlockMask defines what entity types a wall blocks
type WallBlockMask uint8

const (
	WallBlockNone     WallBlockMask = 0
	WallBlockCursor   WallBlockMask = 1 << 0
	WallBlockKinetic  WallBlockMask = 1 << 1 // Drain, Swarm, Quasar
	WallBlockParticle WallBlockMask = 1 << 2 // Decay, Blossom, Dust
	WallBlockSpawn    WallBlockMask = 1 << 3 // All entity spawning
	WallBlockAll      WallBlockMask = 0xFF
)

// Has checks if specific block flag is set
func (m WallBlockMask) Has(flag WallBlockMask) bool {
	return m&flag != 0
}

// IsBlocking returns true if wall blocks any entity type
func (m WallBlockMask) IsBlocking() bool {
	return m != WallBlockNone
}

// WallComponent marks an entity as a wall/obstacle with visual properties
type WallComponent struct {
	BlockMask WallBlockMask

	// Foreground visual (character layer)
	Char     rune // 0 = no foreground character
	FgColor  terminal.RGB
	RenderFg bool

	// Background visual (cell fill)
	BgColor  terminal.RGB
	RenderBg bool
}

// NeedsRender returns true if wall has any visual component
func (w *WallComponent) NeedsRender() bool {
	return w.RenderFg || w.RenderBg
}

// WallCellDef defines a single cell in composite wall structure
// Used by WallCompositeSpawnRequestPayload
type WallCellDef struct {
	OffsetX  int
	OffsetY  int
	Char     rune
	FgColor  terminal.RGB
	BgColor  terminal.RGB
	RenderFg bool
	RenderBg bool
}