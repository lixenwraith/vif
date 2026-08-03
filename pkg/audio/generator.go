package audio

import (
	"math"
)

// Waveform types
const (
	waveSine = iota
	waveSquare
	waveSaw
	waveNoise
)

// floatBuffer is mono float64 samples at unity gain.
//
// Alias, not a defined type: exported entry points (RenderVariants,
// RenderPreview, WriteWAV) hand buffers to callers outside the package, and a
// defined type would be unnameable there. Nothing in the package relies on
// buffers being distinguishable from a bare []float64.
type floatBuffer = []float64

// applyEnvelope applies attack/release envelope in place
func applyEnvelope(buf floatBuffer, attackSec, releaseSec float64) {
	total := len(buf)
	attackSamples := int(attackSec * float64(AudioSampleRate))
	releaseSamples := int(releaseSec * float64(AudioSampleRate))

	releaseStart := max(total-releaseSamples, attackSamples)

	for i := range total {
		vol := 1.0
		if i < attackSamples && attackSamples > 0 {
			vol = float64(i) / float64(attackSamples)
		} else if i >= releaseStart && releaseSamples > 0 {
			vol = float64(total-i) / float64(releaseSamples)
		}
		buf[i] *= vol
	}
}

// applyDecayEnvelope applies attack then exponential-style decay
// decayRate: samples for amplitude to drop to ~37% (1/e)
func applyDecayEnvelope(buf floatBuffer, attackSec float64, decayRate float64) {
	total := len(buf)
	attackSamples := int(attackSec * float64(AudioSampleRate))

	for i := range total {
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

// applyAM applies amplitude modulation. phase offsets the modulator in
// cycles: 0.5 inverts the sine, which is what locks the ring tremolo in
// quadrature with its Doppler vibrato.
func applyAM(buf floatBuffer, modFreq, depth, phase float64) {
	inc := modFreq / float64(AudioSampleRate)
	p := 0.0
	for i := range buf {
		buf[i] *= 1.0 - depth*0.5*(1.0+math.Sin(2*math.Pi*(p+phase)))
		if p += inc; p >= 1.0 {
			p -= 1.0
		}
	}
}

// applyRingMod multiplies buffer by a sine oscillator (ring modulation)
// Creates sum and difference frequencies for metallic/bell tones
func applyRingMod(buf floatBuffer, modFreq, phase float64) {
	phaseInc := modFreq / float64(AudioSampleRate)

	for i := range buf {
		buf[i] *= math.Sin(2 * math.Pi * phase)
		phase += phaseInc
		if phase >= 1.0 {
			phase -= 1.0
		}
	}
}

// filterBiquadLP applies 2nd-order Butterworth low-pass filter
// cutoffHz: cutoff frequency in Hz, q: resonance (0.707 = Butterworth flat)
func filterBiquadLP(buf floatBuffer, cutoffHz, q float64) {
	if len(buf) < 2 {
		return
	}
	omega := 2.0 * math.Pi * cutoffHz / float64(AudioSampleRate)
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
	omega := 2.0 * math.Pi * cutoffHz / float64(AudioSampleRate)
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
	omega := 2.0 * math.Pi * centerHz / float64(AudioSampleRate)
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

// sweepBiquadBP glides a band-pass center start→end Hz across the buffer
// Coefficients recomputed per 64-sample block; filter state carries across
// blocks, keeping the steps click-free. Render-time only
func sweepBiquadBP(buf floatBuffer, startHz, endHz, q float64) {
	const blk = 64
	n := len(buf)
	if n == 0 {
		return
	}
	var x1, x2, y1, y2 float64
	for off := 0; off < n; off += blk {
		end := min(off+blk, n)
		f := startHz + (endHz-startHz)*float64(off)/float64(n)
		omega := 2 * math.Pi * f / float64(AudioSampleRate)
		sn, cs := math.Sin(omega), math.Cos(omega)
		alpha := sn / (2 * q)
		a0 := 1 + alpha
		b0 := alpha / a0
		b2 := -alpha / a0
		a1 := -2 * cs / a0
		a2 := (1 - alpha) / a0
		for i := off; i < end; i++ {
			x0 := buf[i]
			y0 := b0*x0 + b2*x2 - a1*y1 - a2*y2
			buf[i] = y0
			x2, x1 = x1, x0
			y2, y1 = y1, y0
		}
	}
}
