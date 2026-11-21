package parameter

import "time"

const (
	DevourerInitialHP = 10000
	DevourerGutHP     = 1000
	DevourerGutCap    = 4

	// A touch of the body or a gut, each tick: energy through the shield, heat bare
	DevourerShieldDrain = 10 * QuasarShieldDrain
	DevourerDamageHeat  = 10 * QuasarDamageHeat

	// The body steps a frame every DevourerFrame, the animation tick of ascimage's
	// viewer, so walls, hitbox and shading change together and the ticks between
	// show the frame it stands at. It creeps in from the edges over DevourerCreep,
	// and a killed one draws back to them over DevourerRecede.
	DevourerFrame  = 100 * time.Millisecond
	DevourerCreep  = 3 * time.Second
	DevourerRecede = 2 * time.Second

	// Its bezel is DevourerBezel of the map's shorter side thick, a row two columns,
	// from DevourerEdge column units as it rises, the corners it leaves open rounded
	// by DevourerRound of that. Its walls swell by DevourerSwell of it in bumps
	// DevourerSwellSize of it across, rising and sinking at DevourerSwellRate a
	// second, and breathe by DevourerBreath of it.
	DevourerBezel      = 0.23
	DevourerEdge       = 2.0
	DevourerRound      = 0.25
	DevourerSwell      = 0.2
	DevourerSwellSize  = 0.87
	DevourerSwellRate  = 0.32
	DevourerBreath     = 0.05
	DevourerBreathRate = 0.8 // radians a second

	// An omen lights DevourerOmen before its gut forms, DevourerOmenMin to
	// DevourerOmenMax after the last formed while a slot stands empty; its ends
	// stand half a turn apart round the open space, give or take DevourerOmenSpread
	DevourerOmen       = 2 * time.Second
	DevourerOmenMin    = 4 * time.Second
	DevourerOmenMax    = 6 * time.Second
	DevourerOmenSpread = 1.4 // radians

	// A gut strikes within DevourerGutRadius column units of its centreline, which
	// forms straight and then sags DevourerGutSag and writhes DevourerGutWrithe
	DevourerGutRadius = 2.0
	DevourerGutSag    = 3.2
	DevourerGutWrithe = 1.1

	// A smaller map leaves no open space between the walls
	DevourerMinWidth  = 40
	DevourerMinHeight = 16

	// DevourerRecedeCap draw back at once
	DevourerRecedeCap = 2
)

// DevourerRipples run round its walls, a whole number of each, so they close on
// themselves; Amp is a share of the bezel and Speed radians a second
var DevourerRipples = [...]struct{ Count, Speed, Amp, Phase float64 }{
	{11, -0.9, 0.067, 0}, {5, 0.55, 0.036, 1.7},
}
