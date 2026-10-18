//go:build !vif_headless && !vif_noaudio

package parameter

import (
	"testing"
	"time"

	"github.com/lixenwraith/vif/pkg/audio"
)

// TestTierTrackerHoldsAndSteps is the hysteresis rule: a burst waits out the rise
// hold and then reaches its tier in one move, a dip inside the margin moves nothing,
// and a lull steps down one tier per fall hold.
func TestTierTrackerHoldsAndSteps(t *testing.T) {
	var tr TierTracker
	tick := 50 * time.Millisecond
	run := func(apm uint64, d time.Duration) (moves int) {
		for ; d > 0; d -= tick {
			if tr.Observe(apm, tick) {
				moves++
			}
		}
		return moves
	}
	if n := run(TierPeakAPM, TierRiseHold-tick); n != 0 || tr.Tier != audio.IntensityCalm {
		t.Fatalf("rose to %v within the rise hold", tr.Tier)
	}
	if n := run(TierPeakAPM, 2*tick); n != 1 || tr.Tier != audio.IntensityPeak {
		t.Fatalf("after the rise hold: %d moves to %v, want one to peak", n, tr.Tier)
	}
	if n := run(TierPeakAPM-TierFallMargin, 2*TierFallHold); n != 0 {
		t.Fatalf("a dip inside the margin moved the tier %d times", n)
	}
	if n := run(0, TierFallHold-tick); n != 0 {
		t.Fatalf("fell within the fall hold")
	}
	if n := run(0, 2*tick); n != 1 || tr.Tier != audio.IntensityIntense {
		t.Fatalf("after the fall hold: %d moves to %v, want one step to intense", n, tr.Tier)
	}
}
