package visual

import (
	"github.com/lixenwraith/vi-fighter/terminal"
)

// Snake TrueColor palette
var (
	// Head colors
	RgbSnakeHeadBright = terminal.YellowGreen
	RgbSnakeHeadDark   = terminal.HunterGreen

	// Body colors (gradient from head toward tail)
	RgbSnakeBodyBright = terminal.GrassGreen
	RgbSnakeBodyMid    = terminal.FernGreen
	RgbSnakeBodyDark   = terminal.DarkFern

	// Shielded state overlay
	RgbSnakeShieldTint = terminal.SkyTeal
)

// Snake 256-color fallback
const (
	Snake256Head      uint8 = 82 // Bright green
	Snake256BodyFront uint8 = 76 // Medium green
	Snake256BodyBack  uint8 = 28 // Dark green
)

// Snake basic color (8/16 color terminals)
const (
	SnakeBasicHead uint8 = 2 // Green
	SnakeBasicBody uint8 = 2 // Green
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

// Segment visual spacing (for renderer continuity tuning)
const (
	// Visual overlap between segments (0 = touching, negative = gap, positive = overlap)
	SnakeSegmentVisualOverlap = 0.2
)

// SnakeHeadChars defines the 5×3 head character pattern
// Rows indexed by Y offset (0-2), columns by X offset (0-4)
// Directional arrow shape facing right (default)
var SnakeHeadChars = [3][5]rune{
	{'▖', '▀', '▀', '▀', '▗'},
	{'▌', '●', '▓', '●', '▐'},
	{'▘', '▄', '▄', '▄', '▝'},
}