package render

import (
	"github.com/lixenwraith/vi-fighter/components"
)

// HeatGradientLUT holds the pre-calculated rainbow gradient
// 768 bytes, fits in L1 cache alongside other hot data
var HeatGradientLUT [256]RGB

func init() {
	// ... existing init logic if any ...

	// Pre-calculate heat gradient
	for i := 0; i < 256; i++ {
		progress := float64(i) / 255.0
		HeatGradientLUT[i] = calculateHeatColor(progress)
	}
}

// RGB color definitions for all game systems
var (
	// RGB color definitions for sequences - all dark/normal/bright levels have minimum floor to prevent perceptual blackout at low alpha
	RgbSequenceGreenDark   = RGB{15, 130, 15} // Floor R/B to prevent blackout
	RgbSequenceGreenNormal = RGB{20, 200, 20}
	RgbSequenceGreenBright = RGB{50, 255, 50}

	RgbSequenceRedDark   = RGB{180, 40, 40} // Floor G/B
	RgbSequenceRedNormal = RGB{255, 60, 60}
	RgbSequenceRedBright = RGB{255, 100, 100}

	RgbSequenceBlueDark   = RGB{50, 80, 200} // Floor R/G
	RgbSequenceBlueNormal = RGB{80, 130, 255}
	RgbSequenceBlueBright = RGB{120, 170, 255}

	RgbSequenceGold = RGB{255, 255, 0} // Bright Yellow for gold sequence
	RgbDecay        = RGB{0, 139, 139} // Dark Cyan for decay animation
	RgbDrain        = RGB{0, 200, 200} // Vibrant Cyan for drain entity
	RgbMaterialize  = RGB{0, 220, 220} // Bright cyan for materialize head

	RgbLineNumbers     = RGB{180, 180, 180} // Brighter gray
	RgbStatusBar       = RGB{255, 255, 255} // White
	RgbColumnIndicator = RGB{180, 180, 180} // Brighter gray
	RgbBackground      = RGB{26, 27, 38}    // Tokyo Night background

	RgbPingHighlight = RGB{5, 5, 5}       // Almost Black for INSERT mode ping
	RgbPingNormal    = RGB{153, 102, 0}   // Dark orange for NORMAL/SEARCH ping
	RgbPingOrange    = RGB{60, 40, 0}     // Very dark orange for ping on whitespace
	RgbPingGreen     = RGB{0, 40, 0}      // Very dark green for ping on green char
	RgbPingRed       = RGB{50, 15, 15}    // Very dark red for ping on red char
	RgbPingBlue      = RGB{15, 25, 50}    // Very dark blue for ping on blue char
	RgbCursorNormal  = RGB{255, 165, 0}   // Orange for normal mode
	RgbCursorInsert  = RGB{255, 255, 255} // Bright white for insert mode

	// Splash colors
	RgbSplashInsert = RGB{200, 200, 200} // Light gray for insert mode
	RgbSplashNormal = RGB{153, 102, 0}   // Dark orange (ping base) for normal mode

	// Nugget colors
	RgbNuggetOrange = RGB{255, 165, 0}   // Same as insert cursor
	RgbNuggetDark   = RGB{101, 67, 33}   // Dark brown for contrast
	RgbCursorError  = RGB{255, 0, 0}     // Error Red
	RgbTrailGray    = RGB{200, 200, 200} // Light gray base

	// Status bar backgrounds
	RgbModeNormalBg  = RGB{135, 206, 250} // Light sky blue
	RgbModeInsertBg  = RGB{144, 238, 144} // Light grass green
	RgbModeSearchBg  = RGB{255, 165, 0}   // Orange
	RgbModeCommandBg = RGB{128, 0, 128}   // Dark purple
	RgbEnergyBg      = RGB{255, 255, 255} // Bright white
	RgbBoostBg       = RGB{255, 192, 203} // Pink for boost timer
	RgbDecayTimerBg  = RGB{200, 50, 50}   // Red for decay timer
	RgbStatusText    = RGB{0, 0, 0}       // Dark text for status

	// Runtime Metrics Backgrounds
	RgbFpsBg = RGB{0, 255, 255}   // Cyan for FPS
	RgbGtBg  = RGB{255, 200, 100} // Light Orange for Game Ticks
	RgbApmBg = RGB{50, 205, 50}   // Lime Green for APM

	// Cleaner colors
	RgbCleanerBase  = RGB{255, 255, 0}   // Bright yellow
	RgbRemovalFlash = RGB{255, 255, 200} // Bright yellow-white flash

	// Shield Colors
	RgbShieldBase = RGB{180, 0, 150} // Deep Magenta

	// General colors
	RgbBlack = RGB{0, 0, 0} // Black for various uses

	// Audio indicator colors
	RgbAudioMuted   = RGB{255, 0, 0} // Bright red when muted
	RgbAudioUnmuted = RGB{0, 255, 0} // Bright green when unmuted

	// Energy meter blink colors
	RgbEnergyBlinkBlue  = RGB{160, 210, 255} // Blue blink
	RgbEnergyBlinkGreen = RGB{120, 255, 120} // Green blink
	RgbEnergyBlinkRed   = RGB{255, 140, 140} // Red blink
	RgbEnergyBlinkWhite = RGB{255, 255, 255} // White blink

	// Overlay colors
	RgbOverlayBorder = RGB{0, 255, 255}   // Bright Cyan for border
	RgbOverlayBg     = RGB{20, 20, 30}    // Dark background
	RgbOverlayText   = RGB{255, 255, 255} // White text for high contrast
	RgbOverlayTitle  = RGB{255, 255, 0}   // Yellow for title

	// Status bar auxiliary colors
	RgbColorModeIndicator = RGB{200, 200, 200} // Light gray for TC/256 indicator
	RgbGridTimerFg        = RGB{255, 255, 255} // White for grid timer text
	RgbLastCommandText    = RGB{255, 255, 0}   // Yellow for last command indicator
	RgbSearchInputText    = RGB{255, 255, 255} // White for search input
	RgbCommandInputText   = RGB{255, 255, 255} // White for command input
	RgbStatusMessageText  = RGB{200, 200, 200} // Light gray for status messages
)

// calculateHeatColor returns the color for a given position in the heat meter gradient
// Progress is 0.0 to 1.0, representing position from start to end
// Only used for LUT generation
func calculateHeatColor(progress float64) RGB {
	if progress < 0.0 {
		progress = 0.0
	}
	if progress > 1.0 {
		progress = 1.0
	}

	// Rainbow gradient: deep red → orange → yellow → green → cyan → blue → purple/pink
	// Split into segments
	if progress < 0.167 { // Red to Orange
		t := progress / 0.167
		return RGB{
			R: uint8(139 + (255-139)*t),
			G: uint8(0 + 69*t),
			B: 0,
		}
	} else if progress < 0.333 { // Orange to Yellow
		t := (progress - 0.167) / 0.166
		return RGB{
			R: 255,
			G: uint8(69 + (215-69)*t),
			B: 0,
		}
	} else if progress < 0.500 { // Yellow to Green
		t := (progress - 0.333) / 0.167
		return RGB{
			R: uint8(255 - (255-34)*t),
			G: uint8(215 - (215-139)*t),
			B: uint8(34 * t),
		}
	} else if progress < 0.667 { // Green to Cyan
		t := (progress - 0.500) / 0.167
		return RGB{
			R: uint8(34 - 34*t),
			G: uint8(139 + (206-139)*t),
			B: uint8(34 + (209-34)*t),
		}
	} else if progress < 0.833 { // Cyan to Blue
		t := (progress - 0.667) / 0.166
		return RGB{
			R: uint8(65 * t),
			G: uint8(206 - (206-105)*t),
			B: uint8(209 + (225-209)*t),
		}
	} else { // Blue to Purple/Pink
		t := (progress - 0.833) / 0.167
		return RGB{
			R: uint8(65 + (219-65)*t),
			G: uint8(105 + 7*t),
			B: uint8(225 - (225-147)*t),
		}
	}
}

// GetFgForSequence returns the foreground color for a given sequence type and level
func GetFgForSequence(seqType components.SequenceType, level components.SequenceLevel) RGB {
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