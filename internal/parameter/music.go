//go:build !vif_headless && !vif_noaudio && !wasm

package parameter

import (
	"math"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/asset"
	"github.com/lixenwraith/vi-fighter/pkg/audio"
)

// Conductor policy: APM → tempo and arrangement tier. Everything here is game
// interpretation of the music engine; the engine carries no APM concept.

// BuiltinPatterns parses the shipped music bank, whose roles, groups and tiers are the
// drawn arrangement; any pattern it drops is a broken build.
func BuiltinPatterns() ([]*audio.Pattern, error) {
	return audio.LoadPatternsTOML(asset.DefaultMusic)
}

// Tier thresholds (MusicAPM: 5s burst normalized to per-minute)
const (
	TierNormalAPM   = 60
	TierElevatedAPM = 140
	TierIntenseAPM  = 220
	TierPeakAPM     = 300
)

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
// applies it beat-quantized, so tempo trails the five-second window by about a second
const (
	BPMHysteresis = 3    // ignore smaller deltas
	BPMRiseRate   = 20.0 // BPM per second, upward
	BPMFallRate   = 16.0 // BPM per second, downward
)

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
