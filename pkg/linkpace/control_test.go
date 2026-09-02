package linkpace

import (
	"errors"
	"testing"
	"time"
)

// gameBounds is the shipped envelope, restated here so a change to the game's
// parameters shows up as a failing expectation rather than as a silently
// different test. The numbers themselves are asserted against the parameter
// package by internal/app's cadence test.
func gameBounds() Bounds {
	return Bounds{
		TickInterval:        50 * time.Millisecond,
		MinCadenceTicks:     2,
		NominalCadenceTicks: 4,
		MaxCadenceTicks:     60,
		MinKeyframe:         1,
		NominalKeyframe:     10,
		MaxKeyframe:         30,
		FloorKeyframeTicks:  60,
		Utilisation:         0.75,
		UrgentMagnitude:     8,
		UrgentRelevance:     4,
		QuietCadence:        8,
		RecoverStepTicks:    2,
		RecoverStepKeyframe: 2,
	}
}

// stormSizes and quietSizes are the two measured worlds from the plan's §8: the
// storm high water and the world at rest.
func stormSizes() Sizes { return Sizes{Keyframe: 175_908, Delta: 29_488} }
func quietSizes() Sizes { return Sizes{Keyframe: 10_891, Delta: 3_200} }

func measured(throughput float64, jitter time.Duration) Metrics {
	return Metrics{
		RTT:        60 * time.Millisecond,
		MinRTT:     50 * time.Millisecond,
		Jitter:     jitter,
		Throughput: throughput,
		Saturated:  true,
		Samples:    8,
		Ready:      true,
	}
}

func mustController(t *testing.T, b Bounds) *Controller {
	t.Helper()
	c, err := NewController(b)
	if err != nil {
		t.Fatalf("controller: %v", err)
	}
	return c
}

func TestBoundsRefuseAContradictoryEnvelope(t *testing.T) {
	cases := map[string]func(*Bounds){
		"no tick":            func(b *Bounds) { b.TickInterval = 0 },
		"zero cadence":       func(b *Bounds) { b.MinCadenceTicks = 0 },
		"cadence order":      func(b *Bounds) { b.NominalCadenceTicks = 1 },
		"keyframe order":     func(b *Bounds) { b.NominalKeyframe = 0; b.MinKeyframe = 2 },
		"floor under a step": func(b *Bounds) { b.FloorKeyframeTicks = 1; b.MinCadenceTicks = 2; b.MinKeyframe = 2 },
		"utilisation":        func(b *Bounds) { b.Utilisation = 0 },
	}
	for name, break_ := range cases {
		b := gameBounds()
		break_(&b)
		if err := b.Validate(); err == nil {
			t.Errorf("%s: an invalid envelope validated", name)
		}
		if _, err := NewController(b); err == nil {
			t.Errorf("%s: an invalid envelope built a controller", name)
		}
	}
}

// TestNominalUntilTheLinkIsMeasured is the "do no harm" case: a delivery rate
// observed while the sender was idle is not capacity, and a controller that
// throttled on one would narrow every session's first seconds.
func TestNominalUntilTheLinkIsMeasured(t *testing.T) {
	b := gameBounds()
	for name, m := range map[string]Metrics{
		"nothing measured": {},
		"not yet ready":    {Throughput: 1000, Saturated: true, Samples: 1},
		"never saturated":  {Throughput: 1000, Saturated: false, Samples: 20, Ready: true},
	} {
		c := mustController(t, b)
		p := c.Update(m, stormSizes(), Demand{})
		if p.CadenceTicks != b.NominalCadenceTicks || p.KeyframeInterval != b.NominalKeyframe {
			t.Errorf("%s: moved to %d/%d without evidence", name, p.CadenceTicks, p.KeyframeInterval)
		}
		if p.FloorBreached {
			t.Errorf("%s: reported a floor breach without a measurement", name)
		}
	}
}

// TestKeyframesStretchBeforeTheCadenceSlows pins the degradation order. A
// keyframe is six times a delta on this world, so the first thing to give under
// pressure is how often a whole world is sent — which costs recovery time the
// floor already bounds — and only then the cadence, which is what a player sees.
func TestKeyframesStretchBeforeTheCadenceSlows(t *testing.T) {
	b, s := gameBounds(), stormSizes()
	nominal := rate(b, b.NominalCadenceTicks, b.NominalKeyframe, s)

	// Just under the nominal schedule's cost: enough to keep the cadence and
	// stretch the interval.
	c := mustController(t, b)
	p := c.Update(measured(nominal*0.95/b.Utilisation, 0), s, Demand{})
	if p.CadenceTicks != b.NominalCadenceTicks {
		t.Fatalf("cadence slowed to %d before the keyframe interval stretched", p.CadenceTicks)
	}
	if p.KeyframeInterval <= b.NominalKeyframe {
		t.Fatalf("keyframe interval stayed at %d under a budget below the nominal rate", p.KeyframeInterval)
	}

	// Well under: no interval saves enough, so the cadence has to give.
	c = mustController(t, b)
	p = c.Update(measured(nominal*0.2/b.Utilisation, 0), s, Demand{})
	if p.CadenceTicks <= b.NominalCadenceTicks {
		t.Fatalf("cadence held at %d on a link carrying a fifth of the nominal rate", p.CadenceTicks)
	}
	if !p.Constrained {
		t.Fatal("a slowed cadence was not reported as constrained")
	}
}

// TestTheFloorIsNeverCrossed sweeps the whole input space the controller can be
// driven through and asserts the one invariant adaptation may not trade away.
func TestTheFloorIsNeverCrossed(t *testing.T) {
	b := gameBounds()
	sizes := []Sizes{quietSizes(), stormSizes(), {Keyframe: 1 << 20, Delta: 1 << 18}, {Keyframe: 1, Delta: 1}}
	rates := []float64{0, 1, 500, 5_000, 50_000, 215_000, 5 << 20}
	jitters := []time.Duration{0, 5 * time.Millisecond, 90 * time.Millisecond, 4 * time.Second}
	demands := []Demand{
		{}, {Known: true}, {Known: true, Magnitude: 40},
		{Known: true, Relevance: 12}, {Known: true, Magnitude: 1, Relevance: 1},
	}

	for _, s := range sizes {
		for _, r := range rates {
			for _, j := range jitters {
				for _, d := range demands {
					c := mustController(t, b)
					for range 40 { // long enough for every step limiter to settle
						p := c.Update(measured(r, j), s, d)
						if got := p.KeyframePeriodTicks(); got > b.FloorKeyframeTicks {
							t.Fatalf("sizes %v rate %.0f jitter %s demand %+v: %d ticks between whole worlds, floor is %d",
								s, r, j, d, got, b.FloorKeyframeTicks)
						}
						if p.CadenceTicks < b.MinCadenceTicks || p.CadenceTicks > b.MaxCadenceTicks {
							t.Fatalf("cadence %d outside [%d,%d]", p.CadenceTicks, b.MinCadenceTicks, b.MaxCadenceTicks)
						}
						if p.KeyframeInterval < b.MinKeyframe || p.KeyframeInterval > b.MaxKeyframe {
							t.Fatalf("keyframe interval %d outside [%d,%d]", p.KeyframeInterval, b.MinKeyframe, b.MaxKeyframe)
						}
					}
				}
			}
		}
	}
}

// TestABreachIsReportedRatherThanAdaptedTo is the boundary the plan states as a
// must-not: a link below the floor is told about, and the schedule sits exactly
// at the floor rather than continuing down past it.
func TestABreachIsReportedRatherThanAdaptedTo(t *testing.T) {
	b, s := gameBounds(), stormSizes()
	floorCost := b.FloorBps(s)
	if floorCost <= 0 {
		t.Fatal("the floor priced at nothing")
	}

	c := mustController(t, b)
	p := c.Update(measured(floorCost*0.5/b.Utilisation, 0), s, Demand{})
	if !p.FloorBreached {
		t.Fatalf("a link at half the floor's cost reported no breach: %+v", p)
	}
	if !p.Constrained {
		t.Fatal("a breach was not also reported as constrained")
	}
	if got := p.KeyframePeriodTicks(); got > b.FloorKeyframeTicks {
		t.Fatalf("a breached plan went past the floor: %d ticks", got)
	}
	if p.FloorBps <= p.BudgetBps {
		t.Fatalf("a breach was reported with the floor (%.0f) inside the budget (%.0f)", p.FloorBps, p.BudgetBps)
	}

	// Exactly at the floor's cost is not a breach: the guarantee holds.
	c = mustController(t, b)
	if p := c.Update(measured(floorCost/b.Utilisation, 0), s, Demand{}); p.FloorBreached {
		t.Fatalf("a link that carries the floor exactly reported a breach: %+v", p)
	}
}

// TestDegradationIsImmediateAndRecoveryIsStepped is the hysteresis claim. A link
// that has narrowed has already narrowed; a link that looks better for one
// sample has not necessarily come back.
func TestDegradationIsImmediateAndRecoveryIsStepped(t *testing.T) {
	b, s := gameBounds(), stormSizes()
	c := mustController(t, b)
	fat := measured(10<<20, 0)
	c.Update(fat, s, Demand{})

	thin := measured(b.FloorBps(s)*1.05/b.Utilisation, 0)
	slow := c.Update(thin, s, Demand{})
	if slow.CadenceTicks <= b.NominalCadenceTicks {
		t.Fatalf("a narrowed link did not slow the cadence in one decision: %+v", slow)
	}

	back := c.Update(fat, s, Demand{})
	if back.CadenceTicks == b.NominalCadenceTicks {
		t.Fatalf("recovery jumped straight back to nominal from %d", slow.CadenceTicks)
	}
	if slow.CadenceTicks-back.CadenceTicks > b.RecoverStepTicks {
		t.Fatalf("recovery moved %d ticks in one decision, step is %d",
			slow.CadenceTicks-back.CadenceTicks, b.RecoverStepTicks)
	}
	for range 60 {
		back = c.Update(fat, s, Demand{})
	}
	if back.CadenceTicks != b.NominalCadenceTicks || back.KeyframeInterval != b.NominalKeyframe {
		t.Fatalf("a restored link never returned to nominal: %+v", back)
	}
}

// TestDemandDecidesWhereInsideTheFeasibleRangeAPeerSits is relevance and
// priority as the controller sees them: a participant whose own picture is
// drifting, or whose neighbourhood is busy, is published to faster — and one
// with neither is published to slower, which is what frees the budget for the
// first.
func TestDemandDecidesWhereInsideTheFeasibleRangeAPeerSits(t *testing.T) {
	b, s := gameBounds(), quietSizes()
	fat := measured(10<<20, 0)

	urgent := mustController(t, b).Update(fat, s, Demand{Known: true, Magnitude: b.UrgentMagnitude})
	if urgent.CadenceTicks != b.MinCadenceTicks {
		t.Fatalf("an urgent peer on a fat link got cadence %d, want the minimum %d",
			urgent.CadenceTicks, b.MinCadenceTicks)
	}
	near := mustController(t, b).Update(fat, s, Demand{Known: true, Relevance: b.UrgentRelevance})
	if near.CadenceTicks != b.MinCadenceTicks {
		t.Fatalf("a peer with a busy neighbourhood got cadence %d", near.CadenceTicks)
	}
	quiet := mustController(t, b).Update(fat, s, Demand{Known: true})
	if quiet.CadenceTicks != b.QuietCadence {
		t.Fatalf("a peer with nothing near it got cadence %d, want %d", quiet.CadenceTicks, b.QuietCadence)
	}
	ordinary := mustController(t, b).Update(fat, s, Demand{Known: true, Magnitude: 1})
	if ordinary.CadenceTicks != b.NominalCadenceTicks {
		t.Fatalf("an ordinary peer got cadence %d, want the nominal %d",
			ordinary.CadenceTicks, b.NominalCadenceTicks)
	}
}

// TestDemandCannotBuyACadenceTheLinkCannotCarry is the other half of the same
// rule: relevance decides where inside the feasible range a peer sits, and never
// widens the range.
func TestDemandCannotBuyACadenceTheLinkCannotCarry(t *testing.T) {
	b, s := gameBounds(), stormSizes()
	thin := measured(b.FloorBps(s)*1.1/b.Utilisation, 0)
	urgent := mustController(t, b).Update(thin, s, Demand{Known: true, Magnitude: 100, Relevance: 100})
	if urgent.CadenceTicks == b.MinCadenceTicks {
		t.Fatalf("demand bought the minimum cadence on a link at the floor: %+v", urgent)
	}
	if got := urgent.KeyframePeriodTicks(); got > b.FloorKeyframeTicks {
		t.Fatalf("demand pushed the plan past the floor: %d ticks", got)
	}
}

// TestJitterKeepsTheCadenceOffItsOwnNoise: publishing faster than the link's own
// timing variation only bunches corrections, so the cadence is held at or above
// twice the measured jitter.
func TestJitterKeepsTheCadenceOffItsOwnNoise(t *testing.T) {
	b, s := gameBounds(), quietSizes()
	p := mustController(t, b).Update(measured(10<<20, 200*time.Millisecond), s, Demand{Known: true, Magnitude: 100})
	want := ticksFor(b, 400*time.Millisecond)
	if p.CadenceTicks < want {
		t.Fatalf("cadence %d is faster than twice the link's %s of jitter (%d ticks)",
			p.CadenceTicks, 200*time.Millisecond, want)
	}
}

func TestAdmitRefusesALinkThatCannotCarryTheFloor(t *testing.T) {
	b, s := gameBounds(), stormSizes()
	need := b.FloorBps(s)

	if err := Admit(b, need/b.Utilisation, s); err != nil {
		t.Fatalf("a link that exactly carries the floor was refused: %v", err)
	}
	err := Admit(b, need*0.4/b.Utilisation, s)
	if err == nil {
		t.Fatal("a link at 40%% of the floor was admitted")
	}
	var fe *FloorError
	if !errors.As(err, &fe) {
		t.Fatalf("refusal was not a FloorError: %T", err)
	}
	if fe.KeyframeBytes != s.Keyframe || fe.Window != b.FloorWindow() {
		t.Fatalf("refusal did not carry what it was measured against: %+v", fe)
	}
	if fe.Error() == "" {
		t.Fatal("refusal has no message")
	}

	// No measurement is not a refusal: a session nobody can join until a probe
	// has completed a round trip is worse than one that reports the condition.
	if err := Admit(b, 0, s); err != nil {
		t.Fatalf("admission was refused on no evidence: %v", err)
	}
	if err := AdmitMetrics(b, Metrics{Throughput: 1, Ready: false}, s); err != nil {
		t.Fatalf("admission was refused on an unready link: %v", err)
	}
	if err := AdmitMetrics(b, Metrics{Throughput: 1, Ready: true, Saturated: false}, s); err != nil {
		t.Fatalf("admission was refused on an idle sender's rate: %v", err)
	}
	if err := AdmitMetrics(b, measured(need*0.1/b.Utilisation, 0), s); err == nil {
		t.Fatal("a measured link a tenth of the floor was admitted")
	}
}

// TestTheFloorIsPricedAtTheCheapestScheduleThatHonoursIt pins what "the link
// cannot sustain the floor" is actually measured against: not a keyframe alone,
// but the cheapest legal schedule that delivers one per window — which on a
// bounded cadence includes the deltas the bounds force in beside it.
func TestTheFloorIsPricedAtTheCheapestScheduleThatHonoursIt(t *testing.T) {
	b, s := gameBounds(), stormSizes()
	cheapest := cheapestFloorPlan(b)
	if got := cheapest.CadenceTicks * uint64(cheapest.KeyframeInterval); got > b.FloorKeyframeTicks {
		t.Fatalf("the cheapest floor schedule itself breaks the floor: %d ticks", got)
	}
	if got, want := b.FloorBps(s), rate(b, cheapest.CadenceTicks, cheapest.KeyframeInterval, s); got != want {
		t.Fatalf("floor priced at %.0f B/s, cheapest schedule costs %.0f", got, want)
	}
	for c := b.MinCadenceTicks; c <= b.MaxCadenceTicks; c++ {
		for k := b.MinKeyframe; k <= b.MaxKeyframe; k++ {
			if c*uint64(k) > b.FloorKeyframeTicks {
				continue
			}
			if r := rate(b, c, k, s); r < b.FloorBps(s) {
				t.Fatalf("schedule %d/%d costs %.0f B/s, below the quoted floor of %.0f",
					c, k, r, b.FloorBps(s))
			}
		}
	}
}

// TestPlansAreReproducible: the same measurements in the same order always
// produce the same operating point, which is what makes a shaped-link run
// diagnosable rather than anecdotal.
func TestPlansAreReproducible(t *testing.T) {
	run := func() []Plan {
		c := mustController(t, gameBounds())
		var out []Plan
		for i := range 30 {
			m := measured(float64(20_000+i*3_000), time.Duration(i)*time.Millisecond)
			out = append(out, c.Update(m, stormSizes(), Demand{Known: true, Magnitude: i % 12, Relevance: i % 5}))
		}
		return out
	}
	a, b := run(), run()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("decision %d differed: %+v then %+v", i, a[i], b[i])
		}
	}
}
