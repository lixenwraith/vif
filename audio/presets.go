package audio

import (
	"math"
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

// bullet — highest-rate SFX in the game; kept short and cheap so
// rapid-fire dampening and MaxSFXPerType do the mixing work
const (
	BulletSoundDuration = 70 * time.Millisecond
	BulletSoundAttack   = 1 * time.Millisecond
	BulletDecayRate     = 20 * time.Millisecond
	BulletStartFreq     = 1500.0 // Hz
	BulletEndFreq       = 240.0  // Hz
	BulletTransientLen  = 5 * time.Millisecond
)

// ring — rotary cue. Pitch leads, amplitude trails by 90°: the phase
// quadrature is what reads as rotation on a mono bus (Leslie signature)
const (
	RingSoundDuration = 700 * time.Millisecond
	RingSoundAttack   = 8 * time.Millisecond
	RingSoundRelease  = 260 * time.Millisecond
	RingCarrierFreq   = 660.0 // Hz
	RingPartialRatio  = 1.5   // inharmonic upper partial
	RingRotationRate  = 2.5   // Hz — full circles per second
	RingDopplerDepth  = 0.06  // ±6% pitch across the circle
	RingTremoloDepth  = 0.55
)

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
		return generateBulletSound(v)
	case SoundRing:
		return generateRingSound(v)
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

// generateBulletSound: fast descending saw with a noise transient — the pitch
// drop carries the shot, the transient carries the impact
func generateBulletSound(v sfxVariance) floatBuffer {
	samples := durationToSamples(BulletSoundDuration.Seconds() * v.decay)

	body := oscillatorSweep(waveSaw, BulletStartFreq*v.pitch, BulletEndFreq*v.pitch, samples)
	filterBiquadLP(body, 4200*v.pitch, 0.9) // tame saw harshness at the top of the sweep

	transient := oscillator(waveNoise, 0, durationToSamples(BulletTransientLen.Seconds()))
	filterBiquadHP(transient, 2000, 0.707)
	applyEnvelope(transient, 0.0003, BulletTransientLen.Seconds()*0.8)

	out := mixFloatBuffers(body, transient, 0.6)
	applyWaveshaper(out, 1.8)
	applyDecayEnvelope(out, BulletSoundAttack.Seconds(), float64(AudioSampleRate)*BulletDecayRate.Seconds()*v.decay)
	normalizePeak(out, 0.85)
	return out
}

// generateRingSound: two partials swept by a single rotation phase. Doppler
// (pitch) uses cos, tremolo (amplitude) uses sin — the quadrature offset is
// the rotation cue; in-phase modulation reads as a flat wobble
func generateRingSound(v sfxVariance) floatBuffer {
	samples := durationToSamples(RingSoundDuration.Seconds() * v.decay)
	buf := make(floatBuffer, samples)

	sr := float64(AudioSampleRate)
	base := RingCarrierFreq * v.pitch
	rotInc := RingRotationRate / sr

	var rot, p1, p2 float64
	for i := range buf {
		a := 2 * math.Pi * rot
		freq := base * (1.0 + RingDopplerDepth*math.Cos(a))
		amp := 1.0 - RingTremoloDepth*0.5*(1.0-math.Sin(a))

		buf[i] = (math.Sin(2*math.Pi*p1) + 0.45*math.Sin(2*math.Pi*p2)) * amp

		p1 += freq / sr
		p2 += freq * RingPartialRatio / sr
		if p1 >= 1.0 {
			p1 -= 1.0
		}
		if p2 >= 1.0 {
			p2 -= 1.0
		}
		rot += rotInc
		if rot >= 1.0 {
			rot -= 1.0
		}
	}

	filterBiquadBP(buf, 1600*v.pitch, 0.8) // hollow the mid, pushes the sweep forward
	applyEnvelope(buf, RingSoundAttack.Seconds(), RingSoundRelease.Seconds()*v.decay)
	normalizePeak(buf, 0.85)
	return buf
}
