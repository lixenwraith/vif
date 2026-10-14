package vmath

import "math"

var (
	// Float64 look-up tables avoid per-call transcendental calculations.
	SinLUT      [LUTSize]float64
	CosLUT      [LUTSize]float64
	Atan2LUT    [LUTSize]float64
	ExpDecayLUT [ExpLUTSize]float64
)

// radToIndex is a precalculated multiplier to convert radians to a LUT index
const radToIndex = float64(LUTSize) / (2.0 * math.Pi)

func init() {
	// Initialize trigonometric LUTs.
	for i := range LUTSize {
		rad := 2.0 * math.Pi * float64(i) / float64(LUTSize)
		SinLUT[i] = math.Sin(rad)
		CosLUT[i] = math.Cos(rad)

		// Atan2 LUT maps ratio [0,1] to angle [0, π/4]
		ratio := float64(i) / float64(LUTMask)
		Atan2LUT[i] = math.Atan(ratio)
	}

	// Initialize the exponential decay LUT.
	for i := range ExpLUTSize {
		x := float64(i) * float64(ExpLUTMaxInput) / float64(ExpLUTSize-1)
		ExpDecayLUT[i] = math.Exp(-x / ExpLUTDecayK)
	}
}

// Sin returns the sine of angleRad using O(1) LUT lookup.
func Sin(angleRad float64) float64 {
	// Floor keeps negative angles in the preceding angular bin instead of
	// truncating them toward zero.
	idx := int(math.Floor(angleRad*radToIndex)) & LUTMask
	return SinLUT[idx]
}

// Cos returns the cosine of angleRad using O(1) LUT lookup.
func Cos(angleRad float64) float64 {
	idx := int(math.Floor(angleRad*radToIndex)) & LUTMask
	return CosLUT[idx]
}

// Atan2 returns angle in [0, 2π) for (dy, dx) using LUT.
// The octant reduction keeps the lookup table compact.
func Atan2(dy, dx float64) float64 {
	if dx == 0 && dy == 0 {
		return 0.0
	}

	adx := math.Abs(dx)
	ady := math.Abs(dy)

	var baseAngle float64
	if adx >= ady {
		if adx == 0 {
			baseAngle = 0
		} else {
			idx := int((ady / adx) * float64(LUTMask))
			baseAngle = Atan2LUT[idx]
		}
	} else {
		idx := int((adx / ady) * float64(LUTMask))
		baseAngle = (math.Pi / 2.0) - Atan2LUT[idx]
	}

	// Map back to correct quadrant
	if dx > 0 {
		if dy >= 0 {
			return baseAngle // Q1
		}
		if baseAngle == 0 {
			return 0.0 // Q4 boundary: 2pi wraps to 0
		}
		return (2.0 * math.Pi) - baseAngle // Q4
	} else if dx < 0 {
		if dy >= 0 {
			return math.Pi - baseAngle // Q2
		}
		return math.Pi + baseAngle // Q3
	}

	// Perfect vertical cases
	if dy > 0 {
		return math.Pi / 2.0
	}
	return 3.0 * math.Pi / 2.0
}

// ExpDecay returns e^(-count/k) using LUT linear interpolation.
// The result is clamped to the LUT domain and lies in [0.0, 1.0].
func ExpDecay(count int) float64 {
	if count <= 0 {
		return 1.0
	}
	if count >= ExpLUTMaxInput {
		return ExpDecayLUT[ExpLUTSize-1]
	}

	scaledIdx := count * (ExpLUTSize - 1)
	idx := scaledIdx / ExpLUTMaxInput
	frac := float64(scaledIdx%ExpLUTMaxInput) / float64(ExpLUTMaxInput)

	if idx >= ExpLUTSize-1 {
		return ExpDecayLUT[ExpLUTSize-1]
	}

	v0 := ExpDecayLUT[idx]
	v1 := ExpDecayLUT[idx+1]

	// Linear interpolation between the two closest values
	return v0 + (v1-v0)*frac
}

// ExpDecayScaled returns 1 + boostMax * e^(-count/k)
func ExpDecayScaled(count int, boostMax float64) float64 {
	return 1.0 + boostMax*ExpDecay(count)
}
