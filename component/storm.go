package component

import (
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/vmath"
)

const StormCircleCount = 3

// StormComponent marks the root storm controller entity
// Attached to phantom header that owns the 3 circle sub-headers
type StormComponent struct {
	Circles      [StormCircleCount]core.Entity
	CirclesAlive [StormCircleCount]bool
}

// AliveCount returns number of living circles
func (c *StormComponent) AliveCount() int {
	count := 0
	for _, alive := range c.CirclesAlive {
		if alive {
			count++
		}
	}
	return count
}

// StormCircleComponent holds per-circle 3D physics state
// Attached to each circle header entity
type StormCircleComponent struct {
	Pos3D vmath.Vec3
	Vel3D vmath.Vec3
	Index int // 0, 1, or 2 - position in parent storm
}