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

// modeNames indexes core.GameMode for telemetry display
var ModeNames = [...]string{"normal", "visual", "insert", "search", "command", "overlay"}
