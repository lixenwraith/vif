package navigation

import "github.com/lixenwraith/vi-fighter/pkg/vmath"

// FlowFieldCache manages flow field recomputation with throttling
type FlowFieldCache struct {
	Field *FlowField

	// Recomputation throttling
	LastTargets            []vmath.Point // Tracks previously requested target coords
	TicksSinceCompute      int           // Ticks since last computation
	MinTicksBetweenCompute int           // Minimum ticks between recomputes
	DirtyDistance          int           // Target must move this many cells to trigger immediate recompute

	// PendingUpdate latches true on any state change, cleared after compute
	PendingUpdate bool
}

// NewFlowFieldCache creates a cache with default throttling
func NewFlowFieldCache(width, height, minTicks, dirtyDist int) *FlowFieldCache {
	return &FlowFieldCache{
		Field:                  NewFlowField(width, height),
		LastTargets:            make([]vmath.Point, 0, 8),
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

// Update recomputes the field when dirty state and throttling allow
// targets: goal points the field converges toward
// Returns true if field was recomputed this tick
func (c *FlowFieldCache) Update(targets []vmath.Point, isBlocked WallChecker) bool {
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

// Rebuild recomputes the field for the targets the cache last computed for and
// leaves the throttle phase exactly as it found it. It reports whether it ran.
//
// It exists for one caller: a cache whose throttle phase was restored from another
// instance but whose field was not, because a field is derived rather than carried.
// Update cannot serve that caller. It would derive from *this* tick's targets, which
// are not the ones the restored phase belongs to, so the field would be one the
// sender never held; and it would then reset TicksSinceCompute and clear
// PendingUpdate, which is the phase itself. Deriving from LastTargets reproduces the
// sender's field, and leaving the counters alone leaves the next recompute due on
// the tick the sender's is.
func (c *FlowFieldCache) Rebuild(isBlocked WallChecker) {
	ticks, pending := c.TicksSinceCompute, c.PendingUpdate
	c.Field.Compute(c.LastTargets, isBlocked)
	c.TicksSinceCompute, c.PendingUpdate = ticks, pending
}

// Computed reports whether a field has been derived at least once. It is part of
// the throttle phase for the same reason the counters are: an install that left the
// field underived would force a compute on the next tick that the instance it
// copied is not making.
func (c *FlowFieldCache) Computed() bool { return c.Field != nil && c.Field.Valid }

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
