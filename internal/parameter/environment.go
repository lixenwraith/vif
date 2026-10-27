package parameter

// Wind variation is sampled once per active simulation tick from the
// environment system's Shared RNG stream. Keeping both ranges global makes the
// current effect authoring surface small; they can move into WindStartPayload if
// encounter-specific gust profiles become useful.
const (
	// WindForceVariationRatio varies force uniformly within +/- this fraction.
	WindForceVariationRatio = 0.10

	// WindDirectionVariation is the half-width of the direction spread in
	// radians (approximately five degrees).
	WindDirectionVariation = 0.08726646259971647
)

// DriftLayerFactor is each glyph level's fraction of the drift speed, indexed by
// component.GlyphLevel (dark, normal, bright): nearer text moves faster, which is
// what reads as depth.
var DriftLayerFactor = [3]float64{0.25, 0.5, 1}

const (
	// DriftMaxSpeed caps a drift's pace in cells/second, bounding a layer's steps per
	// tick: a soft cap, past which the bright layer jumps more than five cells a tick.
	DriftMaxSpeed = 100.0

	// DriftEntryBand is how many cells from the upwind edge new glyph lines are
	// placed while a drift is active.
	DriftEntryBand = 4

	// DriftFillSpeed is the drift pace the glyph spawner's interval suits: a faster
	// drift spawns sooner in proportion, so its text keeps its density in passing.
	DriftFillSpeed = 10.0
)

const (
	// CaveMaxSpeed caps a cave's scroll in columns a second, a soft cap: the rock steps
	// a column at a time however many a tick carries, and past it a tick jumps more
	// than ten columns.
	CaveMaxSpeed = 200.0

	// A cave start that leaves its shape unnamed takes these; any map of 18 rows
	// holds them
	CaveDefaultSpeed      = 8.0 // columns per second
	CaveDefaultMinGap     = 8
	CaveDefaultMaxGap     = 18
	CaveDefaultWavelength = 48
)
