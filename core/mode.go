// @lixen: #dev{feature[shield(render,system)]}
package core

type GameMode uint8

const (
	ModeNormal GameMode = iota
	ModeInsert
	ModeSearch
	ModeCommand
	ModeOverlay
)