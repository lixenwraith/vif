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