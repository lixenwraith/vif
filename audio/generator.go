package audio

import (
	"math"
	"math/rand"

	"github.com/lixenwraith/vi-fighter/constants"
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
	phaseInc := freq / float64(constants.AudioSampleRate)

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
	attackSamples := int(attackSec * float64(constants.AudioSampleRate))
	releaseSamples := int(releaseSec * float64(constants.AudioSampleRate))

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
	return int(d * float64(constants.AudioSampleRate))
}

// --- Sound Generators (unity gain) ---

func generateErrorSound() floatBuffer {
	samples := durationToSamples(constants.ErrorSoundDuration.Seconds())
	buf := oscillator(waveSaw, 100.0, samples)
	applyEnvelope(buf, constants.ErrorSoundAttack.Seconds(), constants.ErrorSoundRelease.Seconds())
	return buf
}

func generateBellSound() floatBuffer {
	samples := durationToSamples(constants.BellSoundDuration.Seconds())

	// Fundamental A5 (880Hz)
	fund := oscillator(waveSine, 880.0, samples)
	applyEnvelope(fund, constants.BellSoundAttack.Seconds(), constants.BellSoundFundamentalRelease.Seconds())

	// Overtone A6 (1760Hz)
	over := oscillator(waveSine, 1760.0, samples)
	applyEnvelope(over, constants.BellSoundAttack.Seconds(), constants.BellSoundOvertoneRelease.Seconds())

	// Mix 70% fundamental + 30% overtone
	return mixFloatBuffers(fund, over, 0.3/0.7)
}

func generateWhooshSound() floatBuffer {
	samples := durationToSamples(constants.WhooshSoundDuration.Seconds())
	buf := oscillator(waveNoise, 0, samples)
	applyEnvelope(buf, constants.WhooshSoundAttack.Seconds(), constants.WhooshSoundRelease.Seconds())
	return buf
}

func generateCoinSound() floatBuffer {
	// Note 1: B5 (987.77 Hz)
	n1Samples := durationToSamples(constants.CoinSoundNote1Duration.Seconds())
	n1 := oscillator(waveSquare, 987.77, n1Samples)
	applyEnvelope(n1, constants.CoinSoundAttack.Seconds(), constants.CoinSoundNote1Release.Seconds())

	// Note 2: E6 (1318.51 Hz)
	n2Samples := durationToSamples(constants.CoinSoundNote2Duration.Seconds())
	n2 := oscillator(waveSquare, 1318.51, n2Samples)
	applyEnvelope(n2, constants.CoinSoundAttack.Seconds(), constants.CoinSoundNote2Release.Seconds())

	return concatFloatBuffers(n1, n2)
}

// generateSound dispatches to specific generator
func generateSound(st SoundType) floatBuffer {
	switch st {
	case SoundError:
		return generateErrorSound()
	case SoundBell:
		return generateBellSound()
	case SoundWhoosh:
		return generateWhooshSound()
	case SoundCoin:
		return generateCoinSound()
	default:
		return nil
	}
}