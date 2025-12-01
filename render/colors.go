package render

import (
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/core"
)

// RGB color definitions for sequences - dark/normal/bright levels
var (
	RgbSequenceGreenDark   = core.RGB{0, 130, 0}   // Dark Green
	RgbSequenceGreenNormal = core.RGB{0, 200, 0}   // Normal Green
	RgbSequenceGreenBright = core.RGB{50, 255, 50} // Bright Green

	RgbSequenceRedDark   = core.RGB{180, 50, 50}   // Dark Red
	RgbSequenceRedNormal = core.RGB{255, 80, 80}   // Normal Red
	RgbSequenceRedBright = core.RGB{255, 120, 120} // Bright Red

	RgbSequenceBlueDark   = core.RGB{60, 100, 200}  // Dark Blue
	RgbSequenceBlueNormal = core.RGB{100, 150, 255} // Normal Blue
	RgbSequenceBlueBright = core.RGB{140, 190, 255} // Bright Blue

	RgbSequenceGold = core.RGB{255, 255, 0} // Bright Yellow for gold sequence
	RgbDecay        = core.RGB{0, 139, 139} // Dark Cyan for decay animation
	RgbDrain        = core.RGB{0, 200, 200} // Vibrant Cyan for drain entity
	RgbMaterialize  = core.RGB{0, 220, 220} // Bright cyan for materialize head

	RgbLineNumbers     = core.RGB{180, 180, 180} // Brighter gray
	RgbStatusBar       = core.RGB{255, 255, 255} // White
	RgbColumnIndicator = core.RGB{180, 180, 180} // Brighter gray
	RgbBackground      = core.RGB{26, 27, 38}    // Tokyo Night background

	RgbPingHighlight = core.RGB{50, 50, 50}    // Very dark gray for ping
	RgbPingNormal    = core.RGB{5, 5, 5}       // Almost black for NORMAL/SEARCH ping
	RgbPingOrange    = core.RGB{60, 40, 0}     // Very dark orange for ping on whitespace
	RgbPingGreen     = core.RGB{0, 40, 0}      // Very dark green for ping on green char
	RgbPingRed       = core.RGB{50, 15, 15}    // Very dark red for ping on red char
	RgbPingBlue      = core.RGB{15, 25, 50}    // Very dark blue for ping on blue char
	RgbCursorNormal  = core.RGB{255, 165, 0}   // Orange for normal mode
	RgbCursorInsert  = core.RGB{255, 255, 255} // Bright white for insert mode

	// Nugget colors
	RgbNuggetOrange = core.RGB{255, 165, 0}   // Same as insert cursor
	RgbNuggetDark   = core.RGB{101, 67, 33}   // Dark brown for contrast
	RgbCursorError  = core.RGB{255, 0, 0}     // Error Red
	RgbTrailGray    = core.RGB{200, 200, 200} // Light gray base

	// Status bar backgrounds
	RgbModeNormalBg  = core.RGB{135, 206, 250} // Light sky blue
	RgbModeInsertBg  = core.RGB{144, 238, 144} // Light grass green
	RgbModeSearchBg  = core.RGB{255, 165, 0}   // Orange
	RgbModeCommandBg = core.RGB{128, 0, 128}   // Dark purple
	RgbEnergyBg      = core.RGB{255, 255, 255} // Bright white
	RgbBoostBg       = core.RGB{255, 192, 203} // Pink for boost timer
	RgbDecayTimerBg  = core.RGB{200, 50, 50}   // Red for decay timer
	RgbStatusText    = core.RGB{0, 0, 0}       // Dark text for status

	// Runtime Metrics Backgrounds
	RgbFpsBg = core.RGB{0, 255, 255}   // Cyan for FPS
	RgbGtBg  = core.RGB{255, 200, 100} // Light Orange for Game Ticks
	RgbApmBg = core.RGB{50, 205, 50}   // Lime Green for APM

	// Cleaner colors
	RgbCleanerBase  = core.RGB{255, 255, 0}   // Bright yellow
	RgbRemovalFlash = core.RGB{255, 255, 200} // Bright yellow-white flash

	// Shield Colors
	RgbShieldBase = core.RGB{180, 0, 150} // Deep Magenta

	// General colors
	RgbBlack = core.RGB{0, 0, 0} // Black for various uses

	// Audio indicator colors
	RgbAudioMuted   = core.RGB{255, 0, 0} // Bright red when muted
	RgbAudioUnmuted = core.RGB{0, 255, 0} // Bright green when unmuted

	// Energy meter blink colors
	RgbEnergyBlinkBlue  = core.RGB{160, 210, 255} // Blue blink
	RgbEnergyBlinkGreen = core.RGB{120, 255, 120} // Green blink
	RgbEnergyBlinkRed   = core.RGB{255, 140, 140} // Red blink
	RgbEnergyBlinkWhite = core.RGB{255, 255, 255} // White blink

	// Overlay colors
	RgbOverlayBorder = core.RGB{0, 255, 255}   // Bright Cyan for border
	RgbOverlayBg     = core.RGB{20, 20, 30}    // Dark background
	RgbOverlayText   = core.RGB{255, 255, 255} // White text for high contrast
	RgbOverlayTitle  = core.RGB{255, 255, 0}   // Yellow for title
)

// GetHeatMeterColor returns the color for a given position in the heat meter gradient
// progress is 0.0 to 1.0, representing position from start to end
func GetHeatMeterColor(progress float64) core.RGB {
	if progress <= 0.0 {
		return core.RGB{0, 0, 0} // Black for unfilled
	}
	if progress > 1.0 {
		progress = 1.0
	}

	// Rainbow gradient: deep red → orange → yellow → green → cyan → blue → purple/pink
	// Split into segments
	if progress < 0.167 { // Red to Orange
		t := progress / 0.167
		return core.RGB{
			R: uint8(139 + (255-139)*t),
			G: uint8(0 + 69*t),
			B: 0,
		}
	} else if progress < 0.333 { // Orange to Yellow
		t := (progress - 0.167) / 0.166
		return core.RGB{
			R: 255,
			G: uint8(69 + (215-69)*t),
			B: 0,
		}
	} else if progress < 0.500 { // Yellow to Green
		t := (progress - 0.333) / 0.167
		return core.RGB{
			R: uint8(255 - (255-34)*t),
			G: uint8(215 - (215-139)*t),
			B: uint8(34 * t),
		}
	} else if progress < 0.667 { // Green to Cyan
		t := (progress - 0.500) / 0.167
		return core.RGB{
			R: uint8(34 - 34*t),
			G: uint8(139 + (206-139)*t),
			B: uint8(34 + (209-34)*t),
		}
	} else if progress < 0.833 { // Cyan to Blue
		t := (progress - 0.667) / 0.166
		return core.RGB{
			R: uint8(65 * t),
			G: uint8(206 - (206-105)*t),
			B: uint8(209 + (225-209)*t),
		}
	} else { // Blue to Purple/Pink
		t := (progress - 0.833) / 0.167
		return core.RGB{
			R: uint8(65 + (219-65)*t),
			G: uint8(105 + 7*t),
			B: uint8(225 - (225-147)*t),
		}
	}
}

// GetFgForSequence returns the foreground color for a given sequence type and level
func GetFgForSequence(seqType components.SequenceType, level components.SequenceLevel) core.RGB {
	switch seqType {
	case components.SequenceGreen:
		switch level {
		case components.LevelDark:
			return RgbSequenceGreenDark
		case components.LevelNormal:
			return RgbSequenceGreenNormal
		case components.LevelBright:
			return RgbSequenceGreenBright
		}
	case components.SequenceRed:
		switch level {
		case components.LevelDark:
			return RgbSequenceRedDark
		case components.LevelNormal:
			return RgbSequenceRedNormal
		case components.LevelBright:
			return RgbSequenceRedBright
		}
	case components.SequenceBlue:
		switch level {
		case components.LevelDark:
			return RgbSequenceBlueDark
		case components.LevelNormal:
			return RgbSequenceBlueNormal
		case components.LevelBright:
			return RgbSequenceBlueBright
		}
	case components.SequenceGold:
		// Gold sequence always uses bright yellow, regardless of level
		return RgbSequenceGold
	}
	return RgbBackground
}