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

// BoxDrawStyle defines box-drawing character set
type BoxDrawStyle uint8

const (
	BoxDrawNone   BoxDrawStyle = 0
	BoxDrawSingle BoxDrawStyle = 1
	BoxDrawDouble BoxDrawStyle = 2
)

// WallComponent marks an entity as a wall/obstacle with visual properties
type WallComponent struct {
	BlockMask WallBlockMask

	// Foreground visual (character layer)
	Rune     rune // 0 = no foreground character
	FgColor  terminal.RGB
	RenderFg bool

	// Background visual (cell fill)
	BgColor  terminal.RGB
	RenderBg bool

	// Box-drawing: when non-zero, Rune is computed from neighbor topology
	BoxStyle BoxDrawStyle

	// Color mode attributes (AttrFg256/AttrBg256 from pattern pipeline)
	// Zero value = TrueColor RGB; renderer converts to 256 at render time
	Attrs terminal.Attr
}

// WallCellDef defines a single cell in composite wall structure (used by event payload)
type WallCellDef struct {
	OffsetX int
	OffsetY int
	WallVisualConfig
	Attrs terminal.Attr
}

// WallVisualConfig defines visual properties for wall rendering
// Zero value struct signals use of system defaults
type WallVisualConfig struct {
	Char     rune         `toml:"char"`
	FgColor  terminal.RGB `toml:"fg_color"`
	BgColor  terminal.RGB `toml:"bg_color"`
	RenderFg bool         `toml:"render_fg"`
	RenderBg bool         `toml:"render_bg"`
	BoxStyle BoxDrawStyle `toml:"box_style"`
}