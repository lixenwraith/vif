package engine

import (
	"testing"
	"time"
)

// TestSimTimeIsAFunctionOfTheTick pins the property the shared simulation depends
// on: the instant a tick reads is decided by the tick number alone, so two
// participants — and a replay — read the same value without agreeing on anything.
func TestSimTimeIsAFunctionOfTheTick(t *testing.T) {
	const interval = 50 * time.Millisecond

	if got := SimTime(0, interval); !got.Equal(SimEpoch) {
		t.Fatalf("tick 0 is not the epoch: got %v want %v", got, SimEpoch)
	}

	// Two instances that started at different wall instants still agree, because
	// neither reads a wall clock to answer.
	for _, tick := range []uint64{1, 7, 744, 1914, 100000} {
		a := SimTime(tick, interval)
		b := SimTime(tick, interval)
		if !a.Equal(b) {
			t.Fatalf("tick %d not reproducible: %v vs %v", tick, a, b)
		}
		if want := SimEpoch.Add(time.Duration(tick) * interval); !a.Equal(want) {
			t.Fatalf("tick %d: got %v want %v", tick, a, want)
		}
	}
}

// TestSimTimeAdvancesByExactlyTheTickInterval is the DeltaTime agreement. A
// shared reader that compares now.Sub(stored) against a duration must cross the
// threshold on one definite tick; that is only true if the stamp advances by the
// same delta the systems are told they ran for.
func TestSimTimeAdvancesByExactlyTheTickInterval(t *testing.T) {
	const interval = 50 * time.Millisecond

	for tick := uint64(1); tick <= 64; tick++ {
		step := SimTime(tick, interval).Sub(SimTime(tick-1, interval))
		if step != interval {
			t.Fatalf("tick %d advanced by %v, want %v", tick, step, interval)
		}
	}

	// The quasar's speed step is the reader that broke: a 20-tick threshold must
	// fire on tick 20 and not on tick 19, whatever the wall clock did in between.
	const speedInterval = 20 * interval
	start := SimTime(3, interval)
	if SimTime(3+19, interval).Sub(start) >= speedInterval {
		t.Fatal("threshold crossed a tick early")
	}
	if SimTime(3+20, interval).Sub(start) != speedInterval {
		t.Fatal("threshold did not land exactly on its tick")
	}
}

// TestManualEpochIsTheSimEpoch keeps the replay clock and the tick-derived stamp
// on one origin, so a manual run's Now and its tick's SimTime are the same instant
// rather than two conventions that happen to both be constant.
func TestManualEpochIsTheSimEpoch(t *testing.T) {
	if !ManualEpoch.Equal(SimEpoch) {
		t.Fatalf("ManualEpoch %v != SimEpoch %v", ManualEpoch, SimEpoch)
	}

	mc := NewManualClock()
	const interval = 50 * time.Millisecond
	for tick := uint64(1); tick <= 10; tick++ {
		mc.Step(interval)
		if got, want := mc.Now(), SimTime(tick, interval); !got.Equal(want) {
			t.Fatalf("tick %d: manual clock %v, SimTime %v", tick, got, want)
		}
	}
}
