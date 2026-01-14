package component

// GlyphComponent represents a typeable spawned character entity
type GlyphComponent struct {
	Rune  rune
	Type  GlyphType
	Level GlyphLevel
}

// GlyphType represents the semantic type affecting game mechanics
type GlyphType int

const (
	GlyphGreen GlyphType = iota
	GlyphBlue
	GlyphRed
	GlyphWhite
	GlyphGold
)

// GlyphLevel represents brightness (0 = dark, 1 = normal, 2 = bright)
type GlyphLevel int

// Order matters
const (
	GlyphDark   GlyphLevel = iota // x1
	GlyphNormal                   // x2
	GlyphBright                   // x3
)