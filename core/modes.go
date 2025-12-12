// @lixen: #focus{flow[state,mode],control[types,mode],input[types,mode]}
// @lixen: #interact{state[mode]}
package core

type GameMode uint8

const (
	ModeNormal GameMode = iota
	ModeInsert
	ModeSearch
	ModeCommand
	ModeOverlay
)