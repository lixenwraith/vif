package parameter

import "time"

// Magic square
const (
	// SquareBlockWidth by SquareBlockHeight cells make a block, about square at the
	// reference cell aspect; SquareBlockGap cells part two blocks
	SquareBlockWidth  = 7
	SquareBlockHeight = 3
	SquareBlockGap    = 1

	// A line fires its kind again each cooldown, counted from its last fire, while it
	// holds three of it: the strike at once, the hold and the boost each for half
	// its cooldown, so neither holds more than half the time
	SquareStrikeCooldown = 2 * time.Second
	SquareHoldDuration   = time.Second
	SquareHoldCooldown   = 2 * time.Second
	SquareBoostDuration  = time.Second
	SquareBoostCooldown  = 2 * time.Second

	// SquareBoostCap is the most boost the shield over a square banks: a strike's
	// cooldown, so a strike that kills many covers the wait for the next and no more
	SquareBoostCap = SquareStrikeCooldown

	// SquareShieldMargin is the columns the shield over the square reaches past its
	// corners, and SquareOrbGap the columns from that shield out to the orbit the
	// kinds' orbs stand on, outside the orbit of the cursor's own
	SquareShieldMargin = 2
	SquareOrbGap       = 6

	// SquareOrbIdlePeriod is a turn of the orbit by the orb of a kind no line holds
	SquareOrbIdlePeriod = 8 * time.Second

	// SquareBoltDuration is how long a fire's bolt stays drawn
	SquareBoltDuration = 300 * time.Millisecond
)
