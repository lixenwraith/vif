// @focus: #conf { visual } #render { ui }
package constants

import "time"

// Layout & Margins
const (
	// BottomMargin for status bar (1 line for X coords, 1 line for status bar)
	BottomMargin = 2

	// TopMargin for status bar (1 line for heat meter)
	TopMargin = 1
)

// Status Bar & Modes
const (
	// ModeIndicatorWidth is the consistent width for all mode indicators
	ModeIndicatorWidth = 10

	// Mode indicator text (padded to ModeIndicatorWidth)
	ModeTextNormal  = " NORMAL  "
	ModeTextInsert  = " INSERT  "
	ModeTextSearch  = " SEARCH  "
	ModeTextCommand = " COMMAND "

	// UI Symbols
	AudioStr = "♫ "

	// CommandStatusMessageTimeout is how long command status messages are displayed
	CommandStatusMessageTimeout = 2 * time.Second
)

// Overlay Configuration
const (
	// OverlayWidthPercent is the percentage of screen width the overlay covers
	OverlayWidthPercent = 0.8

	// OverlayHeightPercent is the percentage of screen height the overlay covers
	OverlayHeightPercent = 0.8

	// OverlayPaddingX is the horizontal padding inside the overlay
	OverlayPaddingX = 2

	// OverlayPaddingY is the vertical padding inside the overlay
	OverlayPaddingY = 1
)

// Splash Layout
const (
	// SplashQuadrantBaseScore is the base score for each quadrant in smart layout
	SplashQuadrantBaseScore = 100

	// SplashCursorPenalty is the penalty applied to the quadrant containing the cursor
	SplashCursorPenalty = 1000

	// SplashGoldSequencePenalty is the penalty applied per gold character in a quadrant
	SplashGoldSequencePenalty = 50
)

// Ping
const (
	PingGridDuration = 500 * time.Millisecond
)