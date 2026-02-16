package parameter

import "time"

// Layout & Margins
const (
	// BottomMargin for status bar (1 line for X coords, 1 line for status bar)
	BottomMargin = 2

	// TopMargin for status bar (1 line for heat meter)
	TopMargin = 1

	// LeftMargin (1 left padding + 1 digit + 1 right padding)
	LeftMargin = 3
)

// Status Bar & Modes
const (
	// Mode indicator text (padded to ModeIndicatorWidth)
	ModeTextNormal  = " NORMAL "
	ModeTextVisual  = " VISUAL "
	ModeTextInsert  = " INSERT "
	ModeTextSearch  = " SEARCH "
	ModeTextCommand = "  CMD   "
	ModeTextRecord  = " REC"

	// UI Symbols
	AudioStr = "♫ "

	// CommandStatusMessageTimeout is how long command status messages are displayed
	CommandStatusMessageTimeout = 2 * time.Second

	// StatusCursorBlinkDuration is the blink duration of the cursor when visible in status bar in search and command modes
	StatusCursorBlinkDuration = 250 * time.Millisecond

	// StatusCursorChar is status bar cursor character that blinks
	StatusCursorChar = '█'
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
	// SplashMinDistance is the minimum distance from cursor for magnifier placement
	SplashMinDistance = 25
)

// Ping
const (
	PingBoundFactor = 2

	PingGridDuration = 500 * time.Millisecond
)