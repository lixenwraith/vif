package navigation

import (
	"github.com/lixenwraith/vif/pkg/vmath"
	"slices"
)

// FlowFieldCache manages flow field recomputation with throttling
type FlowFieldCache struct {
	Field *FlowField

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
	n.LastTargets = slices.Clone(c.LastTargets)
	if c.Field != nil {
		f := *c.Field
		f.Directions, f.Distances, f.VisitedGen = slices.Clone(f.Directions), slices.Clone(f.Distances), slices.Clone(f.VisitedGen)
		f.search, f.changed = SearchGrid{}, nil
		n.Field = &f
	}
	return &n
}

// Reset returns the cache to the state NewFlowFieldCache leaves it in, keeping
// its storage for a group that replaces one removed
func (c *FlowFieldCache) Reset(width, height int) {
	c.Resize(width, height)
	c.TicksSinceCompute = c.MinTicksBetweenCompute
}

// Resize adjusts dimensions
func (c *FlowFieldCache) Resize(width, height int) {
	c.Field.Resize(width, height)
	c.LastTargets = c.LastTargets[:0]
	c.PendingUpdate = true
	c.deferred = false
}

// Update recomputes the field on g when it is due, or in a lazy cache records that
// it is. It is due at once when it has no field or the target count changes; after
// the cooldown, when marked dirty or a target drifted DirtyDistance cells; and a
// smaller drift after SettleTicks. Returns true when the field was due.
func (c *FlowFieldCache) Update(targets []vmath.Point, g *SearchGrid) bool {
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
	c.compute(g)
	return true
}

// compute derives the field for LastTargets, or in a lazy cache defers it,
// knowing already whether it will be valid
func (c *FlowFieldCache) compute(g *SearchGrid) {
	if !c.Lazy {
		c.Field.ComputeOn(g, c.LastTargets)
		return
	}
	c.Field.Invalidate()
	c.deferred = c.Field.Reachable(g, c.LastTargets)
}

// Materialize computes a deferred field before it is read. g must hold the
// passability the field fell due on, which holds while the observed grid is unmoved.
func (c *FlowFieldCache) Materialize(g *SearchGrid) {
	if c.deferred {
		c.Field.ComputeOn(g, c.LastTargets)
		c.deferred = false
	}
}

// MarkDirty forces recomputation on next eligible tick
func (c *FlowFieldCache) MarkDirty() {
	c.PendingUpdate = true
}

// Closed drops the field after a cell became impassable, since a direction may
// now point into it; the next Update finds it due at once.
func (c *FlowFieldCache) Closed() {
	c.Field.Invalidate()
	c.deferred = false
}

// Moved hands the cache one tick's change of its grid: a cell closed drops the
// field, cells opened alone repair a computed field in place. One a repair would
// reseed computes again, deferred or not as the cache is, and a deferred field
// computes on the grid as it stands when read.
func (c *FlowFieldCache) Moved(g *SearchGrid, closed, opened []vmath.Point) {
	if len(closed) > 0 {
		c.Closed()
		return
	}
	if c.Field.Valid && !c.Field.Opened(g, opened, c.LastTargets) {
		c.compute(g)
	}
}

// Rebuild derives a restored field without advancing its captured throttle phase.
func (c *FlowFieldCache) Rebuild(g *SearchGrid) {
	c.compute(g)
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
