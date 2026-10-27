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

// ModeNames are what telemetry and a document call each GameMode
var ModeNames = [...]string{
	ModeNormal:  "normal",
	ModeVisual:  "visual",
	ModeInsert:  "insert",
	ModeSearch:  "search",
	ModeCommand: "command",
	ModeOverlay: "overlay",
}
