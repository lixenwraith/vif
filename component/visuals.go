package component

// ColorClass represents semantic color categories for rendering
// Renderers resolve these to concrete RGB values
type ColorClass uint8

// Sematic colors
const (
	ColorNone ColorClass = iota
	ColorNormal
	ColorNugget
	ColorShield
	ColorBlossom
	ColorDecay
	ColorDrain
	ColorCleaner
	ColorMaterialize
	ColorFlash
	// Sequence colors derived from CharacterType + CharacterLevel
)

// TextStyle represents semantic text styling
// Renderers resolve these to terminal.Attr
type TextStyle uint8

const (
	StyleNormal TextStyle = iota
	StyleBold
	StyleDim
	StyleUnderline
	StyleBlink
)