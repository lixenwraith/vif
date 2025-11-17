package audio

import (
	"math"
	"sync"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
)

const (
	sampleRate = beep.SampleRate(48000)
)

// SoundManager manages all game audio
type SoundManager struct {
	mu              sync.Mutex
	trailStreamer   *beep.Ctrl
	errorStreamer   *beep.Ctrl
	maxHeatStreamer *beep.Ctrl
	decayStreamer   *beep.Ctrl
	mixer           *beep.Mixer
	initialized     bool
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
	err := speaker.Init(sampleRate, sampleRate.N(time.Millisecond*100))
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

	// Stop all active sounds
	if sm.trailStreamer != nil {
		sm.trailStreamer.Paused = true
	}
	if sm.errorStreamer != nil {
		sm.errorStreamer.Paused = true
	}
	if sm.maxHeatStreamer != nil {
		sm.maxHeatStreamer.Paused = true
	}
	if sm.decayStreamer != nil {
		sm.decayStreamer.Paused = true
	}

	// Clear mixer
	sm.mixer.Clear()

	// Note: beep doesn't provide a Close() method for speaker,
	// but clearing all streamers ensures no audio artifacts
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
	streamer := beep.Loop(-1, NewWhroomGenerator(sampleRate))
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

	// Create a short low-pitched buzz
	streamer := beep.Take(sampleRate.N(time.Millisecond*150), NewBuzzGenerator(sampleRate, 120))
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
	streamer := beep.Loop(-1, NewSynthwaveGenerator(sampleRate))
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

	// Create a crackling/breaking sound effect
	streamer := beep.Take(sampleRate.N(time.Millisecond*300), NewDecayGenerator(sampleRate))
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
		samples: sr.N(time.Second * 2), // 2 second cycle
	}
}

func (g *WhroomGenerator) Stream(samples [][2]float64) (n int, ok bool) {
	for i := range samples {
		t := float64(g.pos) / float64(g.sr)

		// Frequency sweep from 80Hz to 200Hz and back
		cyclePos := float64(g.pos%g.samples) / float64(g.samples)
		freq := 80 + 120*math.Sin(cyclePos*math.Pi)

		// Generate sine wave with envelope
		amplitude := 0.15 * (0.5 + 0.5*math.Sin(cyclePos*math.Pi*2))
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
		envelope := math.Min(float64(g.pos)/float64(g.sr)/0.02, 1.0)
		sample *= envelope * 0.2

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
		samples: sr.N(time.Millisecond * 600), // 100 BPM (600ms per beat)
	}
}

func (g *SynthwaveGenerator) Stream(samples [][2]float64) (n int, ok bool) {
	for i := range samples {
		beatPos := g.pos % g.samples
		t := float64(beatPos) / float64(g.sr)

		// Kick drum on beat 1
		kick := 0.0
		if beatPos < g.sr.N(time.Millisecond*100) {
			kickEnv := 1.0 - float64(beatPos)/float64(g.sr.N(time.Millisecond*100))
			kickFreq := 60 * (1 + 2*kickEnv)
			kick = 0.4 * kickEnv * math.Sin(2*math.Pi*kickFreq*t)
		}

		// Bass synth
		bass := 0.15 * math.Sin(2*math.Pi*110*t)

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
		envelope := math.Exp(-t * 8)

		// Noise with low-pass filtering for crackling
		g.seed = (g.seed*1103515245 + 12345) & 0x7fffffff
		noise := float64(g.seed)/float64(0x7fffffff)*2 - 1

		// Mix with low rumble
		rumble := 0.3 * math.Sin(2*math.Pi*80*t)

		sample := envelope * (0.25*noise + rumble)

		samples[i][0] = sample
		samples[i][1] = sample
		g.pos++
	}
	return len(samples), true
}

func (g *DecayGenerator) Err() error {
	return nil
}
