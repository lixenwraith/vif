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

	// StatusMessageDefaultTimeout is how long status message on status bar lasts
	StatusMessageDefaultTimeout = 2 * time.Second

	// StatusCursorBlinkDuration is the blink duration of the cursor when visible in status bar in search and command modes
	StatusCursorBlinkDuration = 250 * time.Millisecond

	// StatusCursorChar is status bar cursor character that blinks
	StatusCursorChar = '█'
)

// Overlay Window
const (
	// OverlayWidthPercent is the fraction of screen width the overlay covers
	OverlayWidthPercent = 0.8

	// OverlayHeightPercent is the fraction of screen height the overlay covers
	OverlayHeightPercent = 0.8

	// OverlayMinWidth/OverlayMinHeight are floors applied only when the screen allows them
	OverlayMinWidth  = 40
	OverlayMinHeight = 15

	// OverlayMaxWidth/OverlayMaxHeight cap the window on large terminals; 0 disables
	OverlayMaxWidth  = 160
	OverlayMaxHeight = 60

	// OverlayScreenMarginX/OverlayScreenMarginY is the gap kept to the screen edge
	OverlayScreenMarginX = 1
	OverlayScreenMarginY = 1

	// OverlayUsableWidth/OverlayUsableHeight is the smallest window worth drawing
	OverlayUsableWidth  = 12
	OverlayUsableHeight = 5

	// OverlayPaddingX is the padding inside the left and right border
	OverlayPaddingX = 2

	// OverlayPaddingY is the padding above the content; the hint owns the last interior row
	OverlayPaddingY = 1

	// OverlayHintRows is the interior rows reserved for the hint line
	OverlayHintRows = 1

	// OverlayScrollbarMinWidth is the content width below which the scrollbar column is dropped
	OverlayScrollbarMinWidth = 24

	// OverlayScrollbarMargin is the columns between the scrollbar and the right border
	OverlayScrollbarMargin = 1

	// OverlayPageOverlap is the rows kept in view across a page scroll
	OverlayPageOverlap = 1
)

// Overlay Cards (debug)
const (
	// OverlayCardFrameRows is the card rows consumed by its top and bottom border
	OverlayCardFrameRows = 2

	// OverlayCardGapX/OverlayCardGapY is the masonry spacing between cards
	OverlayCardGapX = 2
	OverlayCardGapY = 1

	// Masonry column breakpoints: content width at which each column count applies
	OverlayCardCols4 = 140
	OverlayCardCols3 = 100
	OverlayCardCols2 = 60

	// OverlayPinMarker prefixes the title of a pinned card
	OverlayPinMarker = '●'
)

// Overlay Document (help)
const (
	// HelpMaxWidth caps the document width so wide terminals keep readable lines
	HelpMaxWidth = 110

	// HelpKeyMaxWidth caps the key column; longer keys stack above their description
	HelpKeyMaxWidth = 24

	// HelpMinTextWidth is the description width below which every entry stacks
	HelpMinTextWidth = 28

	// HelpColumnGap is the columns between key and description
	HelpColumnGap = 2

	// HelpIndent is the left indent of section content
	HelpIndent = 1

	// HelpStackIndent is the indent of a stacked description
	HelpStackIndent = 3

	// HelpSectionGap is the blank rows before a section header
	HelpSectionGap = 1
)

// Overlay hint variants, longest first; the renderer draws the widest that fits
const (
	OverlayHintFull  = "ESC close · j/k scroll · PgUp/PgDn page"
	OverlayHintShort = "ESC close · j/k scroll"
	OverlayHintMin   = "ESC close"
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

// Overlay hint variants per layout, longest first; the renderer draws the widest that fits
var (
	OverlayHintsCards = []string{
		"ESC close · hjkl select · SPACE pin · PgUp/PgDn page",
		"ESC close · hjkl select · SPACE pin",
		"ESC close · SPACE pin",
		"ESC close",
	}
	OverlayHintsDoc = []string{
		"ESC close · j/k scroll · PgUp/PgDn page",
		"ESC close · j/k scroll",
		"ESC close",
	}
	OverlayHintsAbout = []string{"ESC close"}
)

// Pinned Stats HUD
const (
	// HudMarginX/HudMarginY is the gap from the viewport's top-right corner
	HudMarginX = 1
	HudMarginY = 0

	// HudMinWidth/HudMaxWidth bound the panel width
	HudMinWidth = 12
	HudMaxWidth = 30

	// HudColumnGap is the columns between a metric name and its value
	HudColumnGap = 1

	// HudBgAlpha is the panel background opacity over the game area
	HudBgAlpha = 0.8
)
