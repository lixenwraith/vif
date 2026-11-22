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

	// A line fires its kind again each cooldown, counted from its last fire, while it
	// holds three of it: the strike at once, the hold and the shield each for half
	// its cooldown, so neither holds more than half the time
	SquareStrikeCooldown = 2 * time.Second
	SquareHoldDuration   = time.Second
	SquareHoldCooldown   = 2 * time.Second
	SquareShieldDuration = time.Second
	SquareShieldCooldown = 2 * time.Second

	// SquareShieldMargin is the cells the shield reaches past the ellipse through
	// the square's corners
	SquareShieldMargin = 2

	// SquareBoltDuration is how long a fire's bolt stays drawn
	SquareBoltDuration = 300 * time.Millisecond
)
