package parameter

import "time"

// Magic square
const (
	// SquareBlockWidth by SquareBlockHeight cells make a block, about square at the
	// reference cell aspect; SquareBlockGap cells part two blocks
	SquareBlockWidth  = 7
	SquareBlockHeight = 3
	SquareBlockGap    = 1

	// SquareBlockHP is every block cell's hit points
	SquareBlockHP = 12

	// What an alignment fires: the strike at once, the hold and the shield for their
	// durations; each kind recharges once its effect ends
	SquareStrikeRecharge = 8 * time.Second
	SquareHoldDuration   = 5 * time.Second
	SquareHoldRecharge   = 10 * time.Second
	SquareShieldDuration = 6 * time.Second
	SquareShieldRecharge = 10 * time.Second

	// SquareShieldMargin is the cells the shield reaches past the ellipse through
	// the square's corners
	SquareShieldMargin = 2

	// SquareBoltDuration is how long a strike's bolt stays drawn
	SquareBoltDuration = 300 * time.Millisecond
)
