// Package app: relevance, priority, and the operating point a player can see.
//
// Relevance in this design is a *scheduling* input and never a filter. The
// tempting shape — send each participant only the entities near it — cannot
// carry D-23's exactness proof: a delta is verified by reconstructing the
// sender's capture and re-hashing it, so a correction carrying a subset of the
// world reconstructs a capture nobody holds and has no proof left to offer. A
// scoped correction would also mean a receiver holding a world assembled from
// two ticks, which is the class of divergence this whole plan exists to stop
// having.
//
// So what relevance moves is *when* a participant's next correction goes out,
// not what is in it. A participant with shared entities churning around its
// cursor is published to at the fastest cadence its link allows; one with
// nothing near it settles for the quiet cadence and gives the budget back. The
// authority is untouched — every correction is still the host's whole world or
// the exact difference from the last one — and the participant who needs the
// freshness gets it, which is what the requirement asks for.
//
// The scope-the-content option is not dead, it is Phase 6's: a scoped correction
// needs its own integrity contract over the subset and a partial reconcile that
// does not adopt the authority's tick, and both are more than a cadence change.
package app

import (
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
	"github.com/lixenwraith/vi-fighter/pkg/linkpace"
)

// relevanceLocked scores, per peer, how many of the shared entities this
// correction moves stand near that participant's own cursor.
//
// The cursor comes back on the peer's link echo, which makes it a transport
// value rather than a simulation one: it is read here to decide a send time and
// is never written anywhere a tick can see. A stale or missing one costs a
// correction sent sooner than it needed to be.
//
// A keyframe moves the whole world by definition, so scoring one entity by entity
// would say "everything is relevant to everyone" and tell the schedule nothing.
// What is scored instead is the shared population near each participant, which is
// the same question asked of the frame that carries it.
//
// Caller MUST hold publishMu.
func (c *corrections) relevanceLocked(
	cap snapshot.SharedCapture, keyframe bool, link engine.LinkMeasuringPort, ids []uint32,
) map[uint32]int {
	out := make(map[uint32]int, len(ids))
	if link == nil {
		return out
	}
	interests := make(map[uint32]linkpace.Cell, len(ids))
	any := false
	for _, id := range ids {
		if cell := link.LinkMetric(id).Interest; cell.Valid {
			interests[id], any = cell, true
		}
	}
	if !any {
		return out
	}

	moved := movedEntities(c.baseline, cap, keyframe)
	radius := parameter.SnapshotRelevanceRadius
	for _, entry := range cap.World.Positions {
		if _, ok := moved[entry.Entity]; !ok && !keyframe {
			continue
		}
		for id, cell := range interests {
			if near(entry.Value, cell, radius) {
				out[id]++
			}
		}
	}
	return out
}

// near is a square neighbourhood rather than a circle, deliberately: the map is a
// grid of character cells, the radius is a scheduling threshold rather than a
// distance, and a square costs two comparisons where a circle costs a multiply
// per entity per peer on the publication path.
func near(p component.PositionComponent, c linkpace.Cell, radius int) bool {
	dx := p.X - int(c.X)
	dy := p.Y - int(c.Y)
	return dx >= -radius && dx <= radius && dy >= -radius && dy <= radius
}

// movedEntities is the set of shared entities a delta against the current
// baseline would touch. A keyframe carries the world whole, so the set is not
// computed for one — the caller scores the whole population instead.
//
// Only placement and motion are consulted. They are what a participant standing
// near an entity actually perceives, and they are the two stores whose delta says
// "this entity is doing something" rather than "a counter on it changed".
func movedEntities(base, next snapshot.SharedCapture, keyframe bool) map[core.Entity]struct{} {
	if keyframe {
		return nil
	}
	d := engine.DiffSharedWorld(base.World, next.World)
	out := make(map[core.Entity]struct{}, len(d.Positions.Changed)+len(d.Kinetic.Changed))
	for _, e := range d.Positions.Changed {
		out[e.Entity] = struct{}{}
	}
	for _, e := range d.Kinetic.Changed {
		out[e.Entity] = struct{}{}
	}
	return out
}

// scoreRelevanceLocked turns each participant's raw near-count into the
// comparative share the controller and the priority order read.
//
// Comparative rather than absolute, and that is the whole of why relevance is
// usable as a scheduling signal at all. A count is a fact about the world: in a
// storm every participant has hundreds of moved entities beside it, so any fixed
// threshold fires for everyone at once and the signal says nothing. What is worth
// acting on is that *this* participant has more at stake in the next correction
// than the others do — which with one guest is nobody, and the whole link is
// already its own.
//
// Caller MUST hold publishMu.
func (c *corrections) scoreRelevanceLocked(ids []uint32, near map[uint32]int) {
	total := 0
	counted := 0
	for _, id := range ids {
		if c.peers[id] == nil {
			continue
		}
		total += near[id]
		counted++
	}
	if counted == 0 {
		return
	}
	mean := float64(total) / float64(counted)
	for _, id := range ids {
		p := c.peers[id]
		if p == nil {
			continue
		}
		p.near = near[id]
		p.share = 0
		if mean >= 1 {
			if above := float64(p.near) - mean; above > 0 {
				p.share = int(100 * above / mean)
			}
		}
	}
}

// publishPlanTelemetryLocked publishes the operating point: what cadence is in
// force, how long the session leaves between whole worlds, what the link was
// measured to carry, and whether either of the two conditions a player should be
// told about holds.
//
// Caller MUST hold publishMu.
func (c *corrections) publishPlanTelemetryLocked(ids []uint32) {
	m := c.a.snapshotTelemetry
	m.cadenceTicks.Store(int64(c.base))
	m.keyframePeriod.Store(int64(c.keyPeriod))
	if c.base > 0 {
		m.keyframeInterval.Store(int64(c.keyPeriod / c.base))
	}

	// The session is as constrained as its most constrained edge, and the budget
	// worth reporting is the tightest one: an average would hide the peer that
	// needs saying.
	constrained := false
	budget, planned, floor := 0.0, 0.0, 0.0
	for _, id := range ids {
		p := c.peers[id]
		if p == nil {
			continue
		}
		constrained = constrained || p.plan.Constrained
		if p.plan.FloorBps > floor {
			floor = p.plan.FloorBps
		}
		if p.plan.PlannedBps > planned {
			planned = p.plan.PlannedBps
		}
		if b := p.plan.BudgetBps; b > 0 && (budget == 0 || b < budget) {
			budget = b
		}
	}
	m.uplinkBps.Store(int64(planned))
	m.budgetBps.Store(int64(budget))
	m.floorBps.Store(int64(floor))
	m.constrained.Store(constrained)
	m.floorBreached.Store(c.breached)

	c.reportFloorLocked()
}

// reportFloorLocked says out loud, once per onset, that a link cannot carry the
// convergence floor.
//
// This is the boundary the plan states as a must-not: adaptation may not reach a
// rate at which convergence is not guaranteed. It does not — the controller
// clamps at the floor — and the condition it clamped against is unrecoverable by
// any cadence, so the honest thing is to name it rather than to keep publishing a
// schedule that cannot deliver what it promises. It is said once on the way in
// and once on the way out, because a message per correction is not a report.
//
// Caller MUST hold publishMu.
func (c *corrections) reportFloorLocked() {
	if c.breached == c.saidFloor {
		return
	}
	c.saidFloor = c.breached
	if !c.breached {
		vlog.Info("app", "msg", "link is carrying the convergence floor again",
			"floor_ticks", c.bounds.FloorKeyframeTicks)
		c.a.ctx.SetStatusMessage("Link recovered; corrections are converging again",
			parameter.StatusMessageDefaultTimeout, false)
		return
	}
	vlog.Warn("app", "msg", "link cannot sustain the convergence floor",
		"floor_ticks", c.bounds.FloorKeyframeTicks,
		"floor_bps", int64(c.a.snapshotTelemetry.floorBps.Load()),
		"budget_bps", int64(c.a.snapshotTelemetry.budgetBps.Load()))
	c.a.ctx.SetStatusMessage(
		"Link cannot carry a whole world within the convergence floor; corrections may not converge",
		4*parameter.StatusMessageDefaultTimeout, true)
}
