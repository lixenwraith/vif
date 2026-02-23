package navigation

// FlowFieldCache manages flow field recomputation with throttling
type FlowFieldCache struct {
	Field *FlowField

	// Recomputation throttling
	LastTargetX, LastTargetY int // Last target we computed for
	TicksSinceCompute        int // Ticks since last computation
	MinTicksBetweenCompute   int // Minimum ticks between recomputes
	DirtyDistance            int // Target must move this many cells to trigger immediate recompute

	// PendingUpdate latches true on any state change, cleared after compute
	PendingUpdate bool
}

// NewFlowFieldCache creates a cache with default throttling
func NewFlowFieldCache(width, height, minTicks, dirtyDist int) *FlowFieldCache {
	return &FlowFieldCache{
		Field:                  NewFlowField(width, height),
		LastTargetX:            -1,
		LastTargetY:            -1,
		TicksSinceCompute:      minTicks, // Allow immediate first compute
		MinTicksBetweenCompute: minTicks,
		DirtyDistance:          dirtyDist,
		PendingUpdate:          true, // Force initial compute
	}
}

// Resize adjusts dimensions
func (c *FlowFieldCache) Resize(width, height int) {
	c.Field.Resize(width, height)
	c.LastTargetX = -1
	c.LastTargetY = -1
	c.PendingUpdate = true
}

// Update checks if recomputation needed and performs it with ROI bounds
// entityPositions: slice of [x, y] pairs for entities requiring flow field navigation
// Returns true if field was recomputed this tick
func (c *FlowFieldCache) Update(targetX, targetY int, isBlocked WallChecker) bool {
	c.TicksSinceCompute++

	// Detect target movement
	if targetX != c.LastTargetX || targetY != c.LastTargetY {
		dx := targetX - c.LastTargetX
		dy := targetY - c.LastTargetY
		if dx < 0 {
			dx = -dx
		}
		if dy < 0 {
			dy = -dy
		}

		c.PendingUpdate = true
		if dx+dy >= c.DirtyDistance {
			c.TicksSinceCompute = c.MinTicksBetweenCompute
		}
	}

	if (c.PendingUpdate && c.TicksSinceCompute >= c.MinTicksBetweenCompute) || !c.Field.Valid {
		c.Field.Compute(targetX, targetY, isBlocked)
		c.LastTargetX = targetX
		c.LastTargetY = targetY
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