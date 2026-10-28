package component

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter"
)

// SplashSlot identifies the visual purpose of a splash for uniqueness enforcement
type SplashSlot uint8

const (
	SlotTimer     SplashSlot = iota // Gold timer, entity-anchored
	SlotMagnifier                   // Typing preview, cursor-anchored
)

// SplashComponent holds state for splash effects (typing feedback, timers)
// Supports multiple concurrent entities
type SplashComponent struct {
	Content [parameter.SplashMaxLength]rune // Content buffer
	Length  int                             // Active character count
	Color   color.RGB                       // Render color

	// Positioning: Offset from AnchorEntity's position, or without one from AnchorX/Y
	AnchorEntity                                     core.Entity
	AnchorX                                          int // Game-relative X
	AnchorY                                          int // Game-relative Y
	MarginLeft, MarginRight, MarginTop, MarginBottom int // Forced splash offset from anchor
	OffsetX                                          int // Calculated offset from the anchor
	OffsetY                                          int

	// Visual purpose and uniqueness enforcement; a timer splash counts down on its
	// lifetime timer, a magnifier persists while the cursor rests on a glyph
	Slot SplashSlot
}
