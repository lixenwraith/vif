package component
// @lixen: #dev{feature[drain(render,system)],feature[quasar(render,system)]}

// GlyphComponent represents a typeable spawned character entity
type GlyphComponent struct {
	Rune  rune
	Type  GlyphType
	Level GlyphLevel
}

// GlyphType represents the semantic type affecting heat/energy
type GlyphType int

const (
	GlyphGreen GlyphType = iota // Positive energy
	GlyphBlue                   // Double positive energy
	GlyphRed                    // Negative energy
	GlyphGold
)

// GlyphLevel represents brightness affecting multiplier
type GlyphLevel int

const (
	GlyphDark   GlyphLevel = iota // x1
	GlyphNormal                   // x2
	GlyphBright                   // x3
)