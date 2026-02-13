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

	// ROI state
	LastROI   *ROIBounds
	ROIMargin int // Expansion margin around computed AABB
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
		ROIMargin:              10,   // Default margin for path curvature
	}
}

// Resize adjusts dimensions
func (c *FlowFieldCache) Resize(width, height int) {
	c.Field.Resize(width, height)
	c.LastTargetX = -1
	c.LastTargetY = -1
	c.PendingUpdate = true
	c.LastROI = nil
}

// Update checks if recomputation needed and performs it with ROI bounds
// entityPositions: slice of [x, y] pairs for entities requiring flow field navigation
// Returns true if field was recomputed this tick
func (c *FlowFieldCache) Update(targetX, targetY int, isBlocked WallChecker, entityPositions [][2]int) bool {
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
		targetMoved := dx + dy

		// Latch pending update on any movement
		c.PendingUpdate = true

		// Immediate recompute if moved far
		if targetMoved >= c.DirtyDistance {
			c.TicksSinceCompute = c.MinTicksBetweenCompute
		}
	}

	needsCompute := false

	// Compute if pending AND cooldown expired
	if c.PendingUpdate && c.TicksSinceCompute >= c.MinTicksBetweenCompute {
		needsCompute = true
	}

	// First computation / invalidated field
	if !c.Field.Valid {
		c.PendingUpdate = true
		needsCompute = true
	}

	if needsCompute {
		// Compute ROI from entity positions
		roi := c.computeROI(targetX, targetY, entityPositions)
		c.Field.Compute(targetX, targetY, isBlocked, roi)
		c.LastTargetX = targetX
		c.LastTargetY = targetY
		c.TicksSinceCompute = 0
		c.PendingUpdate = false
		c.LastROI = roi
		return true
	}

	// Incremental patch: fill newly-free cells from neighbors
	c.Field.IncrementalUpdate(isBlocked)

	return false
}

// computeROI calculates bounding box from target and entity positions
// Returns nil if no entities (skip computation) or full bounds if needed
func (c *FlowFieldCache) computeROI(targetX, targetY int, entityPositions [][2]int) *ROIBounds {
	if len(entityPositions) == 0 {
		// No entities need flow field - still compute minimal area around target
		// for incremental updates when entities enter range
		return &ROIBounds{
			MinX: max(0, targetX-c.ROIMargin),
			MinY: max(0, targetY-c.ROIMargin),
			MaxX: min(c.Field.Width-1, targetX+c.ROIMargin),
			MaxY: min(c.Field.Height-1, targetY+c.ROIMargin),
		}
	}

	// Initialize AABB with target
	minX, minY := targetX, targetY
	maxX, maxY := targetX, targetY

	// Expand to include all entities
	for _, pos := range entityPositions {
		if pos[0] < minX {
			minX = pos[0]
		}
		if pos[0] > maxX {
			maxX = pos[0]
		}
		if pos[1] < minY {
			minY = pos[1]
		}
		if pos[1] > maxY {
			maxY = pos[1]
		}
	}

	// Apply margin and clamp to field bounds
	return &ROIBounds{
		MinX: max(0, minX-c.ROIMargin),
		MinY: max(0, minY-c.ROIMargin),
		MaxX: min(c.Field.Width-1, maxX+c.ROIMargin),
		MaxY: min(c.Field.Height-1, maxY+c.ROIMargin),
	}
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

// GetROI returns the last computed ROI bounds, nil if none
func (c *FlowFieldCache) GetROI() *ROIBounds {
	return c.LastROI
}