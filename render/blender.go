package render
// @lixen: #dev{feat[drain(render,system)]}

// BlendMode defines compositing operations using a bitmask (Flags | Op)
type BlendMode uint8

// Blend Operations (0-15)
const (
	opReplace   uint8 = 0x00
	opAlpha     uint8 = 0x01
	opAdd       uint8 = 0x02
	opMax       uint8 = 0x03
	opSoftLight uint8 = 0x04
	opScreen    uint8 = 0x05
	opOverlay   uint8 = 0x06
)

// Blend Flags
const (
	flagBg uint8 = 0x10 // Apply operation to Background
	flagFg uint8 = 0x20 // Apply operation to Foreground
)

// Pre-defined Blend Modes
const (
	// Standard Modes (affect both Fg and Bg)
	BlendReplace   = BlendMode(opReplace | flagBg | flagFg)
	BlendAlpha     = BlendMode(opAlpha | flagBg | flagFg)
	BlendAdd       = BlendMode(opAdd | flagBg | flagFg)
	BlendMax       = BlendMode(opMax | flagBg | flagFg)
	BlendSoftLight = BlendMode(opSoftLight | flagBg | flagFg)
	BlendScreen    = BlendMode(opScreen | flagBg | flagFg)
	BlendScreenFg  = BlendMode(opScreen | flagFg)
	BlendOverlay   = BlendMode(opOverlay | flagBg | flagFg)

	// Targeted Modes
	BlendFgOnly = BlendMode(opReplace | flagFg) // Replace Fg, Keep Bg
	BlendAddFg  = BlendMode(opAdd | flagFg)     // Add Fg, Keep Bg (Fixes flash bug)

	// Background-only modes
	BlendMaxBg = BlendMode(opMax | flagBg) // Max blend background only, preserve fg
)