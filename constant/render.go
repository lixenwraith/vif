package constant

import (
	"time"
)

// Render masks categorize buffer cells for selective post-processing
// Masks are bitfields allowing combination via OR and exclusion via XOR
const (
	MaskNone      uint8 = 0
	MaskPing      uint8 = 1 << 0 // Background grid, ping overlay
	MaskTypeable  uint8 = 1 << 1 // Characters, nuggets, spawned content
	MaskField     uint8 = 1 << 2 // Cursor shield effect
	MaskTransient uint8 = 1 << 3 // Decay, cleaners, flashes, materializers, drains
	MaskComposite uint8 = 1 << 4 // Composites exempt from grayout (Gold, Quasar)
	MaskUI        uint8 = 1 << 5 // Heat meter, status bar, line numbers, cursor, overlay
	MaskAll       uint8 = 0xFF
)

// Global background dimming when foreground present
const (
	OcclusionDimEnabled = true
	OcclusionDimFactor  = 0.8 // Bg intensity multiplier under foreground chars
	OcclusionDimMask    = MaskTransient | MaskTypeable
)

// Post-Process Effect Configuration
const (
	GrayoutDuration = 1 * time.Second
	GrayoutMask     = MaskTypeable
	a
	DimFactor = 0.5
	DimMask   = MaskAll ^ MaskUI
)