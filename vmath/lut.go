package vmath

import (
	"math"
)

func init() {
	// Sin/Cos LUT calculation
	for i := 0; i < LUTSize; i++ {
		rad := 2.0 * math.Pi * float64(i) / float64(LUTSize)
		SinLUT[i] = int32(math.Sin(rad) * Scale)
		CosLUT[i] = int32(math.Cos(rad) * Scale)
	}

	// Exp LUT calculation
	for i := 0; i < ExpLUTSize; i++ {
		x := float64(i) * ExpLUTMaxInput / float64(ExpLUTSize-1)
		ExpDecayLUT[i] = int32(math.Exp(-x/ExpLUTDecayK) * Scale)
	}
}

// SinLUT and CosLUT scaled by Q16.16
var (
	SinLUT [LUTSize]int32
	CosLUT [LUTSize]int32
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

// ExpDecayLUT contains pre-computed e^(-x/k) values scaled to Q16.16
// Index maps linearly to input range [0, ExpLUTMaxInput]
var ExpDecayLUT [ExpLUTSize]int32

// ExpDecay returns e^(-count/k) in Q16.16 using LUT interpolation
// Result ranges from Scale (at count=0) to ~0 (at high count)
// O(1) with linear interpolation for smoothness
func ExpDecay(count int) int32 {
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
	return v0 + int32((int64(v1-v0)*int64(frac))/int64(ExpLUTMaxInput))
}

// ExpDecayScaled returns Scale + boostMax * e^(-count/k) in Q16.16
// Useful for speed/attraction multipliers that increase as count decreases
// boostMax: Q16.16 maximum additional multiplier at count=0
func ExpDecayScaled(count int, boostMax int32) int32 {
	return Scale + Mul(boostMax, ExpDecay(count))
}