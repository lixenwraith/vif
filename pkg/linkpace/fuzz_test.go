package linkpace

import (
	"testing"
	"time"
)

// FuzzControllerHoldsItsEnvelope drives the controller from arbitrary
// measurements and asserts the four properties that are not allowed to depend on
// the input: the plan stays inside its declared bounds, it never leaves more
// than the floor between whole worlds, a breach is reported rather than
// silently adapted past, and a plan is never zero-valued.
//
// This is the guard that matters most, because the interesting inputs are the
// ones nobody writes a case for: a link measured at a nanosecond, a keyframe
// larger than the address space allows, jitter longer than the floor window.
func FuzzControllerHoldsItsEnvelope(f *testing.F) {
	f.Add(uint64(215_000), int64(175_908), int64(29_488), int64(60), 8, 4, true, true)
	f.Add(uint64(0), int64(0), int64(0), int64(0), 0, 0, false, false)
	f.Add(uint64(1), int64(1<<40), int64(1<<40), int64(1<<40), 1<<20, 1<<20, true, true)

	f.Fuzz(func(t *testing.T, throughput uint64, keyframe, delta, jitterMs int64,
		magnitude, relevance int, saturated, known bool) {
		b := gameBounds()
		c, err := NewController(b)
		if err != nil {
			t.Fatalf("controller: %v", err)
		}
		if jitterMs < 0 {
			jitterMs = -jitterMs
		}
		if keyframe < 0 {
			keyframe = -keyframe
		}
		if delta < 0 {
			delta = -delta
		}
		if magnitude < 0 {
			magnitude = -magnitude
		}
		if relevance < 0 {
			relevance = -relevance
		}
		m := Metrics{
			RTT:        50 * time.Millisecond,
			MinRTT:     50 * time.Millisecond,
			Jitter:     time.Duration(jitterMs%3_600_000) * time.Millisecond,
			Throughput: float64(throughput),
			Saturated:  saturated,
			Samples:    9,
			Ready:      true,
		}
		s := Sizes{Keyframe: keyframe, Delta: delta}
		d := Demand{Known: known, Magnitude: magnitude, Relevance: relevance}

		for range 12 {
			p := c.Update(m, s, d)
			switch {
			case p.CadenceTicks < b.MinCadenceTicks || p.CadenceTicks > b.MaxCadenceTicks:
				t.Fatalf("cadence %d outside [%d,%d]", p.CadenceTicks, b.MinCadenceTicks, b.MaxCadenceTicks)
			case p.KeyframeInterval < b.MinKeyframe || p.KeyframeInterval > b.MaxKeyframe:
				t.Fatalf("keyframe interval %d outside [%d,%d]", p.KeyframeInterval, b.MinKeyframe, b.MaxKeyframe)
			case p.KeyframePeriodTicks() > b.FloorKeyframeTicks:
				t.Fatalf("%d ticks between whole worlds, floor is %d",
					p.KeyframePeriodTicks(), b.FloorKeyframeTicks)
			case p.FloorBreached && !p.Constrained:
				t.Fatal("a breached link was not also reported as constrained")
			}
		}
	})
}
