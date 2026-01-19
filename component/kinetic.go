package component

import (
	"github.com/lixenwraith/vi-fighter/core"
)

// KineticComponent provides a reusable kinematic container for entities requiring sub-pixel motion
// Uses Q32.32 fixed-point arithmetic for deterministic integration and high-performance physics updates
type KineticComponent struct {
	core.Kinetic
}