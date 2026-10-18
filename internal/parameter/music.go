//go:build !vif_headless && !vif_noaudio

package parameter

import (
	"math"
	"time"

	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/pkg/audio"
)

// Conductor policy: APM → tempo and arrangement tier. Everything here is game
// interpretation of the music engine; the engine carries no APM concept.

// BuiltinPatterns parses the shipped music bank, whose roles, groups and tiers are the
// drawn arrangement; any pattern it drops is a broken build.
func BuiltinPatterns() ([]*audio.Pattern, error) {
	return audio.LoadPatternsTOML(asset.DefaultMusic)
}

// APMToBPM maps burst APM to target tempo on a square root, RestBPM + k·√APM with k
// placing audio.MaxBPM at the admission ceiling: the first actions move it most, and
// the last BPM ask for sustained, varied input (half the range at a quarter of the
// ceiling, the top 10% only above 81% of it). RestBPM must stay >= audio.MinBPM.
func APMToBPM(apm uint64) int {
	ceiling := float64(60 * APMMaxPerSecond)
	f := math.Sqrt(min(float64(apm), ceiling) / ceiling)
	return RestBPM + int(math.Round(float64(audio.MaxBPM-RestBPM)*f))
}

// RestBPM is the tempo of a player not acting
const RestBPM = 100

// TierForAPM maps burst APM to an arrangement tier
func TierForAPM(apm uint64) audio.Intensity {
	switch {
	case apm < TierNormalAPM:
		return audio.IntensityCalm
	case apm < TierElevatedAPM:
		return audio.IntensityNormal
	case apm < TierIntenseAPM:
		return audio.IntensityElevated
	case apm < TierPeakAPM:
		return audio.IntensityIntense
	default:
		return audio.IntensityPeak
	}
}

// Tempo dynamics: the conductor slews toward the APM target and the sequencer
// applies it beat-quantized. The fall is half the rise, so a lull winds the tempo
// down over the bars the tiers take to step down rather than ahead of them.
const (
	BPMHysteresis = 3    // ignore smaller deltas
	BPMRiseRate   = 12.0 // BPM per second, upward
	BPMFallRate   = 6.0  // BPM per second, downward
)

// Tier hysteresis. A tier holds TierRiseHold after any change before rising, then
// goes straight to the APM's tier; it falls one tier at a time, each after the APM has
// sat TierFallMargin under the tier's floor for TierFallHold. A burst reaches its
// tier in one or two swaps instead of one a bar, and a lull steps down a phrase at a
// time instead of collapsing within the five-second window.
const (
	TierRiseHold   = 4 * time.Second
	TierFallHold   = 5 * time.Second
	TierFallMargin = 30
)

// TierTracker is the conductor's tier under hysteresis
type TierTracker struct {
	Tier  audio.Intensity
	held  time.Duration // since the last change
	under time.Duration // the APM has sat under the tier's fall threshold this long
}

// Set places the tier, as a start or a manual hold does, and restarts its holds
func (t *TierTracker) Set(tier audio.Intensity) { t.Tier, t.held, t.under = tier, 0, 0 }

// Observe advances the holds by dt and reports whether the tier moved
func (t *TierTracker) Observe(apm uint64, dt time.Duration) bool {
	t.held += dt
	if TierForAPM(apm+TierFallMargin) < t.Tier {
		t.under += dt
	} else {
		t.under = 0
	}
	switch {
	case TierForAPM(apm) > t.Tier && t.held >= TierRiseHold:
		t.Tier = TierForAPM(apm)
	case t.under >= TierFallHold && t.Tier > audio.IntensityCalm:
		t.Tier--
	default:
		return false
	}
	t.held, t.under = 0, 0
	return true
}

// Arrangement transition presets
// Slow must span >= 1 bar at >= 120 BPM to engage the sequencer track reveal;
// Default is deliberately below that — falling tiers swap without a build-up
const (
	PatternTransitionDefault = 250 * time.Millisecond
	PatternTransitionRise    = 400 * time.Millisecond
)

// DefaultRootNote is the harmony root the conductor requests (E2)
// Distinct from audio.DefaultRootNote, which is the engine's construction-time
// default; identical value, independent policy
const DefaultRootNote = 40
