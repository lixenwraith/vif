package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/terminal"
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
	Color   terminal.RGB                    // Render color

	// Positioning: AnchorEntity != 0 uses entity-relative, else absolute AnchorX/Y
	AnchorEntity                                     core.Entity
	AnchorX                                          int // Game-relative X
	AnchorY                                          int // Game-relative Y
	MarginLeft, MarginRight, MarginTop, MarginBottom int // Forced splash offset from anchor
	OffsetX                                          int // Calculated offset from entity position (ignored if AnchorEntity == 0)
	OffsetY                                          int

	// Lifecycle & Animation
	Slot      SplashSlot    // Visual purpose and uniqueness enforcement
	Remaining time.Duration // Time remaining until expiration (Delta-based)
	Duration  time.Duration // Total initial duration (for progress/animations)
}