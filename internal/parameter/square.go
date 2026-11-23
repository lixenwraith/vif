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
	// holds three of it. The boost's cooldown leaves what its gust threw the time to
	// walk back
	SquareStrikeCooldown = 2 * time.Second
	SquareBoostDuration  = time.Second
	SquareBoostCooldown  = 8 * time.Second

	// SquareWindForce is the gust a defense blows out from the square's middle for
	// SquareWindDuration, fading to nothing SquareWindReach columns past the shield:
	// it throws what stands by the shield back toward the map's edge, a light body
	// further than a heavy one
	SquareWindForce    = 1000.0
	SquareWindReach    = 30.0
	SquareWindDuration = 500 * time.Millisecond

	// SquareWhirlForce is the wind a whirl turns round the square for
	// SquareWhirlDuration of each SquareWhirlCooldown, fading to nothing
	// SquareWhirlReach columns past the shield: it carries a body round the shield
	// as it walks in, a light one further than a heavy one
	SquareWhirlForce    = 60.0
	SquareWhirlReach    = 60.0
	SquareWhirlDuration = 1500 * time.Millisecond
	SquareWhirlCooldown = 3 * time.Second

	// A square's magic meter charges a second a second, and SquareMagicKill for each
	// kill its cursor makes, up to SquareMagicFull, which a magic state, every row
	// and column summing alike, fills at once. Its next gust sheds that share of
	// SquareMagicDust dust round the shield and empties it.
	SquareMagicFull = 40 * time.Second
	SquareMagicKill = time.Second
	SquareMagicDust = 36

	// SquareBoostCap is the most boost the shield over a square banks: a strike's
	// cooldown, so a strike that kills many covers the wait for the next and no more
	SquareBoostCap = SquareStrikeCooldown

	// SquareShieldMargin is the columns the shield over the square reaches past its
	// corners, which keeps its cursor's orbs clear of the legend, and SquareOrbGap
	// the columns from that shield out to the orbit the kinds' orbs stand on, their
	// coronas two columns clear of the cursor's own orbs'
	SquareShieldMargin = 4
	SquareOrbGap       = OrbOrbitRadiusX - PlayerShieldRadius + 2*OrbCoronaRadiusX + 2

	// SquareOrbIdlePeriod is a turn of the orbit by the orb of a kind no line holds
	SquareOrbIdlePeriod = 8 * time.Second
)
