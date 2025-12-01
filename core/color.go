package core

// RGB stores explicit 8-bit color channels, decoupled from tcell
type RGB struct {
	R, G, B uint8
}

// Predefined colors
var (
	RGBBlack = RGB{0, 0, 0}
)

// Blend performs alpha blending: result = src*alpha + dst*(1-alpha)
func (c RGB) Blend(src RGB, alpha float64) RGB {
	if alpha <= 0 {
		return c
	}
	if alpha >= 1 {
		return src
	}
	inv := 1.0 - alpha
	return RGB{
		R: uint8(float64(src.R)*alpha + float64(c.R)*inv),
		G: uint8(float64(src.G)*alpha + float64(c.G)*inv),
		B: uint8(float64(src.B)*alpha + float64(c.B)*inv),
	}
}

// Max returns per-channel maximum (non-destructive highlight)
func (c RGB) Max(src RGB) RGB {
	return RGB{
		R: max(c.R, src.R),
		G: max(c.G, src.G),
		B: max(c.B, src.B),
	}
}

// Add performs additive blend with clamping (light accumulation)
func (c RGB) Add(src RGB) RGB {
	return RGB{
		R: uint8(min(int(c.R)+int(src.R), 255)),
		G: uint8(min(int(c.G)+int(src.G), 255)),
		B: uint8(min(int(c.B)+int(src.B), 255)),
	}
}

// Scale multiplies each channel by factor (for fading effects)
func (c RGB) Scale(factor float64) RGB {
	if factor <= 0 {
		return RGBBlack
	}
	if factor >= 1 {
		return c
	}
	return RGB{
		R: uint8(float64(c.R) * factor),
		G: uint8(float64(c.G) * factor),
		B: uint8(float64(c.B) * factor),
	}
}