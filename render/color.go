package render

import (
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// HeatGradientLUT holds the pre-calculated rainbow gradient
// 768 bytes, fits in L1 cache alongside other hot data
var HeatGradientLUT [256]terminal.RGB

func init() {
	// Pre-calculate heat gradient
	for i := 0; i < 256; i++ {
		progress := float64(i) / 255.0
		HeatGradientLUT[i] = calculateHeatColor(progress)
	}
}

// LerpRGBFixed interpolates between colors using Q32.32 factor t
func LerpRGBFixed(a, b terminal.RGB, t int64) terminal.RGB {
	// (diff * t) >> Shift is equivalent to Mul(diff * Scale, t) since diff is integer
	// We do the multiplication in 64-bit to prevent overflow before shift
	r := int64(a.R) + ((int64(b.R)-int64(a.R))*t)>>vmath.Shift
	g := int64(a.G) + ((int64(b.G)-int64(a.G))*t)>>vmath.Shift
	bl := int64(a.B) + ((int64(b.B)-int64(a.B))*t)>>vmath.Shift
	return terminal.RGB{R: uint8(r), G: uint8(g), B: uint8(bl)}
}

// RainbowIndexColor returns a color from HeatGradientLUT mapped to index/total
// Uses bounded range to avoid dark extremes for text readability
// Returns fallback color when total <= 1
func RainbowIndexColor(index, total int64, fallback terminal.RGB) terminal.RGB {
	if total <= 1 {
		return fallback
	}
	lutIdx := visual.RainbowLUTMin + int((index*visual.RainbowLUTRange)/(total-1))
	if lutIdx > visual.RainbowLUTMax {
		lutIdx = visual.RainbowLUTMax
	}
	return HeatGradientLUT[lutIdx]
}

// calculateHeatColor returns the color for a given position in the heat meter gradient
// Progress is 0.0 to 1.0, representing position from start to end
// Only used for LUT generation
func calculateHeatColor(progress float64) terminal.RGB {
	if progress < 0.0 {
		progress = 0.0
	}
	if progress > 1.0 {
		progress = 1.0
	}

	// Rainbow gradient: deep red → orange → yellow → green → cyan → blue → purple/pink
	switch {
	case progress < visual.GradientSeg1: // Red to Orange
		t := progress / visual.GradientSeg1
		return terminal.RGB{
			R: uint8(float64(visual.GradientDeepRed.R) + (float64(visual.GradientOrange.R)-float64(visual.GradientDeepRed.R))*t),
			G: uint8(float64(visual.GradientDeepRed.G) + (float64(visual.GradientOrange.G)-float64(visual.GradientDeepRed.G))*t),
			B: uint8(float64(visual.GradientDeepRed.B) + (float64(visual.GradientOrange.B)-float64(visual.GradientDeepRed.B))*t),
		}
	case progress < visual.GradientSeg2: // Orange to Yellow
		t := (progress - visual.GradientSeg1) / (visual.GradientSeg2 - visual.GradientSeg1)
		return terminal.RGB{
			R: uint8(float64(visual.GradientOrange.R) + (float64(visual.GradientYellow.R)-float64(visual.GradientOrange.R))*t),
			G: uint8(float64(visual.GradientOrange.G) + (float64(visual.GradientYellow.G)-float64(visual.GradientOrange.G))*t),
			B: uint8(float64(visual.GradientOrange.B) + (float64(visual.GradientYellow.B)-float64(visual.GradientOrange.B))*t),
		}
	case progress < visual.GradientSeg3: // Yellow to Green
		t := (progress - visual.GradientSeg2) / (visual.GradientSeg3 - visual.GradientSeg2)
		return terminal.RGB{
			R: uint8(float64(visual.GradientYellow.R) + (float64(visual.GradientGreen.R)-float64(visual.GradientYellow.R))*t),
			G: uint8(float64(visual.GradientYellow.G) + (float64(visual.GradientGreen.G)-float64(visual.GradientYellow.G))*t),
			B: uint8(float64(visual.GradientYellow.B) + (float64(visual.GradientGreen.B)-float64(visual.GradientYellow.B))*t),
		}
	case progress < visual.GradientSeg4: // Green to Cyan
		t := (progress - visual.GradientSeg3) / (visual.GradientSeg4 - visual.GradientSeg3)
		return terminal.RGB{
			R: uint8(float64(visual.GradientGreen.R) + (float64(visual.GradientCyan.R)-float64(visual.GradientGreen.R))*t),
			G: uint8(float64(visual.GradientGreen.G) + (float64(visual.GradientCyan.G)-float64(visual.GradientGreen.G))*t),
			B: uint8(float64(visual.GradientGreen.B) + (float64(visual.GradientCyan.B)-float64(visual.GradientGreen.B))*t),
		}
	case progress < visual.GradientSeg5: // Cyan to Blue
		t := (progress - visual.GradientSeg4) / (visual.GradientSeg5 - visual.GradientSeg4)
		return terminal.RGB{
			R: uint8(float64(visual.GradientCyan.R) + (float64(visual.GradientBlue.R)-float64(visual.GradientCyan.R))*t),
			G: uint8(float64(visual.GradientCyan.G) + (float64(visual.GradientBlue.G)-float64(visual.GradientCyan.G))*t),
			B: uint8(float64(visual.GradientCyan.B) + (float64(visual.GradientBlue.B)-float64(visual.GradientCyan.B))*t),
		}
	default: // Blue to Purple/Pink
		t := (progress - visual.GradientSeg5) / (1.0 - visual.GradientSeg5)
		return terminal.RGB{
			R: uint8(float64(visual.GradientBlue.R) + (float64(visual.GradientPurple.R)-float64(visual.GradientBlue.R))*t),
			G: uint8(float64(visual.GradientBlue.G) + (float64(visual.GradientPurple.G)-float64(visual.GradientBlue.G))*t),
			B: uint8(float64(visual.GradientBlue.B) + (float64(visual.GradientPurple.B)-float64(visual.GradientBlue.B))*t),
		}
	}
}