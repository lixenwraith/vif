package audio

import (
	"math"
	"math/rand"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/effects"
	"github.com/lixenwraith/vi-fighter/constants"
)

// WaveType defines oscillator wave shapes
type WaveType int

const (
	WaveSine WaveType = iota
	WaveSquare
	WaveSaw
	WaveNoise
)

// oscillator generates raw audio waves
type oscillator struct {
	freq     float64
	phase    float64
	duration int
	position int
	wave     WaveType
	rate     beep.SampleRate
}

// NewOscillator creates a new oscillator for wave generation
func NewOscillator(freq float64, duration time.Duration, wave WaveType, rate beep.SampleRate) beep.Streamer {
	samples := rate.N(duration)
	return &oscillator{
		freq:     freq,
		phase:    0,
		duration: samples,
		position: 0,
		wave:     wave,
		rate:     rate,
	}
}

func (o *oscillator) Stream(samples [][2]float64) (n int, ok bool) {
	for i := range samples {
		if o.position >= o.duration {
			return i, false
		}

		var val float64
		switch o.wave {
		case WaveSine:
			val = math.Sin(2 * math.Pi * o.phase)
		case WaveSquare:
			if o.phase < 0.5 {
				val = 1.0
			} else {
				val = -1.0
			}
		case WaveSaw:
			val = 2.0 * (o.phase - 0.5)
		case WaveNoise:
			val = rand.Float64()*2 - 1
		}

		samples[i][0] = val
		samples[i][1] = val

		// Advance phase
		o.phase += o.freq / float64(o.rate)
		o.phase = o.phase - math.Floor(o.phase) // Keep in [0, 1)
		o.position++
	}
	return len(samples), true
}

func (o *oscillator) Err() error { return nil }

// envelope applies attack/release shaping to a stream
type envelope struct {
	streamer       beep.Streamer
	position       int
	attackSamples  int
	releaseSamples int
	sustainSamples int
	totalSamples   int
}

// NewEnvelope creates an ADSR envelope (simplified to just attack/release)
func NewEnvelope(s beep.Streamer, duration, attack, release time.Duration, rate beep.SampleRate) beep.Streamer {
	total := rate.N(duration)
	att := rate.N(attack)
	rel := rate.N(release)
	sus := total - att - rel
	if sus < 0 {
		sus = 0
	}

	return &envelope{
		streamer:       s,
		position:       0,
		attackSamples:  att,
		releaseSamples: rel,
		sustainSamples: sus,
		totalSamples:   total,
	}
}

func (e *envelope) Stream(samples [][2]float64) (n int, ok bool) {
	n, ok = e.streamer.Stream(samples)

	for i := 0; i < n; i++ {
		if e.position >= e.totalSamples {
			return i, false
		}

		var vol float64 = 1.0

		// Attack phase
		if e.position < e.attackSamples && e.attackSamples > 0 {
			vol = float64(e.position) / float64(e.attackSamples)
		}
		// Release phase
		releaseStart := e.attackSamples + e.sustainSamples
		if e.position >= releaseStart && e.releaseSamples > 0 {
			remaining := e.totalSamples - e.position
			vol = float64(remaining) / float64(e.releaseSamples)
			if vol < 0 {
				vol = 0
			}
		}

		samples[i][0] *= vol
		samples[i][1] *= vol
		e.position++
	}

	return n, ok
}

func (e *envelope) Err() error { return e.streamer.Err() }

// Helper to create a volume effect safely
// math.Log2(0) is -Inf, so we handle 0 volume by making it silent
func newVolume(s beep.Streamer, vol float64) beep.Streamer {
	if vol <= 0 {
		return &effects.Volume{Streamer: s, Base: 2, Volume: 0, Silent: true}
	}
	return &effects.Volume{Streamer: s, Base: 2, Volume: math.Log2(vol), Silent: false}
}

// Sound effect generators

// CreateErrorSound generates a short harsh buzz for typing errors
func CreateErrorSound(cfg *AudioConfig) beep.Streamer {
	rate := beep.SampleRate(cfg.SampleRate)

	osc := NewOscillator(100.0, constants.ErrorSoundDuration, WaveSaw, rate)
	shaped := NewEnvelope(osc, constants.ErrorSoundDuration, constants.ErrorSoundAttack, constants.ErrorSoundRelease, rate)

	vol := cfg.EffectVolumes[SoundError] * cfg.MasterVolume
	return newVolume(shaped, vol)
}

// CreateBellSound generates a short ding for nugget collection
func CreateBellSound(cfg *AudioConfig) beep.Streamer {
	rate := beep.SampleRate(cfg.SampleRate)

	// Fundamental (A5)
	fund := NewOscillator(880.0, constants.BellSoundDuration, WaveSine, rate)
	fundShaped := NewEnvelope(fund, constants.BellSoundDuration, constants.BellSoundAttack, constants.BellSoundFundamentalRelease, rate)

	// Harmonic (Octave up)
	over := NewOscillator(1760.0, constants.BellSoundDuration, WaveSine, rate)
	overShaped := NewEnvelope(over, constants.BellSoundDuration, constants.BellSoundAttack, constants.BellSoundOvertoneRelease, rate)

	// Mix fundamentals with harmonics
	mixed := beep.Mix(
		newVolume(fundShaped, 0.7),
		newVolume(overShaped, 0.3),
	)

	vol := cfg.EffectVolumes[SoundBell] * cfg.MasterVolume
	return newVolume(mixed, vol)
}

// CreateWhooshSound generates a quick zip noise for cleaner activation
func CreateWhooshSound(cfg *AudioConfig) beep.Streamer {
	rate := beep.SampleRate(cfg.SampleRate)

	noise := NewOscillator(0, constants.WhooshSoundDuration, WaveNoise, rate)
	shaped := NewEnvelope(noise, constants.WhooshSoundDuration, constants.WhooshSoundAttack, constants.WhooshSoundRelease, rate)

	vol := cfg.EffectVolumes[SoundWhoosh] * cfg.MasterVolume
	return newVolume(shaped, vol)
}

// CreateCoinSound generates a two-note chime for gold completion
func CreateCoinSound(cfg *AudioConfig) beep.Streamer {
	rate := beep.SampleRate(cfg.SampleRate)

	// First note (B5)
	n1 := NewOscillator(987.77, constants.CoinSoundNote1Duration, WaveSquare, rate)
	n1Shaped := NewEnvelope(n1, constants.CoinSoundNote1Duration, constants.CoinSoundAttack, constants.CoinSoundNote1Release, rate)

	// Second note (E6)
	n2 := NewOscillator(1318.51, constants.CoinSoundNote2Duration, WaveSquare, rate)
	n2Shaped := NewEnvelope(n2, constants.CoinSoundNote2Duration, constants.CoinSoundAttack, constants.CoinSoundNote2Release, rate)

	sequence := beep.Seq(n1Shaped, n2Shaped)

	vol := cfg.EffectVolumes[SoundCoin] * cfg.MasterVolume
	return newVolume(sequence, vol)
}

// GetSoundEffect returns the appropriate sound effect streamer for the given type
func GetSoundEffect(soundType SoundType, cfg *AudioConfig) beep.Streamer {
	switch soundType {
	case SoundError:
		return CreateErrorSound(cfg)
	case SoundBell:
		return CreateBellSound(cfg)
	case SoundWhoosh:
		return CreateWhooshSound(cfg)
	case SoundCoin:
		return CreateCoinSound(cfg)
	default:
		return nil
	}
}
