package parameter

import (
	"time"

	"github.com/lixenwraith/vi-fighter/audio"
)

// Conductor policy: APM → tempo and arrangement tier. Everything here is game
// interpretation of the music engine; the engine carries no APM concept.

// Tier thresholds (MusicAPM: 5s burst normalized to per-minute)
const (
	TierNormalAPM   = 60
	TierElevatedAPM = 140
	TierIntenseAPM  = 220
	TierPeakAPM     = 300
)

// TODO: move logic to music system, keep parameters only

// APMToBPM maps burst APM to target tempo; the sequencer re-clamps to
// [audio.MinBPM, audio.MaxBPM]. The calm floor must stay >= audio.MinBPM.
//
// breakpoints are tied to the tier thresholds instead of literals (60 / 120 / 180).
// The mid-range knee moves from APM 120 to TierIntenseAPM (220),
// so tempo rises more slowly through normal play and peak tempo is only reached at TierPeakAPM.
// To keep the original curve, substitute: <=60 → 100; <=120 → 100 + (apm-60)*40/60; else 140 + (apm-120)*40/60.
func APMToBPM(apm uint64) int {
	const (
		calmBPM   = 100
		normalBPM = 140
		peakBPM   = audio.MaxBPM
	)
	switch {
	case apm <= TierNormalAPM:
		return calmBPM
	case apm <= TierIntenseAPM:
		return calmBPM + int(uint64(normalBPM-calmBPM)*(apm-TierNormalAPM)/(TierIntenseAPM-TierNormalAPM))
	case apm <= TierPeakAPM:
		return normalBPM + int(uint64(peakBPM-normalBPM)*(apm-TierIntenseAPM)/(TierPeakAPM-TierIntenseAPM))
	default:
		return peakBPM
	}
}

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

// Tempo dynamics: the conductor slews toward the APM target, the sequencer
// applies the result bar-quantized
const (
	BPMHysteresis = 3    // ignore smaller deltas
	BPMRiseRate   = 8.0  // BPM per second, upward
	BPMFallRate   = 10.0 // BPM per second, downward
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
