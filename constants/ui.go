package constants

import "time"

// UI Layout Constants
const (
	// ModeIndicatorWidth is the consistent width for all mode indicators
	ModeIndicatorWidth = 10

	// Mode indicator text (all padded to ModeIndicatorWidth)
	ModeTextNormal  = " NORMAL  "
	ModeTextInsert  = " INSERT  "
	ModeTextSearch  = " SEARCH  "
	ModeTextCommand = " COMMAND "
)

// UI Element Sizes
const (
	// BottomMargin for status bar, X coordinates
	// 1 line for X coordinates, 1 line for status bar
	BottomMargin = 2

	// TopMargin for status bar
	// 1 line for heat meter at top
	TopMargin = 1
)

// Game Timing Constants
const (
	// BoostExtensionDuration is how long each matching color character extends the boost
	BoostExtensionDuration = 500 * time.Millisecond

	// BoostBaseDuration is the initial duration when boost is activated via command
	BoostBaseDuration = 10 * time.Second

	// CommandStatusMessageTimeout is how long command status messages are displayed
	CommandStatusMessageTimeout = 2 * time.Second
)

// UI Symbols
const (
	AudioStr = "♫ "
)