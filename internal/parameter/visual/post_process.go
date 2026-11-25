package visual

import (
	"time"

	"github.com/lixenwraith/color"
)

// Strobe envelope configuration
const (
	// StrobeRiseRatio is the fraction of duration spent rising to peak intensity
	StrobeRiseRatio = 0.2
	// StrobeDecayRatio is the fraction of duration spent decaying from peak
	StrobeDecayRatio = 0.8
	// StrobeDefaultDuration is the default value used if no duration supplied in payload (not 0)
	StrobeDefaultDuration = 500 * time.Millisecond
)

// Grayout configuration
const (
	GrayoutDuration = 1 * time.Second // Unused, logic commented out in transient system to be wired in if needed
	GrayoutMask     = MaskGlyph
)

// Sight: the moving cursor's light holds SightLightHold past its last step, longer
// than a block's slowest cell takes, and what it fell on fades over SightFade. A
// move of up to SightStepReach cells along one axis between frames is the walk
// going on. A trail draws its cells as SightTrailRune and its first, where the walk
// began, as SightStartRune.
const (
	SightLightHold = 80 * time.Millisecond
	SightStepReach = 8
	SightFade      = 600 * time.Millisecond
	SightTrailRune = '·'
	SightStartRune = '■'
)

var (
	RgbSightTrail = color.RGB{R: 70, G: 110, B: 160}
	RgbSightStart = color.RGB{R: 255, G: 190, B: 60}
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

// A coarse effect background (RenderBuffer.SetCoarseBg) in a true color frame takes
// the xterm color within CoarseBgPaletteDE of it (CIELAB), else its level of
// CoarseBgStep a channel: a glow then goes out in runs of one short SGR
const (
	CoarseBgPaletteDE = 8
	CoarseBgStep      = 16
)
