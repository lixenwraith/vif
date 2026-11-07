package core

type GameMode uint8

const (
	ModeNormal GameMode = iota
	ModeVisual
	ModeInsert
	ModeSearch
	ModeCommand
	ModeOverlay
)

// Operator reports a mode the operator opens over play, the command line or an
// overlay: no part of the run, so its world never hears of one and no journal holds it
func (m GameMode) Operator() bool { return m == ModeCommand || m == ModeOverlay }

// ModeNames are what telemetry and a document call each GameMode
var ModeNames = [...]string{
	ModeNormal:  "normal",
	ModeVisual:  "visual",
	ModeInsert:  "insert",
	ModeSearch:  "search",
	ModeCommand: "command",
	ModeOverlay: "overlay",
}

// ModeMask is a set of play modes, which a document names normal, visual and
// insert, or play for all three
type ModeMask uint8

const (
	ModeMaskNormal ModeMask = 1 << ModeNormal
	ModeMaskVisual ModeMask = 1 << ModeVisual
	ModeMaskInsert ModeMask = 1 << ModeInsert
	ModeMaskPlay            = ModeMaskNormal | ModeMaskVisual | ModeMaskInsert
)

// ModeMaskNames are what a document calls each play mode, and the set of them
var ModeMaskNames = map[string]ModeMask{
	"normal": ModeMaskNormal, "visual": ModeMaskVisual, "insert": ModeMaskInsert, "play": ModeMaskPlay,
}

// Has reports whether the set holds mode
func (m ModeMask) Has(mode GameMode) bool { return m&(1<<mode) != 0 }
