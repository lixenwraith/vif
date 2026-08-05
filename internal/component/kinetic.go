package component

import "github.com/lixenwraith/vi-fighter/pkg/vmath/physics"

// KineticComponent provides a reusable kinematic container for entities requiring sub-cell motion
// Position is in cells, velocity in cells/sec, acceleration in cells/sec²
type KineticComponent struct {
	physics.Kinetic
}
