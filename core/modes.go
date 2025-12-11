// @focus: #flow { state } #control { types } #input { types }
package core

type GameMode uint8

const (
	ModeNormal GameMode = iota
	ModeInsert
	ModeSearch
	ModeCommand
	ModeOverlay
)