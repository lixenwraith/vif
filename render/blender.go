package render

// BlendMode defines compositing operations
type BlendMode uint8

const (
	BlendReplace BlendMode = iota // Dst = Src (opaque overwrite)
	BlendAlpha                    // Dst = Src*α + Dst*(1-α)
	BlendAdd                      // Dst = clamp(Dst + Src, 255)
	BlendMax                      // Dst = max(Dst, Src) per channel
	BlendFgOnly                   // Update Rune/Fg/Attrs only, preserve Dst.Bg exactly
)