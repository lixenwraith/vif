package audio

import "time"

// Engine constants: every value the mixer, sequencer and synthesis path read.
// An embedder's policy (what moves the tiers, its per-sound levels) stays with it.

// --- Hardware / stream format ---
const (
	AudioSampleRate    = 44100
	AudioChannels      = 2
	AudioBitDepth      = 16
	AudioBytesPerFrame = AudioChannels * (AudioBitDepth / 8) // 4

	// AudioBufferDuration is the default mixer period, which sets output latency.
	// AudioConfig.Buffer chooses another inside AudioBufferMin..AudioBufferMax in
	// AudioBufferStep, the shortest period that is a whole number of frames (441).
	AudioBufferDuration = 50 * time.Millisecond
	AudioBufferMin      = 10 * time.Millisecond
	AudioBufferMax      = 200 * time.Millisecond
	AudioBufferStep     = 10 * time.Millisecond

	// AudioProbeWindow is the backend survival window after the probe write
	AudioProbeWindow = 60 * time.Millisecond

	// AudioPauseFade is the per-sample pause gain ramp length
	AudioPauseFade = 250 * time.Millisecond

	// stderrTailMax bounds the retained backend stderr tail
	stderrTailMax = 2048
)

// --- Mixer bus policy ---
const (
	MusicDuckAmount  = 0.7 // music gain under sounding sfx
	MusicDuckAttack  = 10 * time.Millisecond
	MusicDuckRelease = 100 * time.Millisecond

	// The output limiter holds the mix under LimiterCeiling: it sees each peak
	// LimiterLookahead samples before it sounds and ramps its gain down over that
	// time, so stacked kicks duck without the step a gain change inside one sample
	// puts on them, and recovers over LimiterRelease
	LimiterCeiling   = 0.9
	LimiterLookahead = 64
	LimiterRelease   = 80 * time.Millisecond
)

// --- Sfx policy ---
const (
	SoundVariants  = 3  // pre-rendered variants per sound a spec does not count
	MaxSfxPerSound = 2  // concurrent sfx per SoundID (most-progressed stolen)
	MaxActiveSfx   = 12 // global concurrent sfx cap

	// Rapid-fire dampening: repeats inside the cooldown attenuate, then
	// recover proportionally over RapidFireRecovery
	RapidFireCooldown  = 250 * time.Millisecond
	RapidFireDecay     = 0.65
	RapidFireMinVolume = 0.25
	RapidFireRecovery  = 600 * time.Millisecond
)

// --- Sequencer grid ---
const (
	DefaultBPM   = 140 // psytrance range 135-150
	MinBPM       = 80
	MaxBPM       = 200
	StepsPerBeat = 4 // 16th notes
	BeatsPerBar  = 4 // 4/4
	StepsPerBar  = StepsPerBeat * BeatsPerBar

	MaxPatternLen     = 64 // max steps per pattern
	MusicSlots        = 4  // rhythm, melody, free layer, bass
	MaxPolyphony      = 16 // simultaneous tonal voices per slot
	MaxPatternNameLen = 64
	MaxPatternTracks  = 32  // SetMask is a uint32; reveal shifts 1<<n
	MaxPatternEvents  = 512 // per track
	MaxStepDegree     = 64
	MinStepOctave     = -2
	MaxStepOctave     = 8

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
	GroupPhrases  = 4 // phrases one group holds before the music moves to another
	// VariationGapBars is the least a drawn slot rests after any slot changed before a
	// phrase varies or moves it, so a tier change and a variation never stack
	VariationGapBars = 4
)
