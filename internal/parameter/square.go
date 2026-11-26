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
	// walk back. A strike reaches SquareStrikeReach shield radii from the middle.
	SquareStrikeCooldown = 2 * time.Second
	SquareStrikeReach    = 2.0
	SquareBoostDuration  = time.Second
	SquareBoostCooldown  = 8 * time.Second

	// SquareWindForce is the gust a defense blows out from the square's middle for
	// SquareWindDuration, fading to nothing SquareWindReach columns past the shield:
	// it throws what stands by the shield back toward the map's edge, a light body
	// further than a heavy one
	SquareWindForce    = 1000.0
	SquareWindReach    = 30.0
	SquareWindDuration = 500 * time.Millisecond

	// SquareTempestForce is the wind a tempest turns round the square for
	// SquareTempestDuration of each SquareTempestCooldown, fading to nothing
	// SquareTempestReach columns past the shield: a burst that carries a body walking
	// in a quarter to a third round the shield on a small map, and holds most circling
	// on a large one
	SquareTempestForce    = 600.0
	SquareTempestReach    = 40.0
	SquareTempestDuration = 500 * time.Millisecond
	SquareTempestCooldown = 2 * time.Second

	// A square's dust meter charges a second a second, and SquareDustKill for each
	// kill its cursor makes, up to SquareDustFull, which a magic state, every row
	// and column summing alike, fills at once. Its next gust sheds that share of
	// SquareDustShed motes round the shield and empties it.
	SquareDustFull = 40 * time.Second
	SquareDustKill = time.Second
	SquareDustShed = 36

	// SquareShieldMargin is the columns the shield over the square reaches past its
	// corners, which keeps its cursor's orbs clear of the legend, and SquareOrbGap
	// the columns from that shield out to the orbit the kinds' orbs stand on, their
	// coronas two columns clear of the cursor's own orbs'
	SquareShieldMargin = 4
	SquareOrbGap       = OrbOrbitRadiusX - PlayerShieldRadius + 2*OrbCoronaRadiusX + 2

	// SquareOrbIdlePeriod is a turn of the orbit by the orb of a kind no line holds
	SquareOrbIdlePeriod = 8 * time.Second
)
