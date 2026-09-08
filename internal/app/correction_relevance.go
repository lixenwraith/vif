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

// relevanceLocked scores, per peer, how many of the shared entities this correction
// moves stand near that participant's cursor. The cursor comes back on the link
// echo, so it is a transport value: read to decide a send time and never written
// where a tick can see it. A keyframe moves the whole world, so what is scored there
// is the shared population near each participant. Caller MUST hold publishMu.
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

// movedEntities is the set of shared entities a delta against the current baseline
// would touch; nil for a keyframe, which carries the world whole. Only placement and
// motion are consulted: they are what a participant standing near an entity
// perceives, and the two stores whose delta says the entity is doing something.
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

// scoreRelevanceLocked turns each participant's raw near-count into the comparative
// share the controller and the priority order read. Comparative rather than
// absolute: in a storm every participant has hundreds of moved entities beside it,
// so a fixed threshold fires for everyone and says nothing. Caller MUST hold
// publishMu.
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

// publishPlanTelemetryLocked publishes the operating point: the cadence in force,
// the interval between whole worlds, what the link was measured to carry, and the
// two conditions a player should be told about. Caller MUST hold publishMu.
func (c *corrections) publishPlanTelemetryLocked(ids []uint32) {
	m := c.a.telemetry
	m.CadenceTicks.Store(int64(c.base))
	m.KeyframePeriod.Store(int64(c.keyPeriod))
	if c.base > 0 {
		m.KeyframeInterval.Store(int64(c.keyPeriod / c.base))
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
	m.UplinkBps.Store(int64(planned))
	m.BudgetBps.Store(int64(budget))
	m.FloorBps.Store(int64(floor))
	m.Constrained.Store(constrained)
	m.FloorBreached.Store(c.breached)

	c.reportFloorLocked()
}

// reportFloorLocked says out loud, once per onset and once on the way out, that a
// link cannot carry the convergence floor. The controller clamps at the floor, so
// the condition it clamped against is unrecoverable by any cadence and naming it is
// the only honest answer. Caller MUST hold publishMu.
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
		"floor_bps", int64(c.a.telemetry.FloorBps.Load()),
		"budget_bps", int64(c.a.telemetry.BudgetBps.Load()))
	c.a.ctx.SetStatusMessage(
		"Link cannot carry a whole world within the convergence floor; corrections may not converge",
		4*parameter.StatusMessageDefaultTimeout, true)
}
