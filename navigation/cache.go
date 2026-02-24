package navigation

import (
	"github.com/lixenwraith/vi-fighter/core"
)

// FlowFieldCache manages flow field recomputation with throttling
type FlowFieldCache struct {
	Field *FlowField

	// Recomputation throttling
	LastTargets            []core.Point // Tracks previously requested target coords
	TicksSinceCompute      int          // Ticks since last computation
	MinTicksBetweenCompute int          // Minimum ticks between recomputes
	DirtyDistance          int          // Target must move this many cells to trigger immediate recompute

	// PendingUpdate latches true on any state change, cleared after compute
	PendingUpdate bool
}

// NewFlowFieldCache creates a cache with default throttling
func NewFlowFieldCache(width, height, minTicks, dirtyDist int) *FlowFieldCache {
	return &FlowFieldCache{
		Field:                  NewFlowField(width, height),
		LastTargets:            make([]core.Point, 0, 8),
		TicksSinceCompute:      minTicks, // Allow immediate first compute
		MinTicksBetweenCompute: minTicks,
		DirtyDistance:          dirtyDist,
		PendingUpdate:          true, // Force initial compute
	}
}

// Resize adjusts dimensions
func (c *FlowFieldCache) Resize(width, height int) {
	c.Field.Resize(width, height)
	c.LastTargets = c.LastTargets[:0]
	c.PendingUpdate = true
}

// Update checks if recomputation needed and performs it
// targets: slice of points for entities requiring flow field navigation
// Returns true if field was recomputed this tick
func (c *FlowFieldCache) Update(targets []core.Point, isBlocked WallChecker) bool {
	c.TicksSinceCompute++

	if len(targets) != len(c.LastTargets) {
		c.PendingUpdate = true
		c.TicksSinceCompute = c.MinTicksBetweenCompute
	} else {
		for i, t := range targets {
			dx := t.X - c.LastTargets[i].X
			dy := t.Y - c.LastTargets[i].Y
			if dx < 0 {
				dx = -dx
			}
			if dy < 0 {
				dy = -dy
			}
			if dx+dy >= c.DirtyDistance {
				c.PendingUpdate = true
				c.TicksSinceCompute = c.MinTicksBetweenCompute
				break
			}
		}
	}

	if (c.PendingUpdate && c.TicksSinceCompute >= c.MinTicksBetweenCompute) || !c.Field.Valid {
		c.Field.Compute(targets, isBlocked)
		c.LastTargets = c.LastTargets[:0]
		c.LastTargets = append(c.LastTargets, targets...)
		c.TicksSinceCompute = 0
		c.PendingUpdate = false
		return true
	}

	return false
}

// MarkDirty forces recomputation on next eligible tick
func (c *FlowFieldCache) MarkDirty() {
	c.PendingUpdate = true
}

// GetDirection returns cached flow direction
func (c *FlowFieldCache) GetDirection(x, y int) int8 {
	return c.Field.GetDirection(x, y)
}

// GetDistance returns cached BFS distance
func (c *FlowFieldCache) GetDistance(x, y int) int {
	return c.Field.GetDistance(x, y)
}

// IsValid returns true if field has valid data
func (c *FlowFieldCache) IsValid() bool {
	return c.Field.Valid
}