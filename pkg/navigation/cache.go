package navigation

import (
	"github.com/lixenwraith/vif/pkg/vmath"
	"slices"
)

// FlowFieldCache manages flow field recomputation with throttling
type FlowFieldCache struct {
	Field   *FlowField
	grid    []bool // passability used by this cache
	gridGen uint64 // the generation grid was last observed at, zero for none

	// Recomputation throttling
	LastTargets            []vmath.Point // Tracks previously requested target coords
	TicksSinceCompute      int           // Ticks since last computation
	MinTicksBetweenCompute int           // Cooldown between recomputes
	DirtyDistance          int           // Drift, in cells, that recomputes once the cooldown passes
	SettleTicks            int           // Ticks a smaller drift waits before it recomputes

	// PendingUpdate latches true when a caller marks the field dirty, cleared after compute
	PendingUpdate bool

	// Lazy defers each due compute until Materialize, so a field nothing reads costs
	// nothing; the throttle decides exactly as it would eagerly.
	Lazy     bool
	deferred bool // due and reachable, but not yet computed for LastTargets
}

// NewFlowFieldCache creates a cache with default throttling
func NewFlowFieldCache(width, height, minTicks, dirtyDist, settleTicks int) *FlowFieldCache {
	return &FlowFieldCache{
		Field:                  NewFlowField(width, height),
		LastTargets:            make([]vmath.Point, 0, 8),
		TicksSinceCompute:      minTicks, // Allow immediate first compute
		MinTicksBetweenCompute: minTicks,
		DirtyDistance:          dirtyDist,
		SettleTicks:            settleTicks,
		PendingUpdate:          true, // Force initial compute
	}
}

// Clone returns a cache sharing no storage with c, so a copied world continues from
// the field c holds instead of computing one; the search is scratch.
func (c *FlowFieldCache) Clone() *FlowFieldCache {
	n := *c
	n.grid, n.LastTargets = slices.Clone(c.grid), slices.Clone(c.LastTargets)
	if c.Field != nil {
		f := *c.Field
		f.Directions, f.Distances, f.VisitedGen = slices.Clone(f.Directions), slices.Clone(f.Distances), slices.Clone(f.VisitedGen)
		f.search = gridSearch{}
		n.Field = &f
	}
	return &n
}

// Resize adjusts dimensions
func (c *FlowFieldCache) Resize(width, height int) {
	c.Field.Resize(width, height)
	c.LastTargets = c.LastTargets[:0]
	c.PendingUpdate = true
	c.deferred = false
}

// Update recomputes the field when it is due, or in a lazy cache records that it
// is. It is due at once when it has no field or the target count changes; after
// the cooldown, when marked dirty or a target drifted DirtyDistance cells; and a
// smaller drift after SettleTicks. Returns true when the field was due.
func (c *FlowFieldCache) Update(targets []vmath.Point, isBlocked WallChecker) bool {
	c.TicksSinceCompute++
	due := !c.Computed() || len(targets) != len(c.LastTargets)
	if !due && c.TicksSinceCompute >= c.MinTicksBetweenCompute {
		drift := 0
		for i, t := range targets {
			drift = max(drift, abs(t.X-c.LastTargets[i].X)+abs(t.Y-c.LastTargets[i].Y))
		}
		due = c.PendingUpdate || drift >= c.DirtyDistance || drift > 0 && c.TicksSinceCompute >= c.SettleTicks
	}
	if !due {
		return false
	}
	c.LastTargets = append(c.LastTargets[:0], targets...)
	c.TicksSinceCompute = 0
	c.PendingUpdate = false
	c.compute(isBlocked)
	return true
}

// compute derives the field for LastTargets, or in a lazy cache defers it,
// knowing already whether it will be valid
func (c *FlowFieldCache) compute(isBlocked WallChecker) {
	if !c.Lazy {
		c.Field.Compute(c.LastTargets, isBlocked)
		return
	}
	c.Field.Invalidate()
	c.deferred = c.Field.Reachable(c.LastTargets, isBlocked)
}

// Materialize computes a deferred field before it is read. isBlocked must answer
// as it did when the field fell due, which holds while the observed grid is unmoved.
func (c *FlowFieldCache) Materialize(isBlocked WallChecker) {
	if c.deferred {
		c.Field.Compute(c.LastTargets, isBlocked)
		c.deferred = false
	}
}

// MarkDirty forces recomputation on next eligible tick
func (c *FlowFieldCache) MarkDirty() {
	c.PendingUpdate = true
}

// ObserveGrid invalidates routes immediately when their passability changes.
// The slice may describe blocked or passable cells; only equality matters. A nonzero
// gen is the caller's name for grid's content, moving whenever it may have changed,
// so a grid observed at the same gen is not compared again.
func (c *FlowFieldCache) ObserveGrid(grid []bool, gen uint64) {
	if gen != 0 && gen == c.gridGen {
		return
	}
	c.gridGen = gen
	if !slices.Equal(c.grid, grid) {
		c.grid = append(c.grid[:0], grid...)
		c.Field.Invalidate()
		c.deferred = false
	}
}

// Rebuild derives a restored field without advancing its captured throttle phase.
func (c *FlowFieldCache) Rebuild(isBlocked WallChecker) {
	c.compute(isBlocked)
}

// Computed reports whether a field has been derived at least once. It is part of
// the throttle phase for the same reason the counters are: an install that left the
// field underived would force a compute on the next tick that the instance it
// copied is not making.
func (c *FlowFieldCache) Computed() bool { return c.Field != nil && (c.Field.Valid || c.deferred) }

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
