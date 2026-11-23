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

	// SquareWindForce is the gust a defense blows out from the square's middle for
	// SquareWindDuration, fading to nothing SquareWindReach columns past the shield:
	// it turns back what is near the shield, a light body further than a heavy one
	SquareWindForce    = 70.0
	SquareWindReach    = 14.0
	SquareWindDuration = 400 * time.Millisecond

	// A turn that leaves every row and column summing alike, a magic state, sheds
	// SquareMagicDust dust from each block, then none for SquareMagicCooldown
	SquareMagicDust     = 4
	SquareMagicCooldown = 20 * time.Second

	// SquareBoostCap is the most boost the shield over a square banks: a strike's
	// cooldown, so a strike that kills many covers the wait for the next and no more
	SquareBoostCap = SquareStrikeCooldown

	// SquareShieldMargin is the columns the shield over the square reaches past its
	// corners, and SquareOrbGap the columns from that shield out to the orbit the
	// kinds' orbs stand on, their coronas two columns clear of the cursor's own orbs'
	SquareShieldMargin = 2
	SquareOrbGap       = OrbOrbitRadiusX - PlayerShieldRadius + 2*OrbCoronaRadiusX + 2

	// SquareOrbIdlePeriod is a turn of the orbit by the orb of a kind no line holds
	SquareOrbIdlePeriod = 8 * time.Second

	// SquareBoltDuration is how long a fire's bolt stays drawn
	SquareBoltDuration = 300 * time.Millisecond
)
