package constant
// @lixen: #dev{feature[drain(render,system)],feature[dust(render,system)],feature[quasar(render,system)]}

import (
	"time"
)

// Render masks categorize buffer cells for selective post-processing
// Masks are bitfields allowing combination via OR and exclusion via XOR
const (
	MaskNone      uint8 = 0
	MaskPing      uint8 = 1 << 0 // Background ping and ping grid
	MaskGlyph     uint8 = 1 << 1 // Interactable non-composite characters: glyphs, nugget
	MaskField     uint8 = 1 << 2 // Shields
	MaskTransient uint8 = 1 << 3 // Decay, blossom, cleaner, flash, materialize, drain
	MaskComposite uint8 = 1 << 4 // Composites (Gold, Quasar)
	MaskUI        uint8 = 1 << 5 // Heat meter, status bar, line numbers, cursor, overlay
	MaskAll       uint8 = 0xFF
)

// Global background dimming when foreground present
const (
	OcclusionDimEnabled = true
	OcclusionDimFactor  = 0.8 // Bg intensity multiplier under foreground chars
	OcclusionDimMask    = MaskTransient | MaskGlyph
)

// Post-Process Effect Configuration
const (
	GrayoutDuration = 1 * time.Second
	GrayoutMask     = MaskGlyph
	a
	DimFactor = 0.5
	DimMask   = MaskAll ^ MaskUI
)