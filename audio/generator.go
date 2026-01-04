package audio

import (
	"math"
	"math/rand"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
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
	phaseInc := freq / float64(constant.AudioSampleRate)

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
	attackSamples := int(attackSec * float64(constant.AudioSampleRate))
	releaseSamples := int(releaseSec * float64(constant.AudioSampleRate))

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
	return int(d * float64(constant.AudioSampleRate))
}

// oscillatorSweep generates waveform with linear frequency glide
func oscillatorSweep(waveType int, startFreq, endFreq float64, samples int) floatBuffer {
	buf := make(floatBuffer, samples)
	phase := 0.0
	freqDelta := (endFreq - startFreq) / float64(samples)

	for i := 0; i < samples; i++ {
		freq := startFreq + freqDelta*float64(i)
		phaseInc := freq / float64(constant.AudioSampleRate)

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
	attackSamples := int(attackSec * float64(constant.AudioSampleRate))

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
	carrierInc := carrierFreq / float64(constant.AudioSampleRate)
	modInc := modFreq / float64(constant.AudioSampleRate)

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
	phaseInc := modFreq / float64(constant.AudioSampleRate)

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
	phaseInc := modFreq / float64(constant.AudioSampleRate)

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
	omega := 2.0 * math.Pi * cutoffHz / float64(constant.AudioSampleRate)
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
	omega := 2.0 * math.Pi * cutoffHz / float64(constant.AudioSampleRate)
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
	omega := 2.0 * math.Pi * centerHz / float64(constant.AudioSampleRate)
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
	samples := durationToSamples(constant.ErrorSoundDuration.Seconds())
	buf := oscillator(waveSaw, 100.0, samples)
	applyEnvelope(buf, constant.ErrorSoundAttack.Seconds(), constant.ErrorSoundRelease.Seconds())
	return buf
}

func generateBellSound() floatBuffer {
	samples := durationToSamples(constant.BellSoundDuration.Seconds())

	// Fundamental A5 (880Hz)
	fund := oscillator(waveSine, 880.0, samples)
	applyEnvelope(fund, constant.BellSoundAttack.Seconds(), constant.BellSoundFundamentalRelease.Seconds())

	// Overtone A6 (1760Hz)
	over := oscillator(waveSine, 1760.0, samples)
	applyEnvelope(over, constant.BellSoundAttack.Seconds(), constant.BellSoundOvertoneRelease.Seconds())

	// Mix 70% fundamental + 30% overtone
	return mixFloatBuffers(fund, over, 0.3/0.7)
}

func generateWhooshSound() floatBuffer {
	samples := durationToSamples(constant.WhooshSoundDuration.Seconds())
	buf := oscillator(waveNoise, 0, samples)
	applyEnvelope(buf, constant.WhooshSoundAttack.Seconds(), constant.WhooshSoundRelease.Seconds())
	return buf
}

func generateCoinSound() floatBuffer {
	// Note 1: B5 (987.77 Hz)
	n1Samples := durationToSamples(constant.CoinSoundNote1Duration.Seconds())
	n1 := oscillator(waveSquare, 987.77, n1Samples)
	applyEnvelope(n1, constant.CoinSoundAttack.Seconds(), constant.CoinSoundNote1Release.Seconds())

	// Note 2: E6 (1318.51 Hz)
	n2Samples := durationToSamples(constant.CoinSoundNote2Duration.Seconds())
	n2 := oscillator(waveSquare, 1318.51, n2Samples)
	applyEnvelope(n2, constant.CoinSoundAttack.Seconds(), constant.CoinSoundNote2Release.Seconds())

	return concatFloatBuffers(n1, n2)
}

func generateShieldSound() floatBuffer {
	samples := durationToSamples(constant.ShieldSoundDuration.Seconds())

	// Lower pitch sweep for deeper thump
	thump := oscillatorSweep(waveSine, constant.ShieldStartFreq, constant.ShieldEndFreq, samples)

	// Add sub-harmonic for body
	sub := oscillatorSweep(waveSine, constant.ShieldStartFreq/2, constant.ShieldEndFreq/2, samples)

	// Combine before envelope
	thump = mixFloatBuffers(thump, sub, 0.5)

	// Heavy low-pass to remove metallic character
	filterBiquadLP(thump, 200.0, 0.5)

	applyEnvelope(thump, constant.ShieldSoundAttack.Seconds(), constant.ShieldSoundRelease.Seconds())

	// Soft transient - filtered noise
	transientSamples := durationToSamples(0.012)
	transient := oscillator(waveNoise, 0, transientSamples)
	filterBiquadLP(transient, 400.0, 0.707)
	applyEnvelope(transient, 0.001, 0.010)

	result := mixFloatBuffers(thump, transient, 0.15)

	normalizePeak(result, 0.9)
	return result
}

func generateZapSound() floatBuffer {
	samples := durationToSamples(constant.ZapSoundDuration.Seconds())

	// Base: band-limited noise in mid frequencies
	noise := oscillator(waveNoise, 0, samples)
	filterBiquadBP(noise, 800.0, 1.5)

	// Amplitude modulation for "zzZZzz" pulsing effect
	applyAM(noise, constant.ZapModulationRate, 0.6)

	// Secondary wobble layer - slightly detuned
	wobble := oscillator(waveNoise, 0, samples)
	filterBiquadBP(wobble, 1200.0, 2.0)
	applyAM(wobble, constant.ZapModulationRate*1.3, 0.5)

	// Low buzz undertone
	buzz := oscillatorFM(120.0, 60.0, 2.0, samples)
	applyAM(buzz, constant.ZapModulationRate*0.8, 0.3)

	// Combine layers
	result := mixFloatBuffers(noise, wobble, 0.4)
	result = mixFloatBuffers(result, buzz, 0.3)

	applyEnvelope(result, constant.ZapSoundAttack.Seconds(), constant.ZapSoundRelease.Seconds())

	normalizePeak(result, 0.85)
	return result
}

func generateCrackleSound() floatBuffer {
	// Electrical crackle = rapid discrete micro-bursts, not sustained noise
	bursts := generateBurst(
		constant.CrackleBurstCount,
		constant.CrackleBurstDuration.Seconds(),
		constant.CrackleGapDuration.Seconds(),
		0.6, // High gap variation for organic feel
	)

	// Band-pass to electrical frequency range
	filterBiquadBP(bursts, 2500.0, 1.0)

	// Add sharp high-freq snap at start
	snapSamples := durationToSamples(0.008)
	snap := oscillator(waveNoise, 0, snapSamples)
	filterBiquadHP(snap, 3000.0, 0.707)
	applyEnvelope(snap, 0.0003, 0.006)

	// Prepend snap
	result := concatFloatBuffers(snap, bursts)

	// Overall envelope
	applyEnvelope(result, 0.001, float64(len(result))/float64(constant.AudioSampleRate)*0.3)

	normalizePeak(result, 0.9)
	return result
}

func generateMetalHitSound() floatBuffer {
	samples := durationToSamples(constant.MetalHitSoundDuration.Seconds())
	decaySamples := float64(constant.AudioSampleRate) * constant.MetalHitDecayRate.Seconds()

	// Sharp transient
	transientSamples := durationToSamples(constant.MetalHitTransientLength.Seconds())
	transient := oscillator(waveNoise, 0, transientSamples)
	filterBiquadBP(transient, 2000.0, 2.0)
	applyEnvelope(transient, 0.0003, constant.MetalHitTransientLength.Seconds()*0.6)

	// Shorter inharmonic ring - fewer components, faster decay
	ring1 := oscillator(waveSine, 1200.0, samples)
	ring2 := oscillator(waveSine, 2050.0, samples)

	applyDecayEnvelope(ring1, constant.MetalHitAttack.Seconds(), decaySamples)
	applyDecayEnvelope(ring2, constant.MetalHitAttack.Seconds(), decaySamples*0.6)

	ring := mixFloatBuffers(ring1, ring2, 0.5)

	// Combine
	result := mixFloatBuffers(ring, transient, 0.7)

	// Abrupt tail cutoff
	cutoffStart := int(float64(len(result)) * 0.7)
	cutoffLen := len(result) - cutoffStart
	for i := cutoffStart; i < len(result); i++ {
		fade := 1.0 - float64(i-cutoffStart)/float64(cutoffLen)
		fade = fade * fade // Quadratic for sharper drop
		result[i] *= fade
	}

	normalizePeak(result, 0.9)
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