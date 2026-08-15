package engine

import (
	"testing"
	"time"
)

// TestManualClockPurity asserts manual time is a pure function of Step calls
func TestManualClockPurity(t *testing.T) {
	mc := NewManualClock()

	if got := mc.Now(); !got.Equal(ManualEpoch) {
		t.Fatalf("fresh clock at %v, want %v", got, ManualEpoch)
	}
	if mc.Elapsed() != 0 {
		t.Fatalf("fresh elapsed %v, want 0", mc.Elapsed())
	}
	if !mc.RealTime().Equal(mc.Now()) {
		t.Fatal("RealTime diverges from Now")
	}
	if d := mc.ToReal(time.Second); d != 0 {
		t.Fatalf("ToReal %v, want 0", d)
	}

	const step = 50 * time.Millisecond
	for range 1000 {
		mc.Step(step)
	}
	want := ManualEpoch.Add(1000 * step)
	if got := mc.Now(); !got.Equal(want) {
		t.Fatalf("after 1000 steps at %v, want %v", got, want)
	}
	if !mc.RealTime().Equal(want) {
		t.Fatal("RealTime diverges from Now after stepping")
	}
}

// TestManualClockRejectsNonPositiveStep keeps time monotonic
func TestManualClockRejectsNonPositiveStep(t *testing.T) {
	mc := NewManualClock()
	mc.Step(0)
	mc.Step(-time.Second)
	if mc.Elapsed() != 0 {
		t.Fatalf("elapsed %v after non-positive steps, want 0", mc.Elapsed())
	}
}

// TestManualClockPauseIsInert confirms the backend holds no pause state
func TestManualClockPauseIsInert(t *testing.T) {
	mc := NewManualClock()
	mc.Step(time.Second)
	mc.Pause()
	mc.Step(time.Second)
	mc.Resume()
	mc.Step(time.Second)
	if got := mc.Elapsed(); got != 3*time.Second {
		t.Fatalf("elapsed %v, want 3s", got)
	}
}

// TestManualClockScaleRecordedNotApplied pins the telemetry-only contract
func TestManualClockScaleRecordedNotApplied(t *testing.T) {
	mc := NewManualClock()
	s := TimeScale{Num: 1, Den: 8}
	mc.SetScale(s)
	if mc.Scale() != s {
		t.Fatalf("scale %v, want %v", mc.Scale(), s)
	}
	mc.Step(time.Second)
	if mc.Elapsed() != time.Second {
		t.Fatalf("elapsed %v, want 1s: scale must not dilate manual time", mc.Elapsed())
	}
	mc.SetScale(TimeScale{})
	if mc.Scale() != s {
		t.Fatal("invalid scale applied")
	}
}

// TestManualClockReproducible asserts no wall input reaches manual time
func TestManualClockReproducible(t *testing.T) {
	a, b := NewManualClock(), NewManualClock()
	for i := range 500 {
		d := time.Duration(i%7+1) * time.Millisecond
		a.Step(d)
		b.Step(d)
		if !a.Now().Equal(b.Now()) {
			t.Fatalf("diverged at step %d: %v vs %v", i, a.Now(), b.Now())
		}
	}
}
