package render
// @lixen: #dev{feature[drain(render,system)],feature[quasar(render,system)]}

import (
	"math"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// RGB is an alias to terminal.RGB for colors, allowing render package to extend functionality
type RGB = terminal.RGB

// Predefined default color
var (
	RGBBlack = RGB{0, 0, 0}
)

// Lookup tables array access (no pointers) for speed
var (
	softLightG  [256]float64
	softLightDF [256]float64
)

// init pre-calculates the Perez SoftLight lookup tables to avoid expensive square root and division operations in the render loop
func init() {
	for i := 0; i < 256; i++ {
		df := float64(i) / 255.0
		softLightDF[i] = df

		// Pre-compute the Perez function 'G' term
		if df <= 0.25 {
			softLightG[i] = ((16.0*df-12.0)*df + 4.0) * df
		} else {
			softLightG[i] = math.Sqrt(df)
		}
	}
}

// clamp converts float to uint8 efficiently
func clamp(v float64) uint8 {
	if v >= 255.0 {
		return 255
	}
	if v <= 0.0 {
		return 0
	}
	return uint8(v)
}

// softLightChannel is designed to be inlined and uses the global LUTs directly
// Pre-computed LUT to avoid repeated sqrt/division in render loops
func softLightChannel(d, s uint8, intensity float64) uint8 {
	df := softLightDF[d]
	sf := softLightDF[s]

	var result float64
	if sf < 0.5 {
		// Pure math, 2 multiplications
		result = df - (1.0-2.0*sf)*df*(1.0-df)
	} else {
		// LUT lookup replaces math.Sqrt
		result = df + (2.0*sf-1.0)*(softLightG[d]-df)
	}

	// Mix with intensity
	// Lerp: df*(1-intensity) + result*intensity = df + (result-df)*intensity
	// This reduces dependency chains slightly
	result = df + (result-df)*intensity

	// Clamp to [0, 255]
	return clamp(result*255.0 + 0.5) // +0.5 for rounding
}

// SoftLight applies Perez soft light blend - gentler than linear alpha
func SoftLight(c, src RGB, intensity float64) RGB {
	return RGB{
		R: softLightChannel(c.R, src.R, intensity),
		G: softLightChannel(c.G, src.G, intensity),
		B: softLightChannel(c.B, src.B, intensity),
	}
}

// Blend optimizes alpha blending
// If alpha is 1.0 or 0.0, we return early to save math
func Blend(c, src RGB, alpha float64) RGB {
	if alpha >= 1.0 {
		return src
	}
	if alpha <= 0.0 {
		return c
	}

	// Pre-calculate invariant
	inv := 1.0 - alpha

	return RGB{
		R: uint8(float64(src.R)*alpha + float64(c.R)*inv),
		G: uint8(float64(src.G)*alpha + float64(c.G)*inv),
		B: uint8(float64(src.B)*alpha + float64(c.B)*inv),
	}
}

// Max returns per-channel maximum with alpha blending
func Max(c, src RGB, alpha float64) RGB {
	if alpha <= 0.0 {
		return c
	}

	maxed := RGB{
		R: max(c.R, src.R),
		G: max(c.G, src.G),
		B: max(c.B, src.B),
	}

	if alpha >= 1.0 {
		return maxed
	}

	return Blend(c, maxed, alpha)
}

// add is addition with clamping
func add(a, b uint8) uint8 {
	sum := int(a) + int(b)
	if sum > 255 {
		return 255
	}
	return uint8(sum)
}

// Add performs additive blend with clamping and alpha blending
func Add(c, src RGB, alpha float64) RGB {
	if alpha <= 0.0 {
		return c
	}

	added := RGB{
		R: add(c.R, src.R),
		G: add(c.G, src.G),
		B: add(c.B, src.B),
	}

	if alpha >= 1.0 {
		return added
	}

	return Blend(c, added, alpha)
}

// fastDiv255 approximates x / 255 using integer math
// Formula: (x + (x >> 8) + 1) >> 8
// This is significantly faster than the DIV instruction
func fastDiv255(x int) int {
	return (x + (x >> 8) + 1) >> 8
}

// Screen blend: 1 - (1-Dst)*(1-Src) with alpha blending
func Screen(c, src RGB, alpha float64) RGB {
	if alpha <= 0.0 {
		return c
	}

	screened := RGB{
		R: uint8(255 - fastDiv255((255-int(c.R))*(255-int(src.R)))),
		G: uint8(255 - fastDiv255((255-int(c.G))*(255-int(src.G)))),
		B: uint8(255 - fastDiv255((255-int(c.B))*(255-int(src.B)))),
	}

	if alpha >= 1.0 {
		return screened
	}

	return Blend(c, screened, alpha)
}

// overlayChannel combines Multiply and Screen blend modes
// It uses the destination color (d) to determine the mix:
// - If d < 0.5 (128): Acts like Multiply (darkens)
// - If d >= 0.5 (128): Acts like Screen (lightens)
// This preserves the highlights and shadows of the destination
func overlayChannel(d, s uint8) uint8 {
	if d < 128 {
		// Multiply mode: (2 * d * s) / 255
		val := 2 * int(d) * int(s)
		return uint8(fastDiv255(val))
	}
	// Screen mode: 1 - 2 * (1 - d) * (1 - s)
	// In 255 space: 255 - (2 * (255-d) * (255-s)) / 255
	val := 2 * (255 - int(d)) * (255 - int(s))
	return uint8(255 - fastDiv255(val))
}

// Overlay combines multiply (darks) and screen (lights) with alpha blending
func Overlay(c, src RGB, alpha float64) RGB {
	if alpha <= 0.0 {
		return c
	}

	overlaid := RGB{
		R: overlayChannel(c.R, src.R),
		G: overlayChannel(c.G, src.G),
		B: overlayChannel(c.B, src.B),
	}

	if alpha >= 1.0 {
		return overlaid
	}

	return Blend(c, overlaid, alpha)
}

// Scale multiplies all channels by factor (0.0-1.0)
func Scale(c RGB, factor float64) RGB {
	// Clamp to not wrap on factor > 1.0
	return RGB{
		R: clamp(float64(c.R) * factor),
		G: clamp(float64(c.G) * factor),
		B: clamp(float64(c.B) * factor),
	}
}

// Grayscale converts RGB to grayscale using Rec. 601 luma coefficients
// Formula: Y = R*0.299 + G*0.587 + B*0.114
// Integer math: (R*299 + G*587 + B*114) / 1000
func Grayscale(c RGB) RGB {
	gray := uint8((int(c.R)*299 + int(c.G)*587 + int(c.B)*114) / 1000)
	return RGB{R: gray, G: gray, B: gray}
}

// Lerp linearly interpolates between two colors
// t=0 returns a, t=1 returns b
func Lerp(a, b RGB, t float64) RGB {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	return RGB{
		R: uint8(float64(a.R) + t*float64(int(b.R)-int(a.R))),
		G: uint8(float64(a.G) + t*float64(int(b.G)-int(a.G))),
		B: uint8(float64(a.B) + t*float64(int(b.B)-int(a.B))),
	}
}