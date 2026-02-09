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
}

// SinLUT and CosLUT scaled by Q32.32
var (
	SinLUT [LUTSize]int64
	CosLUT [LUTSize]int64
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