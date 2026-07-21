package audio

import (
	"math"
)

// drumKit holds pre-rendered percussion buffers, N variants per instrument
// Built once at engine Start, read-only afterward
type drumKit struct {
	variants [InstrumentCount][]floatBuffer
}

// buildDrumKit renders n variants per drum with a deterministic parameter
// walk (±4% pitch, ±10% decay); noise re-rolls per render pass
func buildDrumKit(n int) *drumKit {
	if n < 1 {
		n = 1
	}
	k := &drumKit{}
	for i := 0; i < n; i++ {
		det := 1.0 + DrumPitchWalk*(float64(i)/float64(n)-0.5)
		dec := 1.0 + DrumDecayWalk*(float64(i)/float64(n)-0.5)
		k.variants[InstrKick] = append(k.variants[InstrKick], generateKickVar(det, dec))
		k.variants[InstrHihat] = append(k.variants[InstrHihat], generateHihatVar(dec))
		k.variants[InstrSnare] = append(k.variants[InstrSnare], generateSnareVar(det, dec))
		k.variants[InstrClap] = append(k.variants[InstrClap], generateHihatVar(dec))
	}
	return k
}

func generateKickVar(det, dec float64) floatBuffer {
	sr := AudioSampleRate
	duration := int(float64(sr) * KickDecay * dec)
	buf := make(floatBuffer, duration)
	startFreq, endFreq := 150.0*det, 40.0*det
	phase := 0.0
	for i := 0; i < duration; i++ {
		t := float64(i) / float64(duration)
		freq := endFreq + (startFreq-endFreq)*math.Exp(-8*t)
		buf[i] = math.Sin(2*math.Pi*phase) * math.Exp(-5*t)
		phase += freq / float64(sr)
	}
	for i := range buf {
		buf[i] = math.Tanh(buf[i] * 2.0)
	}
	return buf
}

func generateHihatVar(dec float64) floatBuffer {
	sr := AudioSampleRate
	duration := int(float64(sr) * HihatDecay * dec)
	buf := make(floatBuffer, duration)
	for i := 0; i < duration; i++ {
		t := float64(i) / float64(duration)
		buf[i] = (genRng.Float64()*2 - 1) * math.Exp(-15*t)
	}
	filterBiquadHP(buf, 7000, 0.707)
	normalizePeak(buf, 0.9)
	return buf
}

func generateSnareVar(det, dec float64) floatBuffer {
	sr := AudioSampleRate
	duration := int(float64(sr) * SnareDecay * dec)
	buf := make(floatBuffer, duration)

	tonePhase := 0.0
	toneFreq := 200.0 * det
	for i := 0; i < duration; i++ {
		t := float64(i) / float64(duration)
		buf[i] = math.Sin(2*math.Pi*tonePhase) * math.Exp(-10*t) * 0.5
		tonePhase += toneFreq / float64(sr)
	}
	for i := 0; i < duration; i++ {
		t := float64(i) / float64(duration)
		buf[i] += (genRng.Float64()*2 - 1) * math.Exp(-8*t) * 0.5
	}
	filterBiquadBP(buf, 2000*det, 1.5)
	normalizePeak(buf, 0.9)
	return buf
}

func generateClapVar(dec float64) floatBuffer {
	sr := AudioSampleRate
	duration := int(float64(sr) * ClapDecay * dec)
	buf := make(floatBuffer, duration)

	burstLen := sr / 100
	burstGap := sr / 200
	pos := 0
	for b := 0; b < 4 && pos < duration; b++ {
		burstAmp := 1.0 - float64(b)*0.15
		for i := 0; i < burstLen && pos < duration; i++ {
			t := float64(i) / float64(burstLen)
			buf[pos] = (genRng.Float64()*2 - 1) * math.Exp(-5*t) * burstAmp
			pos++
		}
		pos += burstGap
	}
	tailStart := pos
	for i := tailStart; i < duration; i++ {
		t := float64(i-tailStart) / float64(duration-tailStart)
		buf[i] = (genRng.Float64()*2 - 1) * math.Exp(-8*t) * 0.3
	}
	filterBiquadBP(buf, 1500, 2.0)
	normalizePeak(buf, 0.9)
	return buf
}
