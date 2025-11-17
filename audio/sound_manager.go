package audio

import (
	"math"
	"sync"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
)

const (
	// Audio system configuration
	sampleRate              = beep.SampleRate(48000)
	speakerBufferDurationMs = 100

	// Sound effect durations
	errorBuzzDurationMs  = 150
	decaySoundDurationMs = 300

	// Concurrency limits for one-shot sounds
	maxConcurrentErrorSounds = 3
	maxConcurrentDecaySounds = 3

	// Error buzz parameters
	errorBuzzFrequencyHz = 120.0
	errorBuzzAmplitude   = 0.2
	errorBuzzFadeTimeMs  = 20.0

	// Whroom (trail) sound parameters
	whroomCycleDurationS = 2
	whroomFreqMinHz      = 80.0
	whroomFreqMaxHz      = 200.0
	whroomBaseAmplitude  = 0.15

	// Synthwave (max heat) parameters
	synthwaveBPM             = 100
	synthwaveBeatIntervalMs  = 600 // 100 BPM = 600ms per beat
	synthwaveKickDurationMs  = 100
	synthwaveBassFrequencyHz = 110.0
	synthwaveBassAmplitude   = 0.15
	synthwaveKickAmplitude   = 0.4
	synthwaveKickFrequencyHz = 60.0

	// Decay sound parameters
	decayEnvelopeDecayRate = 8.0
	decayNoiseAmplitude    = 0.25
	decayRumbleFrequencyHz = 80.0
	decayRumbleAmplitude   = 0.3
)

// SoundManager manages all game audio
type SoundManager struct {
	mu                 sync.Mutex
	trailStreamer      *beep.Ctrl
	maxHeatStreamer    *beep.Ctrl
	mixer              *beep.Mixer
	initialized        bool
	activeErrorSounds  int
	activeDecaySounds  int
}

// NewSoundManager creates a new sound manager
func NewSoundManager() *SoundManager {
	sm := &SoundManager{
		mixer: &beep.Mixer{},
	}
	return sm
}

// Initialize sets up the audio system
func (sm *SoundManager) Initialize() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.initialized {
		return nil
	}

	// Initialize speaker with sample rate and buffer size
	err := speaker.Init(sampleRate, sampleRate.N(time.Millisecond*speakerBufferDurationMs))
	if err != nil {
		return err
	}

	speaker.Play(sm.mixer)
	sm.initialized = true
	return nil
}

// Cleanup stops all sounds and closes the audio system
func (sm *SoundManager) Cleanup() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.initialized {
		return
	}

	// Stop all active sounds by pausing them
	if sm.trailStreamer != nil {
		sm.trailStreamer.Paused = true
	}
	if sm.maxHeatStreamer != nil {
		sm.maxHeatStreamer.Paused = true
	}

	// Reset counters for one-shot sounds
	// Note: We don't call mixer.Clear() because it causes race conditions with
	// the speaker's streaming goroutine. The mixer is safe to leave with paused
	// streamers, and new sounds won't be added once initialized=false.
	sm.activeErrorSounds = 0
	sm.activeDecaySounds = 0

	// Mark as uninitialized to prevent new sounds from being added
	// Note: beep doesn't provide a Close() method for speaker.
	// The speaker goroutine will continue running but won't produce sound
	// since all streamers are paused and no new ones will be added.
	sm.initialized = false
}

// PlayTrail starts the 'whroom' trail sound
func (sm *SoundManager) PlayTrail() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.initialized {
		return
	}

	// If already playing, don't restart
	if sm.trailStreamer != nil && !sm.trailStreamer.Paused {
		return
	}

	// Create a sweeping 'whroom' sound - low to high frequency sweep
	streamer := NewWhroomGenerator(sampleRate)
	ctrl := &beep.Ctrl{Streamer: streamer, Paused: false}
	sm.trailStreamer = ctrl
	sm.mixer.Add(ctrl)
}

// StopTrail stops the trail sound
func (sm *SoundManager) StopTrail() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.trailStreamer != nil {
		sm.trailStreamer.Paused = true
	}
}

// PlayError plays a short error buzz sound
func (sm *SoundManager) PlayError() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.initialized {
		return
	}

	// Limit concurrent error sounds to prevent buffer overflow
	if sm.activeErrorSounds >= maxConcurrentErrorSounds {
		return
	}

	sm.activeErrorSounds++

	// Create a short low-pitched buzz with callback to decrement counter
	sound := beep.Take(sampleRate.N(time.Millisecond*errorBuzzDurationMs), NewBuzzGenerator(sampleRate, errorBuzzFrequencyHz))
	streamer := beep.Seq(sound, beep.Callback(func() {
		sm.mu.Lock()
		sm.activeErrorSounds--
		if sm.activeErrorSounds < 0 {
			sm.activeErrorSounds = 0
		}
		sm.mu.Unlock()
	}))
	sm.mixer.Add(streamer)
}

// PlayMaxHeat starts the rhythmic beat for max heat
func (sm *SoundManager) PlayMaxHeat() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.initialized {
		return
	}

	// If already playing, don't restart
	if sm.maxHeatStreamer != nil && !sm.maxHeatStreamer.Paused {
		return
	}

	// Create a synthwave-style rhythmic beat
	streamer := NewSynthwaveGenerator(sampleRate)
	ctrl := &beep.Ctrl{Streamer: streamer, Paused: false}
	sm.maxHeatStreamer = ctrl
	sm.mixer.Add(ctrl)
}

// StopMaxHeat stops the max heat rhythm
func (sm *SoundManager) StopMaxHeat() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.maxHeatStreamer != nil {
		sm.maxHeatStreamer.Paused = true
	}
}

// PlayDecay plays the breaking/rotting sound
func (sm *SoundManager) PlayDecay() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.initialized {
		return
	}

	// Limit concurrent decay sounds to prevent buffer overflow
	if sm.activeDecaySounds >= maxConcurrentDecaySounds {
		return
	}

	sm.activeDecaySounds++

	// Create a crackling/breaking sound effect with callback to decrement counter
	sound := beep.Take(sampleRate.N(time.Millisecond*decaySoundDurationMs), NewDecayGenerator(sampleRate))
	streamer := beep.Seq(sound, beep.Callback(func() {
		sm.mu.Lock()
		sm.activeDecaySounds--
		if sm.activeDecaySounds < 0 {
			sm.activeDecaySounds = 0
		}
		sm.mu.Unlock()
	}))
	sm.mixer.Add(streamer)
}

// WhroomGenerator generates a sweeping 'whroom' sound
type WhroomGenerator struct {
	sr      beep.SampleRate
	pos     int
	samples int
}

// NewWhroomGenerator creates a whroom sound generator
func NewWhroomGenerator(sr beep.SampleRate) *WhroomGenerator {
	return &WhroomGenerator{
		sr:      sr,
		samples: sr.N(time.Second * whroomCycleDurationS),
	}
}

func (g *WhroomGenerator) Stream(samples [][2]float64) (n int, ok bool) {
	for i := range samples {
		t := float64(g.pos) / float64(g.sr)

		// Frequency sweep from whroomFreqMinHz to whroomFreqMaxHz and back
		cyclePos := float64(g.pos%g.samples) / float64(g.samples)
		freqRange := whroomFreqMaxHz - whroomFreqMinHz
		freq := whroomFreqMinHz + freqRange*math.Sin(cyclePos*math.Pi)

		// Generate sine wave with envelope
		amplitude := whroomBaseAmplitude * (0.5 + 0.5*math.Sin(cyclePos*math.Pi*2))
		sample := amplitude * math.Sin(2*math.Pi*freq*t)

		samples[i][0] = sample
		samples[i][1] = sample
		g.pos++
	}
	return len(samples), true
}

func (g *WhroomGenerator) Err() error {
	return nil
}

// BuzzGenerator generates a low-pitch buzz sound
type BuzzGenerator struct {
	sr   beep.SampleRate
	freq float64
	pos  int
}

// NewBuzzGenerator creates a buzz sound generator
func NewBuzzGenerator(sr beep.SampleRate, freq float64) *BuzzGenerator {
	return &BuzzGenerator{
		sr:   sr,
		freq: freq,
	}
}

func (g *BuzzGenerator) Stream(samples [][2]float64) (n int, ok bool) {
	for i := range samples {
		t := float64(g.pos) / float64(g.sr)

		// Square wave with harmonics for harsh buzz
		sample := 0.0
		sample += 0.3 * math.Sin(2*math.Pi*g.freq*t)
		sample += 0.15 * math.Sin(2*math.Pi*g.freq*2*t)
		sample += 0.075 * math.Sin(2*math.Pi*g.freq*3*t)

		// Envelope to fade in/out
		fadeTimeSec := errorBuzzFadeTimeMs / 1000.0
		envelope := math.Min(float64(g.pos)/float64(g.sr)/fadeTimeSec, 1.0)
		sample *= envelope * errorBuzzAmplitude

		samples[i][0] = sample
		samples[i][1] = sample
		g.pos++
	}
	return len(samples), true
}

func (g *BuzzGenerator) Err() error {
	return nil
}

// SynthwaveGenerator generates a rhythmic synthwave beat
type SynthwaveGenerator struct {
	sr      beep.SampleRate
	pos     int
	samples int
}

// NewSynthwaveGenerator creates a synthwave beat generator
func NewSynthwaveGenerator(sr beep.SampleRate) *SynthwaveGenerator {
	return &SynthwaveGenerator{
		sr:      sr,
		samples: sr.N(time.Millisecond * synthwaveBeatIntervalMs),
	}
}

func (g *SynthwaveGenerator) Stream(samples [][2]float64) (n int, ok bool) {
	for i := range samples {
		beatPos := g.pos % g.samples
		t := float64(beatPos) / float64(g.sr)

		// Kick drum on beat 1
		kick := 0.0
		kickDuration := g.sr.N(time.Millisecond * synthwaveKickDurationMs)
		if beatPos < kickDuration {
			kickEnv := 1.0 - float64(beatPos)/float64(kickDuration)
			kickFreq := synthwaveKickFrequencyHz * (1 + 2*kickEnv)
			kick = synthwaveKickAmplitude * kickEnv * math.Sin(2*math.Pi*kickFreq*t)
		}

		// Bass synth
		bass := synthwaveBassAmplitude * math.Sin(2*math.Pi*synthwaveBassFrequencyHz*t)

		sample := kick + bass

		samples[i][0] = sample
		samples[i][1] = sample
		g.pos++
	}
	return len(samples), true
}

func (g *SynthwaveGenerator) Err() error {
	return nil
}

// DecayGenerator generates a breaking/crackling sound
type DecayGenerator struct {
	sr   beep.SampleRate
	pos  int
	seed int64
}

// NewDecayGenerator creates a decay sound generator
func NewDecayGenerator(sr beep.SampleRate) *DecayGenerator {
	return &DecayGenerator{
		sr:   sr,
		seed: time.Now().UnixNano(),
	}
}

func (g *DecayGenerator) Stream(samples [][2]float64) (n int, ok bool) {
	for i := range samples {
		t := float64(g.pos) / float64(g.sr)

		// Envelope - quick attack, slower decay
		envelope := math.Exp(-t * decayEnvelopeDecayRate)

		// Noise with low-pass filtering for crackling
		g.seed = (g.seed*1103515245 + 12345) & 0x7fffffff
		noise := float64(g.seed)/float64(0x7fffffff)*2 - 1

		// Mix with low rumble
		rumble := decayRumbleAmplitude * math.Sin(2*math.Pi*decayRumbleFrequencyHz*t)

		sample := envelope * (decayNoiseAmplitude*noise + rumble)

		samples[i][0] = sample
		samples[i][1] = sample
		g.pos++
	}
	return len(samples), true
}

func (g *DecayGenerator) Err() error {
	return nil
}