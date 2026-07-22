package audio

import "time"

// Engine constants. The audio package is standalone: every value the mixer,
// sequencer and synthesis path reads lives here. Game policy — APM mapping,
// arrangement tiers, per-sound mix levels — lives in parameter.

// --- Hardware / stream format ---
const (
	AudioSampleRate    = 44100
	AudioChannels      = 2
	AudioBitDepth      = 16
	AudioBytesPerFrame = AudioChannels * (AudioBitDepth / 8) // 4

	// AudioBufferDuration sets mixer tick rate and output latency
	// TODO: make it adjustable?
	AudioBufferDuration = 50 * time.Millisecond

	// AudioBufferSamples is frames per mixer tick
	AudioBufferSamples = AudioSampleRate * int(AudioBufferDuration/time.Millisecond) / 1000 // 2205

	// AudioProbeWindow is the backend survival window after the probe write
	AudioProbeWindow = 60 * time.Millisecond

	// AudioPauseFade is the per-sample pause gain ramp length
	AudioPauseFade = 250 * time.Millisecond

	// stderrTailMax bounds the retained backend stderr tail
	stderrTailMax = 2048
)

// --- Mixer bus policy ---
const (
	MusicDuckAmount  = 0.7 // music gain under active SFX
	MusicDuckAttack  = 10 * time.Millisecond
	MusicDuckRelease = 100 * time.Millisecond
)

// --- SFX policy ---
const (
	SFXVariants   = 3  // pre-rendered variants per sound effect
	MaxSFXPerType = 2  // concurrent instances per SoundID (most-progressed stolen)
	MaxActiveSFX  = 12 // global concurrent SFX cap

	// Rapid-fire dampening: repeats inside the cooldown attenuate, then
	// recover proportionally over RapidFireRecovery
	RapidFireCooldown  = 250 * time.Millisecond
	RapidFireDecay     = 0.65
	RapidFireMinVolume = 0.25
	RapidFireRecovery  = 600 * time.Millisecond
)

// --- Drum kit rendering ---
const (
	DrumVariants = 6 // pre-rendered buffers per percussion instrument

	// Deterministic parameter walk across the variant set (peak-to-peak)
	DrumPitchWalk = 0.08 // ±4% pitch
	DrumDecayWalk = 0.20 // ±10% decay

	// Per-drum decay, seconds
	KickDecay  = 0.15
	HihatDecay = 0.08
	SnareDecay = 0.12
	ClapDecay  = 0.10
)

// --- Sequencer grid ---
const (
	DefaultBPM   = 140 // psytrance range 135-150
	MinBPM       = 80
	MaxBPM       = 180
	StepsPerBeat = 4 // 16th notes
	BeatsPerBar  = 4 // 4/4
	StepsPerBar  = StepsPerBeat * BeatsPerBar

	MaxPatternLen = 64 // max steps per pattern
	MusicSlots    = 3  // rhythm, melody, free layer
	MaxPolyphony  = 16 // simultaneous tonal voices per slot

	DefaultSwing = 0.0 // straight
	MaxSwing     = 0.5 // max shuffle

	// DefaultRootNote is the harmony root at engine construction (E2)
	DefaultRootNote = 40
)

// SamplesPerStep returns samples per 16th step at the given tempo
func SamplesPerStep(bpm int) int {
	return AudioSampleRate * 60 / (bpm * StepsPerBeat)
}

// SamplesPerBar returns samples per bar at the given tempo
func SamplesPerBar(bpm int) int {
	return SamplesPerStep(bpm) * StepsPerBar
}

// --- Transitions / generative ---
const (
	MinCrossfadeSamples = 256 // ~6ms declick floor for pattern swaps
	MusicStartFadeIn    = 1200 * time.Millisecond

	HumanizeVelJitter       = 0.12
	HumanizeMaxDelaySamples = AudioSampleRate * 4 / 1000 // 4ms positive lag
	MaxPendingTrigs         = 16

	PhraseBars    = 8
	FillEveryBars = 8
)

// --- Instrument voices (ADSR seconds; sustain is a level) ---
const (
	BassAttack  = 0.005
	BassDecay   = 0.1
	BassSustain = 0.7
	BassRelease = 0.15
	// One-pole cutoff closes as the envelope decays: base - track*envLevel
	BassFilterBase  = 0.3
	BassFilterTrack = 0.2

	PianoAttack  = 0.002
	PianoDecay   = 0.3
	PianoSustain = 0.0 // decay only, no sustain
	PianoRelease = 0.25
	// FM: harmonic modulator ratio, peak index scaled by the envelope
	PianoModRatio = 2.0
	PianoModIndex = 3.0

	PadAttack  = 0.3
	PadDecay   = 0.2
	PadSustain = 0.8
	PadRelease = 1.0
	PadDetune  = 0.003 // ±3 cents across the three oscillators

	// Fallback envelope for unmapped instruments
	VoiceDefaultAttack  = 0.01
	VoiceDefaultDecay   = 0.1
	VoiceDefaultSustain = 0.5
	VoiceDefaultRelease = 0.2
)
