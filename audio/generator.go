package audio

import (
	"math"
	"math/rand"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// Waveform types
const (
	waveSine = iota
	waveSquare
	waveSaw
	waveNoise
)

// floatBuffer is mono float64 samples at unity gain
type floatBuffer []float64

// oscillator generates raw waveform samples
func oscillator(waveType int, freq float64, samples int) floatBuffer {
	buf := make(floatBuffer, samples)
	phase := 0.0
	phaseInc := freq / float64(parameter.AudioSampleRate)

	for i := 0; i < samples; i++ {
		switch waveType {
		case waveSine:
			buf[i] = math.Sin(2 * math.Pi * phase)
		case waveSquare:
			if phase < 0.5 {
				buf[i] = 1.0
			} else {
				buf[i] = -1.0
			}
		case waveSaw:
			buf[i] = 2.0 * (phase - 0.5)
		case waveNoise:
			buf[i] = rand.Float64()*2 - 1
		}

		phase += phaseInc
		if phase >= 1.0 {
			phase -= 1.0
		}
	}
	return buf
}

// applyEnvelope applies attack/release envelope in place
func applyEnvelope(buf floatBuffer, attackSec, releaseSec float64) {
	total := len(buf)
	attackSamples := int(attackSec * float64(parameter.AudioSampleRate))
	releaseSamples := int(releaseSec * float64(parameter.AudioSampleRate))

	releaseStart := total - releaseSamples
	if releaseStart < attackSamples {
		releaseStart = attackSamples
	}

	for i := 0; i < total; i++ {
		vol := 1.0
		if i < attackSamples && attackSamples > 0 {
			vol = float64(i) / float64(attackSamples)
		} else if i >= releaseStart && releaseSamples > 0 {
			vol = float64(total-i) / float64(releaseSamples)
		}
		buf[i] *= vol
	}
}

// mixFloatBuffers adds b into a (in place), extending a if needed
func mixFloatBuffers(a, b floatBuffer, bScale float64) floatBuffer {
	if len(b) > len(a) {
		extended := make(floatBuffer, len(b))
		copy(extended, a)
		a = extended
	}
	for i := range b {
		a[i] += b[i] * bScale
	}
	return a
}

// concatFloatBuffers appends b to a
func concatFloatBuffers(a, b floatBuffer) floatBuffer {
	result := make(floatBuffer, len(a)+len(b))
	copy(result, a)
	copy(result[len(a):], b)
	return result
}

// durationToSamples converts duration to sample count
func durationToSamples(d float64) int {
	return int(d * float64(parameter.AudioSampleRate))
}

// oscillatorSweep generates waveform with linear frequency glide
func oscillatorSweep(waveType int, startFreq, endFreq float64, samples int) floatBuffer {
	buf := make(floatBuffer, samples)
	phase := 0.0
	freqDelta := (endFreq - startFreq) / float64(samples)

	for i := 0; i < samples; i++ {
		freq := startFreq + freqDelta*float64(i)
		phaseInc := freq / float64(parameter.AudioSampleRate)

		switch waveType {
		case waveSine:
			buf[i] = math.Sin(2 * math.Pi * phase)
		case waveSquare:
			if phase < 0.5 {
				buf[i] = 1.0
			} else {
				buf[i] = -1.0
			}
		case waveSaw:
			buf[i] = 2.0 * (phase - 0.5)
		case waveNoise:
			buf[i] = rand.Float64()*2 - 1
		}

		phase += phaseInc
		if phase >= 1.0 {
			phase -= 1.0
		}
	}
	return buf
}

// applyCrackle applies random amplitude modulation for electrical sound character
// intensity: 0.0 = none, 1.0 = full random (0-1 multiplier per sample)
func applyCrackle(buf floatBuffer, intensity float64) {
	for i := range buf {
		mod := 1.0 - intensity*rand.Float64()
		buf[i] *= mod
	}
}

// applyDecayEnvelope applies attack then exponential-style decay
// decayRate: samples for amplitude to drop to ~37% (1/e)
func applyDecayEnvelope(buf floatBuffer, attackSec float64, decayRate float64) {
	total := len(buf)
	attackSamples := int(attackSec * float64(parameter.AudioSampleRate))

	for i := 0; i < total; i++ {
		var vol float64
		if i < attackSamples && attackSamples > 0 {
			vol = float64(i) / float64(attackSamples)
		} else {
			t := float64(i - attackSamples)
			vol = math.Exp(-t / decayRate)
		}
		buf[i] *= vol
	}
}

// oscillatorFM generates frequency-modulated waveform
// carrierFreq: base frequency, modFreq: modulator frequency
// modIndex: modulation depth (0 = none, higher = more harmonics)
func oscillatorFM(carrierFreq, modFreq, modIndex float64, samples int) floatBuffer {
	buf := make(floatBuffer, samples)
	carrierPhase := 0.0
	modPhase := 0.0
	carrierInc := carrierFreq / float64(parameter.AudioSampleRate)
	modInc := modFreq / float64(parameter.AudioSampleRate)

	for i := 0; i < samples; i++ {
		modValue := math.Sin(2 * math.Pi * modPhase)
		instantPhase := carrierPhase + modIndex*modValue
		buf[i] = math.Sin(2 * math.Pi * instantPhase)

		carrierPhase += carrierInc
		modPhase += modInc
		if carrierPhase >= 1.0 {
			carrierPhase -= 1.0
		}
		if modPhase >= 1.0 {
			modPhase -= 1.0
		}
	}
	return buf
}

// applyAM applies amplitude modulation (tremolo/wobble effect)
// modFreq: oscillation rate in Hz, depth: 0.0-1.0 modulation depth
func applyAM(buf floatBuffer, modFreq, depth float64) {
	phase := 0.0
	phaseInc := modFreq / float64(parameter.AudioSampleRate)

	for i := range buf {
		mod := 1.0 - depth*0.5*(1.0+math.Sin(2*math.Pi*phase))
		buf[i] *= mod
		phase += phaseInc
		if phase >= 1.0 {
			phase -= 1.0
		}
	}
}

// applyRingMod multiplies buffer by a sine oscillator (ring modulation)
// Creates sum and difference frequencies for metallic/bell tones
func applyRingMod(buf floatBuffer, modFreq float64) {
	phase := 0.0
	phaseInc := modFreq / float64(parameter.AudioSampleRate)

	for i := range buf {
		buf[i] *= math.Sin(2 * math.Pi * phase)
		phase += phaseInc
		if phase >= 1.0 {
			phase -= 1.0
		}
	}
}

// filterOnePoleLP applies single-pole low-pass filter
// cutoff: normalized frequency 0.0-1.0 (1.0 = Nyquist)
func filterOnePoleLP(buf floatBuffer, cutoff float64) {
	if cutoff >= 1.0 {
		return
	}
	if cutoff <= 0.0 {
		cutoff = 0.001
	}
	// Attempt to map cutoff to coefficient
	// alpha ≈ cutoff for low values, approaches 1 as cutoff approaches 1
	alpha := cutoff
	if alpha > 0.99 {
		alpha = 0.99
	}

	prev := 0.0
	for i := range buf {
		buf[i] = prev + alpha*(buf[i]-prev)
		prev = buf[i]
	}
}

// filterOnePoleHP applies single-pole high-pass filter
func filterOnePoleHP(buf floatBuffer, cutoff float64) {
	if cutoff <= 0.0 {
		return
	}
	alpha := 1.0 - cutoff
	if alpha < 0.01 {
		alpha = 0.01
	}

	prevIn := 0.0
	prevOut := 0.0
	for i := range buf {
		in := buf[i]
		buf[i] = alpha * (prevOut + in - prevIn)
		prevIn = in
		prevOut = buf[i]
	}
}

// filterBiquadLP applies 2nd-order Butterworth low-pass filter
// cutoffHz: cutoff frequency in Hz, q: resonance (0.707 = Butterworth flat)
func filterBiquadLP(buf floatBuffer, cutoffHz, q float64) {
	if len(buf) < 2 {
		return
	}
	omega := 2.0 * math.Pi * cutoffHz / float64(parameter.AudioSampleRate)
	sinOmega := math.Sin(omega)
	cosOmega := math.Cos(omega)
	alpha := sinOmega / (2.0 * q)

	b0 := (1.0 - cosOmega) / 2.0
	b1 := 1.0 - cosOmega
	b2 := (1.0 - cosOmega) / 2.0
	a0 := 1.0 + alpha
	a1 := -2.0 * cosOmega
	a2 := 1.0 - alpha

	// Normalize
	b0 /= a0
	b1 /= a0
	b2 /= a0
	a1 /= a0
	a2 /= a0

	x1, x2 := 0.0, 0.0
	y1, y2 := 0.0, 0.0

	for i := range buf {
		x0 := buf[i]
		y0 := b0*x0 + b1*x1 + b2*x2 - a1*y1 - a2*y2
		buf[i] = y0

		x2, x1 = x1, x0
		y2, y1 = y1, y0
	}
}

// filterBiquadHP applies 2nd-order Butterworth high-pass filter
func filterBiquadHP(buf floatBuffer, cutoffHz, q float64) {
	if len(buf) < 2 {
		return
	}
	omega := 2.0 * math.Pi * cutoffHz / float64(parameter.AudioSampleRate)
	sinOmega := math.Sin(omega)
	cosOmega := math.Cos(omega)
	alpha := sinOmega / (2.0 * q)

	b0 := (1.0 + cosOmega) / 2.0
	b1 := -(1.0 + cosOmega)
	b2 := (1.0 + cosOmega) / 2.0
	a0 := 1.0 + alpha
	a1 := -2.0 * cosOmega
	a2 := 1.0 - alpha

	b0 /= a0
	b1 /= a0
	b2 /= a0
	a1 /= a0
	a2 /= a0

	x1, x2 := 0.0, 0.0
	y1, y2 := 0.0, 0.0

	for i := range buf {
		x0 := buf[i]
		y0 := b0*x0 + b1*x1 + b2*x2 - a1*y1 - a2*y2
		buf[i] = y0

		x2, x1 = x1, x0
		y2, y1 = y1, y0
	}
}

// filterBiquadBP applies 2nd-order band-pass filter
func filterBiquadBP(buf floatBuffer, centerHz, q float64) {
	if len(buf) < 2 {
		return
	}
	omega := 2.0 * math.Pi * centerHz / float64(parameter.AudioSampleRate)
	sinOmega := math.Sin(omega)
	cosOmega := math.Cos(omega)
	alpha := sinOmega / (2.0 * q)

	b0 := alpha
	b1 := 0.0
	b2 := -alpha
	a0 := 1.0 + alpha
	a1 := -2.0 * cosOmega
	a2 := 1.0 - alpha

	b0 /= a0
	b1 /= a0
	b2 /= a0
	a1 /= a0
	a2 /= a0

	x1, x2 := 0.0, 0.0
	y1, y2 := 0.0, 0.0

	for i := range buf {
		x0 := buf[i]
		y0 := b0*x0 + b1*x1 + b2*x2 - a1*y1 - a2*y2
		buf[i] = y0

		x2, x1 = x1, x0
		y2, y1 = y1, y0
	}
}

// applyWaveshaper applies soft clipping distortion for harmonic enrichment
// drive: 1.0 = subtle, 5.0+ = heavy distortion
func applyWaveshaper(buf floatBuffer, drive float64) {
	for i := range buf {
		buf[i] = math.Tanh(buf[i] * drive)
	}
}

// applyOverdrive clips signals harder than waveshaper for aggressive harmonics
func applyOverdrive(buf floatBuffer, drive float64) {
	for i := range buf {
		val := buf[i] * drive
		// Hard clip with soft knee
		if val > 1.0 {
			val = 1.0
		} else if val < -1.0 {
			val = -1.0
		} else {
			// Polynomial shaping for warmth: x - x^3/3
			val = val - (val*val*val)/3.0
		}
		buf[i] = val
	}
}

// generateImpulses creates discrete high-amplitude spikes
// density: 0.0-1.0 probability of spike per sample (very low values recommended, e.g. 0.001)
func generateImpulses(samples int, density float64) floatBuffer {
	buf := make(floatBuffer, samples)
	for i := range buf {
		if rand.Float64() < density {
			// Random bipolar spike
			if rand.Float64() > 0.5 {
				buf[i] = 1.0
			} else {
				buf[i] = -1.0
			}
		}
	}
	return buf
}

// generateBurst creates a series of micro-transient noise bursts
// burstCount: number of bursts, burstDuration: each burst length
// gapVariation: 0.0-1.0 randomness in gap timing
func generateBurst(burstCount int, burstDurationSec, gapDurationSec, gapVariation float64) floatBuffer {
	burstSamples := durationToSamples(burstDurationSec)
	gapSamples := durationToSamples(gapDurationSec)

	// Estimate total size
	totalSamples := burstCount * (burstSamples + gapSamples)
	buf := make(floatBuffer, 0, totalSamples)

	for i := 0; i < burstCount; i++ {
		// Generate burst
		burst := oscillator(waveNoise, 0, burstSamples)
		applyEnvelope(burst, 0.0005, burstDurationSec*0.7)

		// Vary amplitude per burst
		amp := 0.5 + rand.Float64()*0.5
		for j := range burst {
			burst[j] *= amp
		}

		buf = append(buf, burst...)

		// Variable gap
		actualGap := gapSamples
		if gapVariation > 0 {
			variance := int(float64(gapSamples) * gapVariation * (rand.Float64()*2 - 1))
			actualGap += variance
			if actualGap < 0 {
				actualGap = 0
			}
		}
		gap := make(floatBuffer, actualGap)
		buf = append(buf, gap...)
	}

	return buf
}

// applyPitchEnvelope modulates playback rate simulation via sample skipping/doubling
// Not true pitch shift but creates pitch contour effect for simple cases
// startRate, endRate: 1.0 = normal, 0.5 = octave down, 2.0 = octave up
func applyPitchEnvelope(buf floatBuffer, startRate, endRate float64) floatBuffer {
	if len(buf) == 0 {
		return buf
	}

	// Estimate output length
	avgRate := (startRate + endRate) / 2.0
	outLen := int(float64(len(buf)) / avgRate)
	if outLen < 1 {
		outLen = 1
	}
	out := make(floatBuffer, outLen)

	srcPos := 0.0
	rateDelta := (endRate - startRate) / float64(outLen)
	rate := startRate

	for i := range out {
		idx := int(srcPos)
		if idx >= len(buf)-1 {
			idx = len(buf) - 2
			if idx < 0 {
				idx = 0
			}
		}
		// Linear interpolation
		frac := srcPos - float64(idx)
		if idx+1 < len(buf) {
			out[i] = buf[idx]*(1-frac) + buf[idx+1]*frac
		} else {
			out[i] = buf[idx]
		}

		srcPos += rate
		rate += rateDelta
	}

	return out
}

// normalizePeak scales buffer so max absolute value equals target
func normalizePeak(buf floatBuffer, target float64) {
	var peak float64
	for _, v := range buf {
		if abs := math.Abs(v); abs > peak {
			peak = abs
		}
	}
	if peak < 0.0001 {
		return
	}
	scale := target / peak
	for i := range buf {
		buf[i] *= scale
	}
}

// --- Sound Generators (unity gain) ---

func generateErrorSound() floatBuffer {
	samples := durationToSamples(parameter.ErrorSoundDuration.Seconds())
	buf := oscillator(waveSaw, 100.0, samples)
	applyEnvelope(buf, parameter.ErrorSoundAttack.Seconds(), parameter.ErrorSoundRelease.Seconds())
	return buf
}

func generateBellSound() floatBuffer {
	samples := durationToSamples(parameter.BellSoundDuration.Seconds())

	// Fundamental A5 (880Hz)
	fund := oscillator(waveSine, 880.0, samples)
	applyEnvelope(fund, parameter.BellSoundAttack.Seconds(), parameter.BellSoundFundamentalRelease.Seconds())

	// Overtone A6 (1760Hz)
	over := oscillator(waveSine, 1760.0, samples)
	applyEnvelope(over, parameter.BellSoundAttack.Seconds(), parameter.BellSoundOvertoneRelease.Seconds())

	// Mix 70% fundamental + 30% overtone
	return mixFloatBuffers(fund, over, 0.3/0.7)
}

func generateWhooshSound() floatBuffer {
	samples := durationToSamples(parameter.WhooshSoundDuration.Seconds())
	buf := oscillator(waveNoise, 0, samples)
	applyEnvelope(buf, parameter.WhooshSoundAttack.Seconds(), parameter.WhooshSoundRelease.Seconds())
	return buf
}

func generateCoinSound() floatBuffer {
	// Note 1: B5 (987.77 Hz)
	n1Samples := durationToSamples(parameter.CoinSoundNote1Duration.Seconds())
	n1 := oscillator(waveSquare, 987.77, n1Samples)
	applyEnvelope(n1, parameter.CoinSoundAttack.Seconds(), parameter.CoinSoundNote1Release.Seconds())

	// Note 2: E6 (1318.51 Hz)
	n2Samples := durationToSamples(parameter.CoinSoundNote2Duration.Seconds())
	n2 := oscillator(waveSquare, 1318.51, n2Samples)
	applyEnvelope(n2, parameter.CoinSoundAttack.Seconds(), parameter.CoinSoundNote2Release.Seconds())

	return concatFloatBuffers(n1, n2)
}

func generateShieldSound() floatBuffer {
	samples := durationToSamples(parameter.ShieldSoundDuration.Seconds())

	// Primary sweep - slightly higher start freq for audibility
	sweep := oscillatorSweep(waveSine, parameter.ShieldStartFreq, parameter.ShieldEndFreq, samples)

	// Add sub-harmonic
	sub := oscillatorSweep(waveSine, parameter.ShieldStartFreq*0.5, parameter.ShieldEndFreq*0.5, samples)

	// Mix and Drive: The saturation creates harmonics that make the bass audible on small speakers
	combined := mixFloatBuffers(sweep, sub, 0.6)
	applyOverdrive(combined, 2.5)

	// Low-pass to remove the harsh edge of the overdrive, keeping the "thud"
	filterBiquadLP(combined, 400.0, 1.0)

	// Add a filtered noise layer for the "force field" texture
	noiseSamples := durationToSamples(0.04)
	noise := oscillator(waveNoise, 0, noiseSamples)
	filterBiquadBP(noise, 800.0, 2.0)
	applyEnvelope(noise, 0.005, 0.03)

	result := mixFloatBuffers(combined, noise, 0.2)

	applyEnvelope(result, parameter.ShieldSoundAttack.Seconds(), parameter.ShieldSoundRelease.Seconds())
	normalizePeak(result, 0.95)
	return result
}

func generateZapSound() floatBuffer {
	// Shorter duration for continuous looping capability
	samples := durationToSamples(parameter.ZapSoundDuration.Seconds())

	// Layer 1: Electrical Arc (Band-limited noise)
	arc := oscillator(waveNoise, 0, samples)
	filterBiquadBP(arc, 1200.0, 1.0)

	// Fast AM modulation for the "buzz" (approx 25Hz)
	applyAM(arc, parameter.ZapModulationRate, 0.8)

	// Layer 2: High Voltage Hum (FM Synthesis)
	// Carrier 110Hz, Modulator 55Hz (Sub-octave), High Index -> Saw-like buzz
	hum := oscillatorFM(110.0, 55.0, 3.0, samples)
	// Tremolo on the hum
	applyAM(hum, parameter.ZapModulationRate*0.5, 0.4)

	// Mix layers
	result := mixFloatBuffers(arc, hum, 0.6)

	// Slight distortion to fuse layers
	applyWaveshaper(result, 1.5)

	// Fast envelope for responsive re-triggering
	applyEnvelope(result, parameter.ZapSoundAttack.Seconds(), parameter.ZapSoundRelease.Seconds())

	normalizePeak(result, 0.85)
	return result
}

func generateCrackleSound() floatBuffer {
	samples := durationToSamples(parameter.CrackleSoundDuration.Seconds())

	// Electrical crackle is defined by discrete, high-voltage sparks
	// 1. Generate sparse impulses (sparks)
	sparks := generateImpulses(samples, 0.002) // ~80-90 sparks per second

	// 2. High-pass filter to remove DC/thump, leaving only the "snap"
	filterBiquadHP(sparks, 1500.0, 0.707)

	// 3. Band-pass random resonance to vary the tone of sparks
	// We simulate this by mixing a few differently filtered copies
	layer2 := make(floatBuffer, len(sparks))
	copy(layer2, sparks)
	filterBiquadBP(layer2, 3500.0, 4.0) // High snap

	result := mixFloatBuffers(sparks, layer2, 0.8)

	// 4. Heavy distortion to turn "clicks" into "zaps"
	applyOverdrive(result, 5.0)

	// 5. Hard decay to ensure it sounds like a spark gap, not a noise wash
	applyDecayEnvelope(result, 0.001, float64(parameter.AudioSampleRate)*0.03)

	normalizePeak(result, 0.9)
	return result
}

func generateMetalHitSound() floatBuffer {
	samples := durationToSamples(parameter.MetalHitSoundDuration.Seconds())

	// FM Synthesis is superior for metallic/inharmonic sounds
	// Carrier: 800Hz (Body), Modulator: 1143Hz (non-integer ratio ~1.42), Index: High decaying to low

	// Create FM buffer manually to control index envelope
	fm := make(floatBuffer, samples)
	carrierPhase := 0.0
	modPhase := 0.0
	cFreq := 800.0
	mFreq := 1143.0

	cInc := cFreq / float64(parameter.AudioSampleRate)
	mInc := mFreq / float64(parameter.AudioSampleRate)

	maxIndex := 5.0

	for i := range fm {
		// Index decays over time: rich spectrum at start -> pure tone at end
		progress := float64(i) / float64(samples)
		index := maxIndex * (1.0 - progress*progress) // Quadratic decay

		modVal := math.Sin(2 * math.Pi * modPhase)
		fm[i] = math.Sin(2 * math.Pi * (carrierPhase + index*modVal))

		carrierPhase += cInc
		modPhase += mInc
	}

	// Filter to remove mud
	filterBiquadHP(fm, 200.0, 0.707)

	// Sharp impact transient (Click)
	transientSamples := durationToSamples(parameter.MetalHitTransientLength.Seconds())
	transient := oscillator(waveNoise, 0, transientSamples)
	filterBiquadLP(transient, 3000.0, 1.0)
	applyEnvelope(transient, 0.0005, 0.003)

	// Mix Impact + FM Body
	result := mixFloatBuffers(fm, transient, 0.5)

	// Short envelope for a dead, solid hit
	applyDecayEnvelope(result, parameter.MetalHitAttack.Seconds(), float64(parameter.AudioSampleRate)*parameter.MetalHitDecayRate.Seconds())

	normalizePeak(result, 0.95)
	return result
}

// generateSound dispatches to specific generator
func generateSound(st core.SoundType) floatBuffer {
	switch st {
	case core.SoundError:
		return generateErrorSound()
	case core.SoundBell:
		return generateBellSound()
	case core.SoundWhoosh:
		return generateWhooshSound()
	case core.SoundCoin:
		return generateCoinSound()
	case core.SoundShield:
		return generateShieldSound()
	case core.SoundZap:
		return generateZapSound()
	case core.SoundCrackle:
		return generateCrackleSound()
	case core.SoundMetalHit:
		return generateMetalHitSound()
	default:
		return nil
	}
}