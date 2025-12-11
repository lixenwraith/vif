// @focus: #render { types }
package components

// ColorClass represents semantic color categories for rendering
// Renderers resolve these to concrete RGB values
type ColorClass uint8

const (
	ColorNone ColorClass = iota
	ColorNormal
	ColorNugget
	ColorShield
	ColorDecay
	ColorDrain
	ColorCleaner
	ColorMaterialize
	ColorFlash
	// Sequence colors derived from SequenceType + SequenceLevel
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