package audio

import (
	"time"
)

// --- Per-sound envelope / spectral constants ---

const (
	ErrorSoundDuration = 80 * time.Millisecond
	ErrorSoundAttack   = 5 * time.Millisecond
	ErrorSoundRelease  = 20 * time.Millisecond
)

const (
	BellSoundDuration           = 350 * time.Millisecond
	BellSoundAttack             = 4 * time.Millisecond
	BellSoundFundamentalRelease = 320 * time.Millisecond
	BellSoundOvertoneRelease    = 140 * time.Millisecond
)

const (
	WhooshSoundDuration = 120 * time.Millisecond
	WhooshSoundAttack   = 15 * time.Millisecond
	WhooshSoundRelease  = 70 * time.Millisecond
)

const (
	CoinSoundNote1Duration = 80 * time.Millisecond
	CoinSoundNote2Duration = 160 * time.Millisecond
	CoinSoundAttack        = 5 * time.Millisecond
	CoinSoundNote1Release  = 40 * time.Millisecond
	CoinSoundNote2Release  = 110 * time.Millisecond
)

const (
	ShieldSoundDuration = 100 * time.Millisecond
	ShieldSoundAttack   = 2 * time.Millisecond
	ShieldSoundRelease  = 80 * time.Millisecond
	ShieldStartFreq     = 160.0 // Hz
	ShieldEndFreq       = 40.0  // Hz
)

const (
	ZapSoundDuration  = 180 * time.Millisecond
	ZapSoundAttack    = 5 * time.Millisecond
	ZapSoundRelease   = 30 * time.Millisecond
	ZapModulationRate = 25.0 // Hz
)

// Crackle sparks are impulse-generated; only the envelope length is a constant
const CrackleSoundDuration = 80 * time.Millisecond

const (
	MetalHitSoundDuration   = 120 * time.Millisecond
	MetalHitTransientLength = 4 * time.Millisecond
	MetalHitAttack          = 1 * time.Millisecond
	MetalHitDecayRate       = 40 * time.Millisecond
)

const ExplosionSoundDuration = 280 * time.Millisecond

// Deterministic per-variant deviation walk (peak-to-peak), mirrors buildDrumKit
const (
	SFXPitchWalk = 0.10
	SFXDecayWalk = 0.24
)

// --- Shaping ---

// SFXParams is embedder-supplied per-sound shaping applied at render time.
// Zero fields mean "unmodified". Pitch reaches every preset. Length scales
// only presets whose duration derives from the decay multiplier — Bell,
// Whoosh, Coin, Zap, Crackle, Explosion; Error, Shield and MetalHit have
// fixed durations and ignore it
type SFXParams struct {
	Pitch  float64 // frequency multiplier; 0 = 1.0
	Length float64 // duration/decay multiplier; 0 = 1.0
}

// norm maps the zero value to unity
func norm(v float64) float64 {
	if v <= 0 {
		return 1.0
	}
	return v
}

// sfxVariance is the per-variant deviation composed with SFXParams
type sfxVariance struct {
	pitch float64
	decay float64
}

// RenderVariants renders n deviated takes of one SoundType under p
// The walk is deterministic; noise re-rolls per pass via genRng
// nil is skipped, so the variant set is empty (len 0, not nil)
func RenderVariants(st SoundType, n int, p SFXParams) []floatBuffer {
	if n < 1 {
		n = 1
	}
	bp, bd := norm(p.Pitch), norm(p.Length)
	out := make([]floatBuffer, 0, n)
	for i := 0; i < n; i++ {
		f := float64(i)/float64(n) - 0.5
		v := sfxVariance{
			pitch: bp * (1 + SFXPitchWalk*f),
			decay: bd * (1 + SFXDecayWalk*f),
		}
		if b := generateSound(st, v); b != nil {
			out = append(out, b)
		}
	}
	return out
}

// --- Sound Generators (unity gain) ---

// generateSound dispatches to the preset renderer
// SoundBullet and SoundRing have no renderer yet: RenderVariants yields an
// empty set and Mixer.startSound returns without activating a voice
func generateSound(st SoundType, v sfxVariance) floatBuffer {
	switch st {
	case SoundError:
		return generateErrorSound(v)
	case SoundBell:
		return generateBellSound(v)
	case SoundWhoosh:
		return generateWhooshSound(v)
	case SoundCoin:
		return generateCoinSound(v)
	case SoundShield:
		return generateShieldSound(v)
	case SoundZap:
		return generateZapSound(v)
	case SoundCrackle:
		return generateCrackleSound(v)
	case SoundMetalHit:
		return generateMetalHitSound(v)
	case SoundExplosion:
		return generateExplosionSound(v)
	case SoundBullet:
		// TODO: add generator
		return nil
	case SoundRing:
		// TODO: add generator
		return nil
	default:
		return nil
	}
}

// generateErrorSound: descending filtered saw blip — pitch motion removes buzzer flatness
func generateErrorSound(v sfxVariance) floatBuffer {
	samples := durationToSamples(ErrorSoundDuration.Seconds())
	buf := oscillatorSweep(waveSaw, 130*v.pitch, 85*v.pitch, samples)
	filterBiquadLP(buf, 900, 0.9)
	applyEnvelope(buf, ErrorSoundAttack.Seconds(), ErrorSoundRelease.Seconds())
	normalizePeak(buf, 0.9)
	return buf
}

// generateBellSound: 3 partials, upper slightly stretched — beating shimmer replaces static dyad
func generateBellSound(v sfxVariance) floatBuffer {
	samples := durationToSamples(BellSoundDuration.Seconds() * v.decay)

	fund := oscillator(waveSine, 880*v.pitch, samples)
	applyEnvelope(fund, BellSoundAttack.Seconds(), BellSoundFundamentalRelease.Seconds()*v.decay)

	over := oscillator(waveSine, 1760*v.pitch*1.003, samples)
	applyEnvelope(over, BellSoundAttack.Seconds(), BellSoundOvertoneRelease.Seconds()*v.decay)

	shim := oscillator(waveSine, 2637*v.pitch, samples)
	applyDecayEnvelope(shim, 0.002, float64(AudioSampleRate)*0.05)

	out := mixFloatBuffers(fund, over, 0.35)
	out = mixFloatBuffers(out, shim, 0.18)
	normalizePeak(out, 0.9)
	return out
}

// generateWhooshSound: swept band-passed air with flutter — was a static noise wash
func generateWhooshSound(v sfxVariance) floatBuffer {
	samples := durationToSamples(WhooshSoundDuration.Seconds() * v.decay)
	buf := oscillator(waveNoise, 0, samples)
	sweepBiquadBP(buf, 500*v.pitch, 2600*v.pitch, 1.2)
	applyAM(buf, 13.0, 0.35)
	applyEnvelope(buf, WhooshSoundAttack.Seconds(), WhooshSoundRelease.Seconds()*v.decay)
	normalizePeak(buf, 0.8)
	return buf
}

func generateCoinSound(v sfxVariance) floatBuffer {
	n1Samples := durationToSamples(CoinSoundNote1Duration.Seconds() * v.decay)
	n1 := oscillator(waveSquare, 987.77*v.pitch, n1Samples)
	applyEnvelope(n1, CoinSoundAttack.Seconds(), CoinSoundNote1Release.Seconds()*v.decay)

	n2Samples := durationToSamples(CoinSoundNote2Duration.Seconds() * v.decay)
	n2 := oscillator(waveSquare, 1318.51*v.pitch, n2Samples)
	applyEnvelope(n2, CoinSoundAttack.Seconds(), CoinSoundNote2Release.Seconds()*v.decay)

	out := concatFloatBuffers(n1, n2)
	filterBiquadLP(out, 6000, 0.707) // tame square harshness
	normalizePeak(out, 0.85)
	return out
}

// generateExplosionSound: short detonation — LF drop body + crack transient + rumble tail
func generateExplosionSound(v sfxVariance) floatBuffer {
	samples := durationToSamples(ExplosionSoundDuration.Seconds() * v.decay)

	body := oscillatorSweep(waveSine, 110*v.pitch, 34*v.pitch, samples)
	applyWaveshaper(body, 3.0)
	applyDecayEnvelope(body, 0.002, float64(AudioSampleRate)*0.10*v.decay)

	rumble := oscillator(waveNoise, 0, samples)
	filterBiquadLP(rumble, 450, 0.9)
	applyDecayEnvelope(rumble, 0.004, float64(AudioSampleRate)*0.09*v.decay)

	crack := oscillator(waveNoise, 0, durationToSamples(0.05))
	filterBiquadBP(crack, 2800*v.pitch, 0.8)
	applyDecayEnvelope(crack, 0.001, float64(AudioSampleRate)*0.012)

	out := mixFloatBuffers(body, rumble, 0.8)
	out = mixFloatBuffers(out, crack, 0.7)
	applyWaveshaper(out, 1.6)
	normalizePeak(out, 0.8) // deliberately below other SFX
	return out
}
