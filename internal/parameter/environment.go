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
