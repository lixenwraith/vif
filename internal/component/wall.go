package component

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
)

// WallBlockMask defines what entity types a wall blocks
type WallBlockMask uint8

const (
	WallBlockNone     WallBlockMask = 0
	WallBlockCursor   WallBlockMask = 1 << 0
	WallBlockKinetic  WallBlockMask = 1 << 1 // Drain, Swarm, Quasar
	WallBlockParticle WallBlockMask = 1 << 2 // Particle and Dust
	WallBlockSpawn    WallBlockMask = 1 << 3 // All entity spawning
	WallBlockAll      WallBlockMask = 0xFF
)

// WallBlockNames are what a document calls each blocked kind, and none or all
var WallBlockNames = map[string]WallBlockMask{
	"none":     WallBlockNone,
	"cursor":   WallBlockCursor,
	"kinetic":  WallBlockKinetic,
	"particle": WallBlockParticle,
	"spawn":    WallBlockSpawn,
	"all":      WallBlockAll,
}

// BoxDrawStyle defines box-drawing character set
type BoxDrawStyle uint8

const (
	BoxDrawNone   BoxDrawStyle = 0
	BoxDrawSingle BoxDrawStyle = 1
	BoxDrawDouble BoxDrawStyle = 2
)

// BoxDrawStyleNames are what a document calls each BoxDrawStyle
var BoxDrawStyleNames = [...]string{BoxDrawNone: "none", BoxDrawSingle: "single", BoxDrawDouble: "double"}

// WallComponent marks an entity as a wall/obstacle with visual properties
type WallComponent struct {
	BlockMask WallBlockMask

	// Foreground visual (character layer)
	Rune     rune // 0 = no foreground character
	FgColor  color.RGB
	RenderFg bool

	// Background visual (cell fill)
	BgColor  color.RGB
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

// WallAttrNames are what a document calls each attribute a cell's Attrs holds
var WallAttrNames = map[string]terminal.Attr{
	"none":       terminal.AttrNone,
	"bold":       terminal.AttrBold,
	"dim":        terminal.AttrDim,
	"italic":     terminal.AttrItalic,
	"underline":  terminal.AttrUnderline,
	"blink":      terminal.AttrBlink,
	"reverse":    terminal.AttrReverse,
	"fg256":      terminal.AttrFg256,
	"bg256":      terminal.AttrBg256,
	"fg_default": terminal.AttrFgDefault,
	"bg_default": terminal.AttrBgDefault,
}

// WallVisualConfig defines visual properties for wall rendering
// Zero value struct signals use of system defaults
type WallVisualConfig struct {
	Char     rune         `toml:"char"`
	FgColor  color.RGB    `toml:"fg_color"`
	BgColor  color.RGB    `toml:"bg_color"`
	RenderFg bool         `toml:"render_fg"`
	RenderBg bool         `toml:"render_bg"`
	BoxStyle BoxDrawStyle `toml:"box_style"`
}
