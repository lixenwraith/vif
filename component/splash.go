package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
)

// SplashSlot identifies the visual purpose of a splash for uniqueness enforcement
type SplashSlot uint8

const (
	SlotTimer     SplashSlot = iota // Gold timer, entity-anchored
	SlotAction                      // Normal mode feedback, far corner
	SlotMagnifier                   // Typing preview, cursor-anchored
)

// SplashColor defines the semantic color for splash effects (decoupling from renderer for cyclic dependency)
type SplashColor uint8

const (
	SplashColorNone SplashColor = iota
	SplashColorNormal
	SplashColorInsert
	SplashColorGreen
	SplashColorBlue
	SplashColorRed
	SplashColorGold
	SplashColorCyan
	SplashColorNugget
	SplashColorWhite
	SplashColorBlossom
	SplashColorDecay
)

// SplashComponent holds state for splash effects (typing feedback, timers)
// Supports multiple concurrent entities
type SplashComponent struct {
	Content [constant.SplashMaxLength]rune // Content buffer
	Length  int                            // Active character count
	Color   SplashColor                    // Render color

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