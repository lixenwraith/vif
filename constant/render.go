package constant

import (
	"time"
)

// Render masks categorize buffer cells for selective post-processing
// Masks are bitfields allowing combination via OR and exclusion via XOR
const (
	MaskNone   uint8 = 0
	MaskGrid   uint8 = 1 << 0 // Background grid, ping overlay
	MaskEntity uint8 = 1 << 1 // Characters, nuggets, spawned content
	MaskShield uint8 = 1 << 2 // Cursor shield effect
	MaskEffect uint8 = 1 << 3 // Decay, cleaners, flashes, materializers, drains
	MaskUI     uint8 = 1 << 4 // Heat meter, status bar, line numbers, cursor, overlay
	MaskAll    uint8 = 0xFF
)

// Global background dimming when foreground present
const (
	OcclusionDimEnabled = true
	// OcclusionDimEnabled = false
	OcclusionDimFactor = 0.8 // Bg intensity multiplier under foreground chars
	OcclusionDimMask   = MaskEffect | MaskEntity
)

// Post-Process Effect Configuration
const (
	GrayoutDuration = 1 * time.Second
	GrayoutMask     = MaskEntity
	a
	DimFactor = 0.5
	DimMask   = MaskAll ^ MaskUI
)