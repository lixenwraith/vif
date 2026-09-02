package linkpace

import (
	"errors"
	"fmt"
	"time"
)

// Bounds is the operating envelope a controller may move inside. Every field is
// a declared limit rather than a tuning knob the controller can talk itself out
// of: the search below never returns a plan outside them, and the floor is
// enforced before capacity is even consulted.
type Bounds struct {
	// TickInterval converts a cadence in ticks into a period in seconds. The
	// cadence is counted in ticks rather than in seconds because it is a property
	// of the simulation: a paused or slowed run should not keep publishing a world
	// that is not moving.
	TickInterval time.Duration

	// MinCadenceTicks is the fastest a correction may be published — the freshness
	// a participant with something happening around it can be given when the link
	// can carry it. MaxCadenceTicks is the slowest adaptation may go.
	MinCadenceTicks     uint64
	NominalCadenceTicks uint64
	MaxCadenceTicks     uint64

	// MinKeyframe, NominalKeyframe and MaxKeyframe bound how many corrections may
	// pass between whole captures. A larger interval is cheaper and slower to
	// recover from a lost keyframe, which is why the floor bounds it too.
	MinKeyframe     int
	NominalKeyframe int
	MaxKeyframe     int

	// FloorKeyframeTicks is the convergence floor: the most ticks a receiver may
	// go without a whole authoritative world. CadenceTicks * KeyframeInterval may
	// never exceed it, and a link that cannot carry one keyframe per floor window
	// cannot sustain the floor at all.
	FloorKeyframeTicks uint64

	// Utilisation is the share of measured capacity the cadence is allowed to
	// spend. The rest is headroom for the crossing stream, the state syncs and
	// the digests, which travel on the same link and are not in this model.
	Utilisation float64

	// UrgentMagnitude is the correction magnitude at which a participant is
	// treated as drifting faster than the nominal cadence repairs, and QuietTicks
	// the cadence a participant with nothing relevant near it falls back to.
	UrgentMagnitude int
	UrgentRelevance int
	QuietCadence    uint64

	// RecoverStepTicks and RecoverStepKeyframe bound how fast the controller may
	// move *back* toward its nominal point. Degradation is immediate — a link that
	// has narrowed has already narrowed — but recovery is stepped, so one lucky
	// sample cannot restore a cadence the link has not actually regained.
	RecoverStepTicks    uint64
	RecoverStepKeyframe int
}

// Validate refuses an envelope whose bounds contradict each other. It is called
// by NewController, so a malformed envelope fails where it is declared rather
// than by producing a plan nobody asked for.
func (b Bounds) Validate() error {
	switch {
	case b.TickInterval <= 0:
		return errors.New("linkpace: tick interval must be positive")
	case b.MinCadenceTicks == 0:
		return errors.New("linkpace: minimum cadence must be at least one tick")
	case b.MinCadenceTicks > b.NominalCadenceTicks || b.NominalCadenceTicks > b.MaxCadenceTicks:
		return fmt.Errorf("linkpace: cadence bounds out of order (%d/%d/%d)",
			b.MinCadenceTicks, b.NominalCadenceTicks, b.MaxCadenceTicks)
	case b.MinKeyframe < 1:
		return errors.New("linkpace: minimum keyframe interval must be at least one correction")
	case b.MinKeyframe > b.NominalKeyframe || b.NominalKeyframe > b.MaxKeyframe:
		return fmt.Errorf("linkpace: keyframe bounds out of order (%d/%d/%d)",
			b.MinKeyframe, b.NominalKeyframe, b.MaxKeyframe)
	case b.FloorKeyframeTicks < b.MinCadenceTicks*uint64(b.MinKeyframe):
		return fmt.Errorf("linkpace: the floor of %d ticks is shorter than the fastest legal keyframe period (%d)",
			b.FloorKeyframeTicks, b.MinCadenceTicks*uint64(b.MinKeyframe))
	case b.Utilisation <= 0 || b.Utilisation > 1:
		return errors.New("linkpace: utilisation must be within (0,1]")
	}
	return nil
}

// Sizes is what one correction currently costs on the wire, measured rather than
// assumed. Both are needed: a schedule's cost is one keyframe plus the deltas
// between them, and at this world's storm high water the two differ six-fold.
type Sizes struct {
	Keyframe int64
	Delta    int64
}

// Demand is what a participant wants, as opposed to what its link can carry.
//
// Magnitude is how far that participant's prediction had drifted when the last
// correction reached it, and Relevance how many of the entities the next
// correction moves are near enough to matter to it. Neither can buy a cadence
// the link cannot sustain; what they decide is where inside the feasible range
// the plan sits.
type Demand struct {
	// Known distinguishes "this participant reported nothing near it" from "this
	// participant has not reported yet". Without it a peer whose first echo has
	// not arrived reads as quiet and is published to slower than nominal for its
	// first seconds, which is exactly backwards: an instance that has just
	// installed a world is the one most likely to need the next correction.
	Known bool

	Magnitude int
	Relevance int
}

// Plan is one peer's operating point, and the reason for it.
type Plan struct {
	CadenceTicks     uint64
	KeyframeInterval int

	// Constrained reports that this plan is not the nominal one — the link, or
	// the demand, moved it. A player seeing it should be told the link is the
	// reason the picture is coarser, rather than guessing the game is broken.
	Constrained bool

	// FloorBreached reports the condition adaptation must never hide: the link
	// cannot carry one whole world per floor window, so convergence within the
	// floor is not guaranteed no matter how the cadence is set. The plan still
	// sits exactly at the floor — it never goes below — and the condition is
	// reported instead.
	FloorBreached bool

	// FloorBps is what the floor costs on this world: one keyframe per floor
	// window and nothing else, which is the cheapest schedule that honours it.
	// PlannedBps is what this plan costs. BudgetBps is what the link was measured
	// to allow after the utilisation share.
	FloorBps   float64
	PlannedBps float64
	BudgetBps  float64

	Reason string
}

// KeyframePeriodTicks is how long this plan leaves between whole worlds, which
// is the value the floor bounds.
func (p Plan) KeyframePeriodTicks() uint64 { return p.CadenceTicks * uint64(p.KeyframeInterval) }

// Controller holds one peer's operating point across decisions.
//
// It is stateful for one reason: a plan is only allowed to *improve* one step at
// a time. Degradation is immediate, because a link that has narrowed has already
// narrowed and publishing into it for another second helps nobody; recovery is
// stepped, because a single sample taken during a quiet moment is not evidence
// that the link came back.
type Controller struct {
	b    Bounds
	plan Plan
}

// NewController returns a controller resting at its nominal operating point.
func NewController(b Bounds) (*Controller, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return &Controller{b: b, plan: Plan{
		CadenceTicks:     b.NominalCadenceTicks,
		KeyframeInterval: b.NominalKeyframe,
		Reason:           "nominal",
	}}, nil
}

// Bounds returns the envelope this controller was built with.
func (c *Controller) Bounds() Bounds { return c.b }

// Plan returns the current operating point without taking a decision.
func (c *Controller) Plan() Plan { return c.plan }

// Update takes one decision and returns the new operating point.
//
// The order is deliberate and is the whole safety argument. The floor bounds the
// search space first, so no candidate the search can return violates it. Demand
// then says where inside that space this participant would like to sit. Capacity
// then says which of those candidates the link can actually carry. If none can,
// the plan clamps to the floor and says so — it never continues down.
func (c *Controller) Update(m Metrics, s Sizes, d Demand) Plan {
	next := c.decide(m, s, d)
	c.plan = c.step(next)
	return c.plan
}

// decide computes the operating point the measurements argue for, before the
// step limiter is applied.
func (c *Controller) decide(m Metrics, s Sizes, d Demand) Plan {
	b := c.b
	out := Plan{
		CadenceTicks:     c.desired(d),
		KeyframeInterval: b.NominalKeyframe,
		Reason:           "nominal",
	}
	out.FloorBps = floorRate(b, s)
	out.PlannedBps = rate(b, out.CadenceTicks, out.KeyframeInterval, s)

	// The floor binds regardless of measurement: a nominal schedule that leaves
	// more than the floor between whole worlds is not a legal starting point.
	out = clampToFloor(b, out, s)

	// Nothing measured yet, or the rate observed while the link was never the
	// limit. Either way capacity is unknown, and the honest response is to keep
	// the nominal point rather than to throttle on a number that means "the
	// sender was idle".
	if !m.Ready || !m.Saturated || m.Throughput <= 0 {
		if d.Known && (d.Magnitude >= b.UrgentMagnitude || d.Relevance >= b.UrgentRelevance) {
			out.Reason = "demand"
			out.Constrained = out.CadenceTicks != b.NominalCadenceTicks ||
				out.KeyframeInterval != b.NominalKeyframe
		}
		return out
	}

	budget := m.Throughput * b.Utilisation
	out.BudgetBps = budget

	// The floor's own cost is the first question, and it is not a preference. If
	// one whole world per floor window does not fit in the budget, no schedule
	// this controller can choose converges within the floor.
	if out.FloorBps > budget {
		floorPlan := cheapestFloorPlan(b)
		floorPlan.FloorBps, floorPlan.BudgetBps = out.FloorBps, budget
		floorPlan.PlannedBps = rate(b, floorPlan.CadenceTicks, floorPlan.KeyframeInterval, s)
		floorPlan.Constrained, floorPlan.FloorBreached = true, true
		floorPlan.Reason = "link below the convergence floor"
		return floorPlan
	}

	// A cadence faster than the link's own timing noise buys nothing: the
	// corrections arrive bunched and the extra bytes are spent describing a world
	// the receiver could not have shown any sooner.
	minTicks := max(c.desired(d), ticksFor(b, 2*m.Jitter))

	if plan, ok := search(b, minTicks, s, budget); ok {
		plan.FloorBps, plan.BudgetBps = out.FloorBps, budget
		plan.Constrained = plan.CadenceTicks != b.NominalCadenceTicks ||
			plan.KeyframeInterval != b.NominalKeyframe
		if plan.Constrained && plan.Reason == "" {
			plan.Reason = "link"
		} else if plan.Reason == "" {
			plan.Reason = "nominal"
		}
		return plan
	}

	// Every candidate inside the bounds costs more than the budget, yet the floor
	// itself fits: sit at the cheapest schedule that honours the floor. This is
	// the constrained-but-converging case, and it is not a breach.
	fallback := cheapestFloorPlan(b)
	fallback.FloorBps, fallback.BudgetBps = out.FloorBps, budget
	fallback.PlannedBps = rate(b, fallback.CadenceTicks, fallback.KeyframeInterval, s)
	fallback.Constrained = true
	fallback.Reason = "link at the convergence floor"
	return fallback
}

// desired is where this participant would like to sit before the link is
// consulted: fast when its own picture is drifting or its neighbourhood is busy,
// slower than nominal when neither is true and the bytes are better spent
// elsewhere.
func (c *Controller) desired(d Demand) uint64 {
	b := c.b
	switch {
	case !d.Known:
		return b.NominalCadenceTicks
	case b.UrgentMagnitude > 0 && d.Magnitude >= b.UrgentMagnitude:
		return b.MinCadenceTicks
	case b.UrgentRelevance > 0 && d.Relevance >= b.UrgentRelevance:
		return b.MinCadenceTicks
	case d.Magnitude == 0 && d.Relevance == 0 && b.QuietCadence > b.NominalCadenceTicks:
		return min(b.QuietCadence, b.MaxCadenceTicks)
	default:
		return b.NominalCadenceTicks
	}
}

// search walks the feasible space in preference order and returns the first
// candidate the budget can carry.
//
// The order is the degradation order, and it is a design statement rather than
// an implementation detail. For a given cadence the keyframe interval is
// stretched first, because a keyframe costs several times a delta on this world
// and stretching it costs only recovery time — which the floor already bounds.
// Only when no legal interval fits does the cadence itself slow, because that is
// what a player actually sees.
//
// Both loops are bounded by the floor rather than by the declared maxima: at a
// slow cadence the floor allows fewer corrections between whole worlds than the
// nominal interval names, so the interval a cadence *starts* from is the nominal
// one pulled inside the floor. Starting from the nominal unconditionally would
// skip every cadence past FloorKeyframeTicks/NominalKeyframe entirely, which is
// most of the range this controller exists to reach.
func search(b Bounds, from uint64, s Sizes, budget float64) (Plan, bool) {
	for c := max(from, b.MinCadenceTicks); c <= b.MaxCadenceTicks; c++ {
		kMax := int(b.FloorKeyframeTicks / c)
		if kMax < b.MinKeyframe {
			continue // this cadence cannot honour the floor at any interval
		}
		kMax = min(kMax, b.MaxKeyframe)
		kFrom := max(min(b.NominalKeyframe, kMax), b.MinKeyframe)
		for k := kFrom; k <= kMax; k++ {
			if r := rate(b, c, k, s); r <= budget {
				return Plan{CadenceTicks: c, KeyframeInterval: k, PlannedBps: r}, true
			}
		}
	}
	return Plan{}, false
}

// cheapestFloorPlan is the least expensive schedule that still honours the
// floor, and it is both the fallback and the price the floor is quoted at.
//
// The cheapest schedule is the slowest cadence the bounds allow with the fewest
// keyframes that still fills the floor window: the whole world has to be sent
// once per window whatever else happens, so what is left to minimise is the
// deltas in between, and the slowest cadence sends the fewest of them.
func cheapestFloorPlan(b Bounds) Plan {
	c := max(min(b.FloorKeyframeTicks, b.MaxCadenceTicks), b.MinCadenceTicks)
	k := max(min(int(b.FloorKeyframeTicks/c), b.MaxKeyframe), b.MinKeyframe)
	for k > b.MinKeyframe && c*uint64(k) > b.FloorKeyframeTicks {
		k--
	}
	return Plan{CadenceTicks: c, KeyframeInterval: k}
}

// clampToFloor pulls a candidate back inside the floor without slowing it: the
// keyframe interval shortens until the whole world fits in the floor window.
func clampToFloor(b Bounds, p Plan, s Sizes) Plan {
	if p.CadenceTicks > b.FloorKeyframeTicks {
		p.CadenceTicks = max(b.MinCadenceTicks, min(b.FloorKeyframeTicks, b.MaxCadenceTicks))
	}
	for p.KeyframeInterval > b.MinKeyframe &&
		p.CadenceTicks*uint64(p.KeyframeInterval) > b.FloorKeyframeTicks {
		p.KeyframeInterval--
	}
	p.PlannedBps = rate(b, p.CadenceTicks, p.KeyframeInterval, s)
	return p
}

// step moves the live plan toward the decided one, immediately when that is a
// degradation and by one bounded step when it is a recovery.
func (c *Controller) step(next Plan) Plan {
	cur := c.plan
	out := next

	if next.CadenceTicks < cur.CadenceTicks && c.b.RecoverStepTicks > 0 {
		if cur.CadenceTicks-next.CadenceTicks > c.b.RecoverStepTicks {
			out.CadenceTicks = cur.CadenceTicks - c.b.RecoverStepTicks
		}
	}
	if next.KeyframeInterval < cur.KeyframeInterval && c.b.RecoverStepKeyframe > 0 {
		if cur.KeyframeInterval-next.KeyframeInterval > c.b.RecoverStepKeyframe {
			out.KeyframeInterval = cur.KeyframeInterval - c.b.RecoverStepKeyframe
		}
	}
	// A step that stops short of the decision must still respect the floor, and
	// stopping short can only ever be a slower cadence or a longer interval than
	// the decision — so re-clamp rather than trust the arithmetic.
	for out.KeyframeInterval > c.b.MinKeyframe &&
		out.CadenceTicks*uint64(out.KeyframeInterval) > c.b.FloorKeyframeTicks {
		out.KeyframeInterval--
	}
	if out.CadenceTicks*uint64(out.KeyframeInterval) > c.b.FloorKeyframeTicks {
		out.CadenceTicks = max(c.b.MinCadenceTicks,
			c.b.FloorKeyframeTicks/uint64(out.KeyframeInterval))
	}
	if out.CadenceTicks != next.CadenceTicks || out.KeyframeInterval != next.KeyframeInterval {
		out.PlannedBps = 0 // recomputed by the next decision; a stepped plan has no priced rate
		out.Constrained = out.CadenceTicks != c.b.NominalCadenceTicks ||
			out.KeyframeInterval != c.b.NominalKeyframe
		if out.Reason == "" || out.Reason == "nominal" {
			out.Reason = "recovering"
		}
	}
	return out
}

// rate prices one schedule: a keyframe plus the deltas that follow it, over the
// ticks that cycle occupies.
func rate(b Bounds, cadence uint64, keyframe int, s Sizes) float64 {
	if cadence == 0 || keyframe <= 0 {
		return 0
	}
	cycle := float64(cadence) * float64(keyframe) * b.TickInterval.Seconds()
	if cycle <= 0 {
		return 0
	}
	return (float64(s.Keyframe) + float64(keyframe-1)*float64(s.Delta)) / cycle
}

// floorRate is what the convergence floor costs on a world of this size: the
// price of the cheapest schedule that honours it. Quoting it as "one keyframe
// per window" alone would understate it wherever the declared maximum cadence
// cannot stretch to the whole window, because then the floor is met by a
// keyframe *and* the deltas the bounds force in beside it.
func floorRate(b Bounds, s Sizes) float64 {
	p := cheapestFloorPlan(b)
	return rate(b, p.CadenceTicks, p.KeyframeInterval, s)
}

// ticksFor rounds a duration up to whole ticks.
func ticksFor(b Bounds, d time.Duration) uint64 {
	if d <= 0 || b.TickInterval <= 0 {
		return 0
	}
	n := (d + b.TickInterval - 1) / b.TickInterval
	if n < 0 {
		return 0
	}
	return uint64(n)
}
