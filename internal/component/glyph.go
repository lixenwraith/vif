package component

// GlyphComponent represents a typeable spawned character entity
type GlyphComponent struct {
	Rune  rune
	Type  GlyphType
	Level GlyphLevel
}

// GlyphType represents the semantic type affecting game mechanics
type GlyphType int

// NOTE: Changing values breaks GlyphColorLUT in parameter/visual/color.go
const (
	GlyphGreen GlyphType = 0
	GlyphBlue  GlyphType = 1
	GlyphRed   GlyphType = 2
	GlyphWhite GlyphType = 3
	GlyphGold  GlyphType = 4
)

// GlyphLevel represents brightness
type GlyphLevel int

// NOTE: Changing values breaks GlyphColorLUT in parameter/visual/color.go
const (
	GlyphDark   GlyphLevel = 0
	GlyphNormal GlyphLevel = 1
	GlyphBright GlyphLevel = 2
)