package visual

import "time"

// Strobe envelope configuration
const (
	// StrobeRiseRatio is the fraction of duration spent rising to peak intensity
	StrobeRiseRatio = 0.3
	// StrobeDecayRatio is the fraction of duration spent decaying from peak
	StrobeDecayRatio = 0.7
)

// Grayout configuration
const (
	GrayoutDuration = 1 * time.Second // Unused, logic commented out in transient system to be wired in if needed
	GrayoutMask     = MaskGlyph
)

// Dim configuration
const (
	DimFactor = 0.5
	DimMask   = MaskAll ^ MaskUI
)

// Global background dimming when foreground present
const (
	OcclusionDimEnabled = true
	OcclusionDimFactor  = 0.8 // Bg intensity multiplier under foreground chars
	OcclusionDimMask    = MaskTransient | MaskGlyph
)