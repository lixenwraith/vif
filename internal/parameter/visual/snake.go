package visual

import (
	"github.com/lixenwraith/color"
)

// Snake TrueColor palette
var (
	// Head colors
	RgbSnakeHeadBright = color.YellowGreen
	RgbSnakeHeadDark   = color.HunterGreen

	// Body colors (gradient from head toward tail)
	RgbSnakeBodyBright = color.GrassGreen

	// Shielded state overlay
	RgbSnakeShieldTint = color.SkyTeal
)

// Snake 256-color fallback; the head is drawn solid, so it can take a bright entry
const (
	Snake256Head = ConBrightGreen
	Snake256Body = ConGreen
)

// Gradient and visual parameters
const (
	// Lateral edge falloff (0.0 = same as center, 1.0 = fully faded)
	SnakeBodyEdgeFalloff = 0.35

	// Longitudinal gradient (how much darker tail is vs head-adjacent)
	SnakeBodyTailDarken = 0.5

	// Shield glow extension (cells beyond body)
	SnakeShieldGlowExtend = 1.5

	// Hit flash intensity
	SnakeHitFlashIntensity = 0.8
)

// SnakeHeadChars defines the 5×3 head character pattern
// Rows indexed by Y offset (0-2), columns by X offset (0-4)
// Directional arrow shape facing right (default)
var SnakeHeadChars = [3][5]rune{
	{'▖', '▀', '▀', '▀', '▗'},
	{'▌', '●', '▓', '●', '▐'},
	{'▘', '▄', '▄', '▄', '▝'},
}

// SnakeHeadChar256 is every head cell in 256 colors: solid in the foreground, which
// takes bright colors where backgrounds cannot
const SnakeHeadChar256 = '█'
