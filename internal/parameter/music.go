//go:build !vif_headless && !vif_noaudio && !wasm

package parameter

import (
	"time"

	"github.com/lixenwraith/vi-fighter/internal/asset"
	"github.com/lixenwraith/vi-fighter/pkg/audio"
)

// Conductor policy: APM → tempo and arrangement tier. Everything here is game
// interpretation of the music engine; the engine carries no APM concept.

// BuiltinPatterns parses the shipped music bank; any pattern it drops is a broken build.
func BuiltinPatterns() ([]*audio.Pattern, error) {
	return audio.LoadPatternsTOML(asset.DefaultMusic)
}

// TierArrangements names the pools each tier draws its rhythm and melody from,
// all in the shipped bank or code-registered (melody_gen). beat_breakdown is left
// out on purpose: it is a drop for a scripted moment, not a groove to hold.
var TierArrangements = [audio.IntensityCount]audio.Arrangement{
	audio.IntensityCalm: {
		Rhythm: []string{"beat_basic", "beat_pulse", "beat_minimal", "beat_halftime"},
		Melody: []string{"melody_bassline", "melody_drone", "melody_pulse_bass", "melody_pad_bells"},
	},
	audio.IntensityNormal: {
		Rhythm: []string{"beat_driving", "beat_shuffle", "beat_offbeat", "beat_rolling", "beat_tribal"},
		Melody: []string{"melody_bassline", "melody_octave_bass", "melody_call", "melody_pulse_bass"},
	},
	audio.IntensityElevated: {
		Rhythm: []string{"beat_driving_plus", "beat_breaks", "beat_electro", "beat_stomp", "beat_syncopated"},
		Melody: []string{"melody_bass_arp", "melody_bass_arp_down", "melody_arp_updown", "melody_stabs"},
	},
	audio.IntensityIntense: {
		Rhythm: []string{"beat_intense", "beat_gallop", "beat_double", "beat_storm"},
		Melody: []string{"melody_bass_arp", "melody_arp_fast", "melody_acid", "melody_full"},
	},
	audio.IntensityPeak: {
		Rhythm: []string{"beat_intense", "beat_hammer", "beat_frenzy", "beat_storm"},
		Melody: []string{"melody_gen", "melody_lead_pad", "melody_acid", "melody_arp_fast"},
	},
}

// Tier thresholds (MusicAPM: 5s burst normalized to per-minute)
const (
	TierNormalAPM   = 60
	TierElevatedAPM = 140
	TierIntenseAPM  = 220
	TierPeakAPM     = 300
)

// APMToBPM maps burst APM to target tempo, with breakpoints on the tier thresholds:
// calm holds the floor, normal through elevated climbs to the knee at TierIntenseAPM,
// and peak tempo arrives at TierPeakAPM. The calm floor must stay >= audio.MinBPM.
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
