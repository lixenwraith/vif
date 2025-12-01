package render

// RGB stores explicit 8-bit color channels, decoupled from tcell
type RGB struct {
	R, G, B uint8
}

// Predefined colors
var (
	RGBBlack = RGB{0, 0, 0}
)

// Blend performs alpha blending: result = src*alpha + dst*(1-alpha)
func (dst RGB) Blend(src RGB, alpha float64) RGB {
	if alpha <= 0 {
		return dst
	}
	if alpha >= 1 {
		return src
	}
	inv := 1.0 - alpha
	return RGB{
		R: uint8(float64(src.R)*alpha + float64(dst.R)*inv),
		G: uint8(float64(src.G)*alpha + float64(dst.G)*inv),
		B: uint8(float64(src.B)*alpha + float64(dst.B)*inv),
	}
}

// Max returns per-channel maximum (non-destructive highlight)
func (dst RGB) Max(src RGB) RGB {
	return RGB{
		R: max(dst.R, src.R),
		G: max(dst.G, src.G),
		B: max(dst.B, src.B),
	}
}

// Add performs additive blend with clamping (light accumulation)
func (dst RGB) Add(src RGB) RGB {
	return RGB{
		R: uint8(min(int(dst.R)+int(src.R), 255)),
		G: uint8(min(int(dst.G)+int(src.G), 255)),
		B: uint8(min(int(dst.B)+int(src.B), 255)),
	}
}

// BlendMode defines compositing operations
type BlendMode uint8

const (
	BlendReplace BlendMode = iota // Dst = Src (opaque overwrite)
	BlendAlpha                    // Dst = Src*α + Dst*(1-α)
	BlendAdd                      // Dst = clamp(Dst + Src, 255)
	BlendMax                      // Dst = max(Dst, Src) per channel
)