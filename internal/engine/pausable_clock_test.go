package engine

import (
	"testing"
	"time"
)

// TestScaleLadder pins token round-trips and clamping
func TestScaleLadder(t *testing.T) {
	if ScaleLadder[ScaleNormalIndex] != ScaleNormal {
		t.Fatal("ScaleNormalIndex does not locate ScaleNormal")
	}
	for _, s := range ScaleLadder {
		if !s.Valid() {
			t.Fatalf("ladder entry %v invalid", s)
		}
		got, ok := ParseScale(s.String())
		if !ok || got != s {
			t.Fatalf("round-trip %q: %v %v", s.String(), got, ok)
		}
	}
	if _, ok := ParseScale("3/7"); ok {
		t.Fatal("off-ladder token accepted")
	}
	last := len(ScaleLadder) - 1
	if ScaleStep(ScaleLadder[0], -3) != ScaleLadder[0] {
		t.Fatal("lower clamp failed")
	}
	if ScaleStep(ScaleLadder[last], 3) != ScaleLadder[last] {
		t.Fatal("upper clamp failed")
	}
	if ScaleStep(TimeScale{Num: 3, Den: 7}, 0) != ScaleNormal {
		t.Fatal("off-ladder rate did not resolve to real time")
	}
	if p := (TimeScale{Num: 1, Den: 4}).Percent(); p != 25 {
		t.Fatalf("percent %d, want 25", p)
	}
}

// TestPausableClockStep asserts Step is exact while frozen and inert while running
func TestPausableClockStep(t *testing.T) {
	pc := NewPausableClock()

	before := pc.Now()
	pc.Step(time.Second)
	if d := pc.Now().Sub(before); d > 100*time.Millisecond {
		t.Fatalf("running clock advanced %v: Step is not gated on pause", d)
	}

	pc.Pause()
	t0 := pc.Now()
	pc.Step(50 * time.Millisecond)
	if d := pc.Now().Sub(t0); d != 50*time.Millisecond {
		t.Fatalf("step delta %v, want 50ms", d)
	}
}

// TestPausableClockPauseFreezes covers idempotence and frozen reads
func TestPausableClockPauseFreezes(t *testing.T) {
	pc := NewPausableClock()
	if pc.IsPaused() {
		t.Fatal("fresh clock paused")
	}
	pc.Pause()
	pc.Pause()
	if !pc.IsPaused() {
		t.Fatal("pause not latched")
	}
	t0 := pc.Now()
	time.Sleep(20 * time.Millisecond)
	if !pc.Now().Equal(t0) {
		t.Fatal("paused clock advanced")
	}
	pc.Resume()
	pc.Resume()
	if pc.IsPaused() {
		t.Fatal("resume not latched")
	}
}

// TestPausableClockToReal converts by the active rate; paused yields no interval
func TestPausableClockToReal(t *testing.T) {
	pc := NewPausableClock()
	if d := pc.ToReal(time.Second); d != time.Second {
		t.Fatalf("1x: %v", d)
	}
	pc.SetScale(TimeScale{Num: 1, Den: 4})
	if d := pc.ToReal(time.Second); d != 4*time.Second {
		t.Fatalf("1/4x: %v, want 4s", d)
	}
	pc.SetScale(TimeScale{Num: 4, Den: 1})
	if d := pc.ToReal(time.Second); d != 250*time.Millisecond {
		t.Fatalf("4x: %v, want 250ms", d)
	}
	pc.Pause()
	if d := pc.ToReal(time.Second); d != 0 {
		t.Fatalf("paused: %v, want 0", d)
	}
}
