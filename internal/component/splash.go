package component

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter"
)

// SplashSlot identifies the visual purpose of a splash for uniqueness enforcement
type SplashSlot uint8

const (
	SlotTimer     SplashSlot = iota // Countdown, anchored to an entity or an origin cell
	SlotMagnifier                   // Typing preview, cursor-anchored
	SlotText                        // Text a scenario writes at an origin cell
)

// SplashFont names a splash face: the large one, or the small one at half its cells
type SplashFont uint8

const (
	SplashLarge SplashFont = iota
	SplashSmall
)

// SplashFontNames are what a document calls each SplashFont
var SplashFontNames = [...]string{SplashLarge: "large", SplashSmall: "small"}

// SplashComponent holds state for splash effects (countdowns, text, the magnifier)
// Supports multiple concurrent entities
type SplashComponent struct {
	Content [parameter.SplashMaxLength]rune // Content buffer
	Length  int                             // Active character count
	Color   color.RGB                       // Render color
	Font    SplashFont

	// Positioning: Offset from AnchorEntity's position, or without one from AnchorX/Y
	AnchorEntity                                     core.Entity
	AnchorX                                          int // Game-relative X
	AnchorY                                          int // Game-relative Y
	MarginLeft, MarginRight, MarginTop, MarginBottom int // Forced splash offset from anchor
	OffsetX                                          int // Calculated offset from the anchor
	OffsetY                                          int

	// Visual purpose and uniqueness enforcement; a timer splash counts down on its
	// lifetime timer, a magnifier persists while the cursor rests on a glyph, a text
	// stands at its origin until replaced or cleared, or until its timer expires
	Slot SplashSlot
}
