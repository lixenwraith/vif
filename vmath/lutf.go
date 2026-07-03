package vmath

import "math"

var (
	// Float64 Look-Up Tables to prevent int64<->float64 conversion at runtime
	SinF_LUT      [LUTSize]float64
	CosF_LUT      [LUTSize]float64
	Atan2F_LUT    [LUTSize]float64
	ExpDecayF_LUT [ExpLUTSize]float64
)

// radToIndex is a precalculated multiplier to convert radians to a LUT index
const radToIndex = float64(LUTSize) / (2.0 * math.Pi)

func init() {
	// Initialize Float64 Trigonometric LUTs
	for i := range LUTSize {
		rad := 2.0 * math.Pi * float64(i) / float64(LUTSize)
		SinF_LUT[i] = math.Sin(rad)
		CosF_LUT[i] = math.Cos(rad)

		// Atan2 LUT maps ratio [0,1] to angle [0, π/4]
		ratio := float64(i) / float64(LUTMask)
		Atan2F_LUT[i] = math.Atan(ratio)
	}

	// Initialize Float64 Exponential Decay LUT
	for i := range ExpLUTSize {
		x := float64(i) * float64(ExpLUTMaxInput) / float64(ExpLUTSize-1)
		ExpDecayF_LUT[i] = math.Exp(-x / ExpLUTDecayK)
	}
}

// SinF returns the sine of angleRad using O(1) LUT lookup.
func SinF(angleRad float64) float64 {
	// Fast conversion to index.
	// Go's bitwise AND handles negative numbers correctly via two's complement.
	idx := int(angleRad*radToIndex) & LUTMask
	return SinF_LUT[idx]
}

// CosF returns the cosine of angleRad using O(1) LUT lookup.
func CosF(angleRad float64) float64 {
	idx := int(angleRad*radToIndex) & LUTMask
	return CosF_LUT[idx]
}

// Atan2F returns angle in [0, 2π) for (dy, dx) using LUT.
// Mimics the exact octant-based logic of the fixed-point Atan2 for speed.
func Atan2F(dy, dx float64) float64 {
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
			baseAngle = Atan2F_LUT[idx]
		}
	} else {
		idx := int((adx / ady) * float64(LUTMask))
		baseAngle = (math.Pi / 2.0) - Atan2F_LUT[idx]
	}

	// Map back to correct quadrant
	if dx > 0 {
		if dy >= 0 {
			return baseAngle // Q1
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

// ExpDecayF returns e^(-count/k) using LUT linear interpolation.
// Matches the fixed-point behavior but returns continuous float64 [0.0, 1.0].
func ExpDecayF(count int) float64 {
	if count <= 0 {
		return 1.0
	}
	if count >= ExpLUTMaxInput {
		return ExpDecayF_LUT[ExpLUTSize-1]
	}

	scaledIdx := count * (ExpLUTSize - 1)
	idx := scaledIdx / ExpLUTMaxInput
	frac := float64(scaledIdx%ExpLUTMaxInput) / float64(ExpLUTMaxInput)

	if idx >= ExpLUTSize-1 {
		return ExpDecayF_LUT[ExpLUTSize-1]
	}

	v0 := ExpDecayF_LUT[idx]
	v1 := ExpDecayF_LUT[idx+1]

	// Linear interpolation between the two closest values
	return v0 + (v1-v0)*frac
}
