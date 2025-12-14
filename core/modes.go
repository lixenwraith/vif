package core

type GameMode uint8

const (
	ModeNormal GameMode = iota
	ModeInsert
	ModeSearch
	ModeCommand
	ModeOverlay
)