package vmath

import (
	"math"
)

func init() {
	// Sin/Cos LUT calculation
	for i := 0; i < LUTSize; i++ {
		rad := 2.0 * math.Pi * float64(i) / LUTSize
		SinLUT[i] = int64(math.Sin(rad) * ScaleF)
		CosLUT[i] = int64(math.Cos(rad) * ScaleF)
	}

	// Exp LUT calculation
	for i := 0; i < ExpLUTSize; i++ {
		x := float64(i) * ExpLUTMaxInput / float64(ExpLUTSize-1)
		ExpDecayLUT[i] = int64(math.Exp(-x/ExpLUTDecayK) * ScaleF)
	}

	// Atan2 LUT: ratio [0,1] -> angle [0, π/4] in Q32.32
	for i := 0; i < LUTSize; i++ {
		ratio := float64(i) / float64(LUTMask)
		angle := math.Atan(ratio)
		atan2LUT[i] = int64(angle / (2 * math.Pi) * ScaleF)
	}
}

// SinLUT and CosLUT scaled by Q32.32
var (
	SinLUT [LUTSize]int64
	CosLUT [LUTSize]int64

	// atan2LUT maps ratio [0,1] to angle [0, Scale/8] (one octant)
	// Index = ratio * LUTMask, value = atan(ratio) scaled to Q32.32
	atan2LUT [LUTSize]int64
)

// Exponential decay LUT for performance-critical scaling
// Usage: speed multiplier based on entity count, damage falloff, etc.

// ExpLUTSize defines resolution of exponential decay lookup table
const ExpLUTSize = 256

// ExpLUTMaxInput is the maximum input value mapped to LUT
// Beyond this, output saturates at minimum value
const ExpLUTMaxInput = 512

// ExpLUTDecayK controls curve steepness (saturation point)
// Higher K = slower decay, lower K = faster decay
const ExpLUTDecayK = 30.0

// ExpDecayLUT contains pre-computed e^(-x/k) values scaled to Q32.32
// Index maps linearly to input range [0, ExpLUTMaxInput]
var ExpDecayLUT [ExpLUTSize]int64

// ExpDecay returns e^(-count/k) in Q32.32 using LUT interpolation
// Result ranges from Scale (at count=0) to ~0 (at high count)
// O(1) with linear interpolation for smoothness
func ExpDecay(count int) int64 {
	if count <= 0 {
		return Scale
	}
	if count >= ExpLUTMaxInput {
		return ExpDecayLUT[ExpLUTSize-1]
	}

	scaledIdx := count * (ExpLUTSize - 1)
	idx := scaledIdx / ExpLUTMaxInput
	frac := scaledIdx % ExpLUTMaxInput

	if idx >= ExpLUTSize-1 {
		return ExpDecayLUT[ExpLUTSize-1]
	}

	v0 := ExpDecayLUT[idx]
	v1 := ExpDecayLUT[idx+1]
	return v0 + ((v1-v0)*int64(frac))/ExpLUTMaxInput
}

// ExpDecayScaled returns Scale + boostMax * e^(-count/k) in Q32.32
// Useful for speed/attraction multipliers that increase as count decreases
// boostMax: Q32.32 maximum additional multiplier at count=0
func ExpDecayScaled(count int, boostMax int64) int64 {
	return Scale + Mul(boostMax, ExpDecay(count))
}

// Atan2 returns angle in [0, Scale) for (dy, dx) using LUT
// Result is Q32.32 where Scale = full rotation (2π)
// Zero vector returns 0
func Atan2(dy, dx int64) int64 {
	if dx == 0 && dy == 0 {
		return 0
	}

	adx, ady := dx, dy
	if adx < 0 {
		adx = -adx
	}
	if ady < 0 {
		ady = -ady
	}

	// Compute ratio and base angle from LUT
	var baseAngle int64
	if adx >= ady {
		// Octants 0,3,4,7: ratio = |dy/dx| in [0,1]
		if adx == 0 {
			baseAngle = 0
		} else {
			// Index = (ady * (LUTSize-1)) / adx
			idx := (ady * LUTMask) / adx
			if idx > LUTMask {
				idx = LUTMask
			}
			baseAngle = atan2LUT[idx]
		}
	} else {
		// Octants 1,2,5,6: ratio = |dx/dy| in [0,1], angle = π/4 - atan(ratio)
		idx := (adx * LUTMask) / ady
		if idx > LUTMask {
			idx = LUTMask
		}
		baseAngle = Scale/4 - atan2LUT[idx] // π/2 - atan(ratio)
	}

	// Map to correct quadrant
	// Q1: dx>0, dy>=0 -> angle = baseAngle
	// Q2: dx<=0, dy>0 -> angle = π/2 - baseAngle (but baseAngle already accounts for octant)
	// Q3: dx<0, dy<=0 -> angle = π + baseAngle
	// Q4: dx>=0, dy<0 -> angle = 2π - baseAngle

	if dx > 0 {
		if dy >= 0 {
			return baseAngle // Q1
		}
		return Scale - baseAngle // Q4
	} else if dx < 0 {
		if dy >= 0 {
			return Scale/2 - baseAngle // Q2
		}
		return Scale/2 + baseAngle // Q3
	}
	if dy > 0 {
		return Scale / 4 // +Y axis = π/2
	}
	return 3 * Scale / 4 // -Y axis = 3π/2
}