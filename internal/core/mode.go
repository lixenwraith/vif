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
