package component

// GlyphComponent represents a typeable spawned character entity
// Used by GlyphSystem (formerly SpawnSystem) for player-interactable content
type GlyphComponent struct {
	Rune  rune
	Type  GlyphType
	Level GlyphLevel
	Style TextStyle
}

// GlyphType represents the semantic type affecting scoring/energy
type GlyphType int

const (
	GlyphGreen GlyphType = iota // Positive energy
	GlyphBlue                   // Double positive energy
	GlyphRed                    // Negative energy
)

// GlyphLevel represents brightness affecting multiplier
type GlyphLevel int

const (
	GlyphDark   GlyphLevel = iota // x1
	GlyphNormal                   // x2
	GlyphBright                   // x3
)