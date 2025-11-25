package render

import (
	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
)

// RGB color definitions for sequences - dark/normal/bright levels
var (
	RgbSequenceGreenDark   = tcell.NewRGBColor(0, 130, 0)   // Dark Green
	RgbSequenceGreenNormal = tcell.NewRGBColor(0, 200, 0)   // Normal Green
	RgbSequenceGreenBright = tcell.NewRGBColor(50, 255, 50) // Bright Green

	RgbSequenceRedDark   = tcell.NewRGBColor(180, 50, 50)   // Dark Red
	RgbSequenceRedNormal = tcell.NewRGBColor(255, 80, 80)   // Normal Red
	RgbSequenceRedBright = tcell.NewRGBColor(255, 120, 120) // Bright Red

	RgbSequenceBlueDark   = tcell.NewRGBColor(60, 100, 200)  // Dark Blue
	RgbSequenceBlueNormal = tcell.NewRGBColor(100, 150, 255) // Normal Blue
	RgbSequenceBlueBright = tcell.NewRGBColor(140, 190, 255) // Bright Blue

	RgbSequenceGold = tcell.NewRGBColor(255, 255, 0) // Bright Yellow for gold sequence
	RgbDecayFalling = tcell.NewRGBColor(0, 139, 139) // Dark Cyan for decay animation
	RgbDrain        = tcell.NewRGBColor(0, 200, 200) // Vibrant Cyan for drain entity

	RgbLineNumbers     = tcell.NewRGBColor(180, 180, 180) // Brighter gray
	RgbStatusBar       = tcell.NewRGBColor(255, 255, 255) // White
	RgbColumnIndicator = tcell.NewRGBColor(180, 180, 180) // Brighter gray
	RgbBackground      = tcell.NewRGBColor(26, 27, 38)    // Tokyo Night background

	RgbPingHighlight = tcell.NewRGBColor(50, 50, 50)    // Very dark gray for ping
	RgbPingOrange    = tcell.NewRGBColor(60, 40, 0)     // Very dark orange for ping on whitespace
	RgbPingGreen     = tcell.NewRGBColor(0, 40, 0)      // Very dark green for ping on green char
	RgbPingRed       = tcell.NewRGBColor(50, 15, 15)    // Very dark red for ping on red char
	RgbPingBlue      = tcell.NewRGBColor(15, 25, 50)    // Very dark blue for ping on blue char
	RgbCursorNormal  = tcell.NewRGBColor(255, 165, 0)   // Orange for normal mode
	RgbCursorInsert  = tcell.NewRGBColor(255, 255, 255) // Bright white for insert mode

	// Nugget colors
	RgbNuggetOrange = tcell.NewRGBColor(255, 165, 0)   // Same as insert cursor
	RgbNuggetDark   = tcell.NewRGBColor(101, 67, 33)   // Dark brown for contrast
	RgbCursorError  = tcell.NewRGBColor(255, 0, 0)     // Error Red
	RgbTrailGray    = tcell.NewRGBColor(200, 200, 200) // Light gray base

	// Status bar backgrounds
	RgbModeNormalBg  = tcell.NewRGBColor(135, 206, 250) // Light sky blue
	RgbModeInsertBg  = tcell.NewRGBColor(144, 238, 144) // Light grass green
	RgbModeSearchBg  = tcell.NewRGBColor(255, 165, 0)   // Orange
	RgbModeCommandBg = tcell.NewRGBColor(128, 0, 128)   // Dark purple
	RgbEnergyBg      = tcell.NewRGBColor(255, 255, 255) // Bright white
	RgbBoostBg       = tcell.NewRGBColor(255, 192, 203) // Pink for boost timer
	RgbDecayTimerBg  = tcell.NewRGBColor(200, 50, 50)   // Red for decay timer
	RgbStatusText    = tcell.NewRGBColor(0, 0, 0)       // Dark text for status

	// Cleaner colors
	RgbCleanerBase  = tcell.NewRGBColor(255, 255, 0)   // Bright yellow
	RgbRemovalFlash = tcell.NewRGBColor(255, 255, 200) // Bright yellow-white flash
)

// GetHeatMeterColor returns the color for a given position in the heat meter gradient
// progress is 0.0 to 1.0, representing position from start to end
func GetHeatMeterColor(progress float64) tcell.Color {
	if progress <= 0.0 {
		return tcell.NewRGBColor(0, 0, 0) // Black for unfilled
	}
	if progress > 1.0 {
		progress = 1.0
	}

	// Rainbow gradient: deep red → orange → yellow → green → cyan → blue → purple/pink
	// Split into segments
	if progress < 0.167 { // Red to Orange
		t := progress / 0.167
		r := int32(139 + (255-139)*t)
		g := int32(0 + (69-0)*t)
		b := int32(0)
		return tcell.NewRGBColor(r, g, b)
	} else if progress < 0.333 { // Orange to Yellow
		t := (progress - 0.167) / 0.166
		r := int32(255)
		g := int32(69 + (215-69)*t)
		b := int32(0)
		return tcell.NewRGBColor(r, g, b)
	} else if progress < 0.500 { // Yellow to Green
		t := (progress - 0.333) / 0.167
		r := int32(255 - (255-34)*t)
		g := int32(215 - (215-139)*t)
		b := int32(0 + (34-0)*t)
		return tcell.NewRGBColor(r, g, b)
	} else if progress < 0.667 { // Green to Cyan
		t := (progress - 0.500) / 0.167
		r := int32(34 - (34-0)*t)
		g := int32(139 + (206-139)*t)
		b := int32(34 + (209-34)*t)
		return tcell.NewRGBColor(r, g, b)
	} else if progress < 0.833 { // Cyan to Blue
		t := (progress - 0.667) / 0.166
		r := int32(0 + (65-0)*t)
		g := int32(206 - (206-105)*t)
		b := int32(209 + (225-209)*t)
		return tcell.NewRGBColor(r, g, b)
	} else { // Blue to Purple/Pink
		t := (progress - 0.833) / 0.167
		r := int32(65 + (219-65)*t)
		g := int32(105 - (105-112)*t)
		b := int32(225 - (225-147)*t)
		return tcell.NewRGBColor(r, g, b)
	}
}

// GetStyleForSequence returns the style for a given sequence type and level
func GetStyleForSequence(seqType components.SequenceType, level components.SequenceLevel) tcell.Style {
	baseStyle := tcell.StyleDefault.Background(RgbBackground)
	switch seqType {
	case components.SequenceGreen:
		switch level {
		case components.LevelDark:
			return baseStyle.Foreground(RgbSequenceGreenDark)
		case components.LevelNormal:
			return baseStyle.Foreground(RgbSequenceGreenNormal)
		case components.LevelBright:
			return baseStyle.Foreground(RgbSequenceGreenBright)
		}
	case components.SequenceRed:
		switch level {
		case components.LevelDark:
			return baseStyle.Foreground(RgbSequenceRedDark)
		case components.LevelNormal:
			return baseStyle.Foreground(RgbSequenceRedNormal)
		case components.LevelBright:
			return baseStyle.Foreground(RgbSequenceRedBright)
		}
	case components.SequenceBlue:
		switch level {
		case components.LevelDark:
			return baseStyle.Foreground(RgbSequenceBlueDark)
		case components.LevelNormal:
			return baseStyle.Foreground(RgbSequenceBlueNormal)
		case components.LevelBright:
			return baseStyle.Foreground(RgbSequenceBlueBright)
		}
	case components.SequenceGold:
		// Gold sequence always uses bright yellow, regardless of level
		return baseStyle.Foreground(RgbSequenceGold)
	}
	return baseStyle
}