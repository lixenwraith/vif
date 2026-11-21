//go:build !vif_headless && !vif_noaudio

package parameter

import (
	"math"
	"testing"
	"time"

	"github.com/lixenwraith/audio"
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

// TestADynamicBeatIsLockedToTheTempo is the dynamic mapping: eight beats of the bed
// to one of the music, so it rises with the conductor's tempo from 13.3 Hz at rest
// to 26.7 at the tempo's ceiling, inside what the engine's bed plays.
func TestADynamicBeatIsLockedToTheTempo(t *testing.T) {
	rest, top := BinauralBeat(RestBPM), BinauralBeat(audio.MaxBPM)
	if math.Abs(rest-13.333) > 0.001 || math.Abs(top-26.667) > 0.001 || top > audio.MaxBinauralBeat {
		t.Fatalf("a dynamic bed beats at %.3f Hz at rest and %.3f at the ceiling, want 13.333 and 26.667 within the engine's %g", rest, top, audio.MaxBinauralBeat)
	}
	for bpm := float64(RestBPM); bpm < audio.MaxBPM; bpm++ {
		if got := BinauralBeat(bpm+1) / BinauralBeat(bpm); math.Abs(got-(bpm+1)/bpm) > 1e-12 {
			t.Fatalf("from %g to %g BPM the beat grows by %g, the tempo by %g", bpm, bpm+1, got, (bpm+1)/bpm)
		}
	}
}

// TestABedIsWhatTheEnginePlays: every preset's beat and every key's carrier, the
// root's octave from BinauralCarrierFloor up, E3 for the conductor's E, lie within
// the beats and carriers the engine's bed takes, so none is clamped.
func TestABedIsWhatTheEnginePlays(t *testing.T) {
	for _, p := range BinauralPresets {
		if p.Beat <= 0 || p.Beat > audio.MaxBinauralBeat {
			t.Errorf("preset %s beats at %g Hz, outside the engine's %g", p.Name, p.Beat, audio.MaxBinauralBeat)
		}
	}
	for root := range 128 {
		hz := BinauralCarrier(root)
		if octaves := math.Log2(hz / audio.NoteFreq(root)); hz < BinauralCarrierFloor || hz >= 2*BinauralCarrierFloor ||
			octaves != math.Round(octaves) || hz < audio.MinBinauralCarrier || hz > audio.MaxBinauralCarrier {
			t.Errorf("root %d carries the bed at %g Hz, %g octaves from it: want an octave of it from %g Hz up", root, hz, octaves, BinauralCarrierFloor)
		}
	}
	if hz := BinauralCarrier(DefaultRootNote); math.Abs(hz-164.814) > 0.001 || BinauralLevel <= 0 || BinauralLevel > 1 {
		t.Errorf("E2 carries the bed at %g Hz at a level of %g, want E3's 164.814 at a level within full scale", hz, BinauralLevel)
	}
}
