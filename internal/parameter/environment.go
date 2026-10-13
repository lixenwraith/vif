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
	// DriftAspectY scales the row component of a drift, rows being about twice as
	// tall as columns are wide, so one speed reads the same in every direction.
	DriftAspectY = 0.5

	// DriftMaxSpeed caps a drift start in cells/second, bounding a layer's steps
	// per tick.
	DriftMaxSpeed = 20.0

	// DriftEntryBand is how many cells from the upwind edge new glyph lines are
	// placed while a drift is active.
	DriftEntryBand = 4
)
