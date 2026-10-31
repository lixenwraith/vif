package parameter

import "time"

// Layout & Margins
const (
	// BottomMargin is the column gutter and the status bar below the map
	BottomMargin = 1 + StatusRows

	// StatusRows is the status bar on every host: the status items' row, then the
	// line of mode, keys, input and messages, the bottom row a keyboard strip repeats
	StatusRows = 2

	// TopMargin for status bar (1 line for heat meter)
	TopMargin = 1

	// LeftMargin is the row gutter: one column holding the relative row digit
	LeftMargin = 1
)

// WindowCols and WindowRows are the grid a desktop window opens at; the player
// resizes it by whole cells from there
const (
	WindowCols  = 120
	WindowRows  = 40
	WindowTitle = "vif"
)

// CellLeast and CellMost bound the cell a zoom reaches, its height in dp (a CSS
// pixel in a browser), in steps of CellStep: the range of Android's pinch and the
// page's alike, and of vif.toml's [presentation] cell
const (
	CellLeast = 4.0
	CellMost  = 40.0
	CellStep  = 0.5
)

// AudioText is the audio indicator by channel mask (muted, effects, music, both): a letter
// every terminal font has, which says the state without its color
var AudioText = [4]string{" X ", " S ", " M ", " O "}

// StatusKind ranks a status message. One replaces the message shown unless that one
// outranks it and has been up for less than its kind's Min; Shown is how long a
// message stays. Both are wall time, so a message is read at the rate it was written.
type StatusKind uint8

const (
	StatusNotice   StatusKind = iota // an answer to the player, or what the run reports
	StatusProgress                   // what the run is waiting on, until it says otherwise
	StatusAlert                      // a failure or session change the player did not ask about
	StatusKindCount
)

var StatusKinds = [StatusKindCount]struct{ Min, Shown time.Duration }{
	StatusNotice:   {Shown: 5 * time.Second},
	StatusProgress: {Shown: NetworkSessionRequestTimeout}, // the longest wait a run reports on
	StatusAlert:    {Min: 3 * time.Second, Shown: 8 * time.Second},
}

// Status Bar & Modes
const (
	// Mode indicator text (padded to ModeIndicatorWidth)
	ModeTextNormal  = " NORMAL "
	ModeTextVisual  = " VISUAL "
	ModeTextInsert  = " INSERT "
	ModeTextSearch  = " SEARCH "
	ModeTextCommand = "  CMD   "
	ModeTextRecord  = " REC"

	// StatusKeyWidth is the key cell after the mode label: the keys of a pending
	// sequence or the last one completed, newest last, in a place no message takes.
	StatusKeyWidth = 5

	// StatusCursorBlinkDuration is the blink duration of the cursor when visible in status bar in search and command modes
	StatusCursorBlinkDuration = 250 * time.Millisecond

	// StatusNetworkHoldDuration is the shortest interval the session cell may
	// change over. Its inputs move on the correction cadence and the cell's width
	// reflows every item beside it, so an unheld cell is unreadable.
	StatusNetworkHoldDuration = 500 * time.Millisecond

	// StatusNetworkDownDuration is how long the badge says a link went down before
	// it leaves, as a notice does: the loss was read, and the run plays on alone
	StatusNetworkDownDuration = 10 * time.Second

	// StatusCursorChar is status bar cursor character that blinks
	StatusCursorChar = '█'

	// StatusNetLatencyWarn/StatusNetLatencyBad colour the session badge by round
	// trip, the reading a player already has from every other networked game. Bad
	// is a round trip whose way out outlasts the default playout lead, past which
	// the lead grows and actions land later; warning is half of that.
	StatusNetLatencyBad  = 2 * NetworkBarrierDelayTicks * GameUpdateInterval
	StatusNetLatencyWarn = StatusNetLatencyBad / 2

	// StatusNetLossWarnPct is the probe loss a badge names rather than absorbs.
	StatusNetLossWarnPct = 2
)

// Start Screen: the terminal's and the desktop window's, laid out as Android's and the
// page's are, with the prompt as lixen.com's button and help in the top right corner
const (
	// StartLogoShare is the mark's side as a share of the screen's shorter side
	StartLogoShare = 0.4

	// StartLogoMinRows is the smallest mark worth drawing; less room shows the prompt alone
	StartLogoMinRows = 4

	// StartPromptGap is the rows between the mark and the prompt's button
	StartPromptGap = 1

	// StartButtonPadX is the columns either side of the prompt inside its button
	StartButtonPadX = 3

	// StartHelpWord follows the key help is bound to on the help button, "F1 help";
	// StartHelpMarginX/Y keep the button off the corner
	StartHelpWord    = "help"
	StartHelpMarginX = 2
	StartHelpMarginY = 1
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
	OverlayUsableWidth    = 12
	OverlayUsableHeight   = 5
	OverlayMenuDetailRows = 4 // Separator and selected-setting explanation

	// OverlayMenuRule draws a menu section's title line; OverlayText* fill an empty text box
	OverlayMenuRule        = '─'
	OverlayTextPlaceholder = "Enter to type"
	OverlayTextUnavailable = "unavailable"

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

	// OverlayWheelRows is the rows a wheel step scrolls an overlay
	OverlayWheelRows = 3
)

// Overlay Cards (telemetry)
const (
	// OverlayCardFrameRows is the card rows consumed by its top and bottom border
	OverlayCardFrameRows = 2

	// OverlayCardMaxEntries bounds telemetry cards to a navigable viewport height
	OverlayCardMaxEntries = 15

	// OverlayCardGapX/OverlayCardGapY is the masonry spacing between cards
	OverlayCardGapX = 2
	OverlayCardGapY = 1

	// Masonry column breakpoints: content width at which each column count applies
	OverlayCardCols4 = 140
	OverlayCardCols3 = 100
	OverlayCardCols2 = 60

	// OverlayPinMarker prefixes the title of a pinned card; console fonts lack it, so 256
	// colors draw OverlayPinMarker256
	OverlayPinMarker    = '●'
	OverlayPinMarker256 = '•'
)

// Overlay About
const (
	// AboutLogoRows is the mark's height in the about window; its width follows the cell aspect
	AboutLogoRows = 13

	// AboutLogoGap is the columns between the mark and the text beside it
	AboutLogoGap = 3

	// AboutStackWidth is the content width below which the text goes under the mark
	AboutStackWidth = 50

	// AboutMinWidth/AboutMinHeight is the content below which the window shows the name
	// and description alone
	AboutMinWidth  = 30
	AboutMinHeight = 10
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
	OverlayHintsMenu = []string{
		"j/k select · h/l change · ENTER choose · / search · ESC back",
		"↑↓ select · ←→ change · ENTER choose · / search · ESC back",
		"↑↓ select · ENTER choose · / search · ESC back",
		"/ search · ESC back",
		"ESC back",
	}
	// Menu hints behind the search prompt, once kept and while editing
	OverlayHintsMenuFilter = []string{
		"ESC clear · / edit · j/k select · h/l change · ENTER choose",
		"ESC clear · / edit · ENTER choose",
		"ESC clear",
	}
	OverlayHintsMenuFilterEdit = []string{"↑↓ select · ENTER keep · ESC clear", "ENTER keep · ESC clear", "ESC clear"}
	// Menu hints while a row's text box is typed in
	OverlayHintsMenuText = []string{
		"←→ cursor · HOME/END · ENTER submit · ↑↓ leave · ESC cancel",
		"ENTER submit · ↑↓ leave · ESC cancel",
		"ENTER submit · ESC cancel",
		"ESC cancel",
	}
	OverlayHintsForm = []string{
		"↑↓ field · ←→ cursor · HOME/END · ENTER submit · ESC cancel",
		"↑↓ field · ENTER submit · ESC cancel",
		"ENTER submit · ESC cancel",
		"ESC cancel",
	}
	OverlayHintsCards = []string{
		"ESC close · / filter · hjkl select · SPACE pin to HUD · :t save snapshot · :h keys",
		"ESC close · / filter · hjkl select · SPACE pin to HUD · :t save snapshot",
		"ESC close · / filter · SPACE pin to HUD",
		"ESC close · / filter",
		"ESC close",
	}
	// Card hints behind the filter prompt, while editing and once kept
	OverlayHintsFilterEdit = []string{"ENTER keep · ESC clear", "ESC clear"}
	OverlayHintsFilter     = []string{
		"ESC close · / edit · hjkl select · SPACE pin to HUD",
		"ESC close · / edit · SPACE pin",
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
	// HudMarginX/HudMarginY is the gap from the viewport's top-left corner
	HudMarginX = 1
	HudMarginY = 0

	// HudMinWidth/HudMaxWidth bound the panel width
	HudMinWidth = 12
	HudMaxWidth = 30

	// HudColumnGap is the columns between a metric name and its value
	HudColumnGap = 1

	// HudWrapGap is the horizontal space between height-wrapped HUD columns
	HudWrapGap = 2

	// HudWidthShrinkFrames delays contraction while allowing immediate expansion
	HudWidthShrinkFrames = 90

	// HudBgAlpha is the panel background opacity over the game area
	HudBgAlpha = 0.96
)
