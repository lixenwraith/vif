package engine

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/status"
)

func newTestControl() *TimeControl {
	return NewTimeControl(NewManualClock(), status.NewRegistry())
}

// drainWake empties the wake channel and reports the pending signal count
func drainWake(tc *TimeControl) int {
	n := 0
	for {
		select {
		case <-tc.Wake():
			n++
		default:
			return n
		}
	}
}

func TestTimeControlDefaults(t *testing.T) {
	tc := newTestControl()
	if tc.Scale() != ScaleNormal {
		t.Fatalf("scale %v", tc.Scale())
	}
	if tc.IsPaused() {
		t.Fatal("fresh control paused")
	}
	if tc.Armed() != nil {
		t.Fatal("fresh control armed")
	}
	if n := drainWake(tc); n != 0 {
		t.Fatalf("construction signalled wake %d times", n)
	}
}

func TestTimeControlScale(t *testing.T) {
	tc := newTestControl()
	for _, s := range ScaleLadder {
		drainWake(tc)
		tc.SetScale(s)
		if tc.Scale() != s {
			t.Fatalf("scale %v, want %v", tc.Scale(), s)
		}
		if n := drainWake(tc); n != 1 {
			t.Fatalf("rate change signalled %d times, want 1", n)
		}
	}
	cur := tc.Scale()
	tc.SetScale(TimeScale{Num: 0, Den: 1})
	if tc.Scale() != cur {
		t.Fatal("invalid rate applied")
	}
}

func TestTimeControlPause(t *testing.T) {
	tc := newTestControl()
	drainWake(tc)

	if !tc.SetPaused(true) {
		t.Fatal("first pause reported no change")
	}
	if !tc.IsPaused() {
		t.Fatal("pause not observable")
	}
	if n := drainWake(tc); n != 1 {
		t.Fatalf("pause signalled %d times, want 1", n)
	}
	if tc.SetPaused(true) {
		t.Fatal("redundant pause reported a change")
	}
	if n := drainWake(tc); n != 0 {
		t.Fatalf("redundant pause signalled %d times", n)
	}
	if !tc.SetPaused(false) || tc.IsPaused() {
		t.Fatal("resume failed")
	}
}

// TestTimeControlManualPassthrough pins headless time semantics
func TestTimeControlManualPassthrough(t *testing.T) {
	tc := NewTimeControl(NewManualClock(), status.NewRegistry())
	tc.Step(50 * time.Millisecond)
	if !tc.Now().Equal(ManualEpoch.Add(50 * time.Millisecond)) {
		t.Fatalf("now %v", tc.Now())
	}
	if !tc.RealTime().Equal(tc.Now()) {
		t.Fatal("manual RealTime diverges from Now")
	}
	if tc.ToReal(time.Hour) != 0 {
		t.Fatal("manual run reported a wall interval")
	}
	// Pause is TimeControl state; an explicit Step still advances manual time
	tc.SetPaused(true)
	tc.Step(50 * time.Millisecond)
	if !tc.Now().Equal(ManualEpoch.Add(100 * time.Millisecond)) {
		t.Fatal("paused manual clock refused an explicit step")
	}
}

func TestTimeControlStepBudget(t *testing.T) {
	tc := newTestControl()
	tc.StepTicks(3)
	for i := range 3 {
		if !tc.TakeStep() {
			t.Fatalf("step %d denied", i)
		}
	}
	if tc.TakeStep() {
		t.Fatal("budget exceeded")
	}
	tc.StepTicks(0) // clamps to 1
	if !tc.TakeStep() || tc.TakeStep() {
		t.Fatal("zero request did not clamp to one tick")
	}
}

func TestTimeControlBreakFSM(t *testing.T) {
	tc := newTestControl()
	slow := TimeScale{Num: 1, Den: 4}
	tc.SetScale(slow)

	bs := &BreakState{Mode: StepFSM, Restore: slow, Label: "fsm:any"}
	tc.Arm(bs, ScaleNormal)
	if tc.Armed() != bs {
		t.Fatal("arm did not publish")
	}
	if tc.Scale() != ScaleNormal {
		t.Fatal("run rate not applied")
	}
	if tc.Trip(StepEvent, "", event.EventType(1)) != nil {
		t.Fatal("wrong mode tripped")
	}
	if got := tc.Trip(StepFSM, "spawn", event.EventNone); got != bs {
		t.Fatal("wildcard region did not match")
	}
	if tc.Scale() != slow {
		t.Fatal("restore rate not reinstated")
	}
	if tc.Armed() != nil {
		t.Fatal("trip did not disarm")
	}
	if tc.Trip(StepFSM, "spawn", event.EventNone) != nil {
		t.Fatal("tripped twice")
	}
}

func TestTimeControlBreakFilters(t *testing.T) {
	tc := newTestControl()

	region := &BreakState{Mode: StepFSM, Region: "combat", Restore: ScaleNormal}
	tc.Arm(region, ScaleNormal)
	if tc.Trip(StepFSM, "spawn", event.EventNone) != nil {
		t.Fatal("foreign region tripped")
	}
	if tc.Trip(StepFSM, "combat", event.EventNone) != region {
		t.Fatal("named region did not match")
	}

	want := event.EventType(7)
	ev := &BreakState{Mode: StepEvent, Event: want, Restore: ScaleNormal}
	tc.Arm(ev, ScaleNormal)
	if tc.Trip(StepEvent, "", event.EventType(8)) != nil {
		t.Fatal("foreign event tripped")
	}
	if tc.Trip(StepEvent, "", want) != ev {
		t.Fatal("named event did not match")
	}
}

func TestTimeControlExpire(t *testing.T) {
	tc := newTestControl()

	bs := &BreakState{Mode: StepFSM, Restore: ScaleNormal, Expiry: 100}
	tc.Arm(bs, TimeScale{Num: 8, Den: 1})
	if tc.Expire(99) != nil {
		t.Fatal("expired early")
	}
	if tc.Expire(100) != bs {
		t.Fatal("did not expire at deadline")
	}
	if tc.Scale() != ScaleNormal {
		t.Fatal("expiry did not restore the rate")
	}

	open := &BreakState{Mode: StepFSM, Restore: ScaleNormal}
	tc.Arm(open, ScaleNormal)
	if tc.Expire(1<<40) != nil {
		t.Fatal("zero expiry expired")
	}
}

func TestTimeControlDisarmPaths(t *testing.T) {
	slow := TimeScale{Num: 1, Den: 8}

	tc := newTestControl()
	tc.SetScale(slow)
	tc.Arm(&BreakState{Mode: StepFSM, Restore: slow}, ScaleNormal)
	tc.SetScale(TimeScale{Num: 1, Den: 2})
	if tc.Armed() != nil {
		t.Fatal("explicit rate did not disarm")
	}
	if tc.Scale() != (TimeScale{Num: 1, Den: 2}) {
		t.Fatal("explicit rate was overwritten by a restore")
	}

	tc = newTestControl()
	tc.SetScale(slow)
	tc.Arm(&BreakState{Mode: StepFSM, Restore: slow}, ScaleNormal)
	tc.StepTicks(4)
	if tc.Armed() != nil {
		t.Fatal("step request did not disarm")
	}

	tc = newTestControl()
	tc.SetScale(slow)
	tc.Arm(&BreakState{Mode: StepFSM, Restore: slow}, ScaleNormal)
	tc.StepTicks(4)
	tc.CancelBreak()
	if tc.TakeStep() {
		t.Fatal("cancel left a step allowance")
	}
}

// TestTimeControlImplicitDisarmRestoresRate pins the rule that a run rate
// installed by Arm does not outlive the break that installed it.
func TestTimeControlImplicitDisarmRestoresRate(t *testing.T) {
	slow := TimeScale{Num: 1, Den: 8}

	for _, tc := range []struct {
		name  string
		abort func(*TimeControl)
	}{
		{"step", func(c *TimeControl) { c.StepTicks(4) }},
		{"cancel", func(c *TimeControl) { c.CancelBreak() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestControl()
			c.SetScale(slow)
			c.Arm(&BreakState{Mode: StepFSM, Restore: slow}, ScaleNormal)
			tc.abort(c)
			if c.Scale() != slow {
				t.Fatalf("scale %v after abort, want %v", c.Scale(), slow)
			}
		})
	}
}
