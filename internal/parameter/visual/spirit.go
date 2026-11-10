package visual

import "github.com/lixenwraith/vif/pkg/vmath"

// Spirit trail presentation.
const (
	SpiritTrailSteps = 10         // head plus nine trailing segments
	SpiritTrailLag   = 1.0 / 60.0 // progress gap between segments

	SpiritHeadFloor       = 0.5 // the head's intensity as it sets off, full at its target
	SpiritTrailBoost      = 1.3 // holds the segments nearest the head at full light
	SpiritTrailAlphaFloor = 0.4 // the blend of a segment gone dark
	SpiritGradientSpan    = 128 // heat gradient entries a flight cycles through

	// SpiritSpin is the turn a converging spirit spirals through, alternating in
	// direction by its start cell so an implosion reads as chaotic
	SpiritSpin = 1.5 * vmath.TwoPi
)

// Spirit scatter presentation: a dead body's glyphs thrown out from its middle.
// Distances are in cell widths.
const (
	SpiritScatterReach  = 1.5 // every glyph flies this far
	SpiritScatterGrowth = 0.4 // and this much more per cell of the body's half-diagonal
	SpiritScatterSpread = 2.5 // and up to this much more by its own draw
	SpiritScatterTurn   = 0.5 // radians its way out turns by its own draw, either way

	// ShellShardSpacing is the length of a killed body's shell each shard of its
	// burst stands for
	ShellShardSpacing = 2.0
)
