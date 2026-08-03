package audio

import (
	"math"
	"math/rand/v2"
	"slices"
)

// SFXParams is embedder-supplied per-sound shaping applied at render time.
// Zero fields mean "unmodified". Length is ignored by specs marked
// fixed_length, which is now a property of the spec rather than of which
// generator function happened to use the multiplier.
type SFXParams struct {
	Pitch  float64 `toml:"pitch,omitempty"`
	Length float64 `toml:"length,omitempty"`
}

func norm(v float64) float64 {
	if v <= 0 {
		return 1.0
	}
	return v
}

// Deterministic per-variant deviation walk (peak-to-peak).
const (
	SFXPitchWalk = 0.10
	SFXDecayWalk = 0.24
)

type variance struct{ pitch, length float64 }

// Shaping bounds. ValidateSound bounds the spec; it cannot see SFXParams,
// which arrive from the embedder at render time. Clamping the composed value
// is what makes checkFreq's Nyquist guarantee actually hold, and what stops a
// stray Length from allocating a multi-minute buffer during Start.
const (
	minPitchScale  = 0.5
	minLengthScale = 0.05
	maxLengthScale = 4.0
)

func clampScale(v, lo, hi float64) float64 {
	switch {
	case !finite(v):
		return 1.0
	case v < lo:
		return lo
	case v > hi:
		return hi
	}
	return v
}

// RenderVariants renders a spec's variant set under embedder shaping.
// Each render draws from an rng seeded by (name, variant index), so noise
// realizations are stable regardless of registration order: overriding or
// inserting a sound cannot perturb any other sound's timbre.
func RenderVariants(d *SoundDef, p SFXParams) []floatBuffer {
	n := d.Variants
	if n < 1 {
		n = SFXVariants
	}
	pw, lw := d.PitchWalk, d.LengthWalk
	if pw == 0 {
		pw = SFXPitchWalk
	}
	if lw == 0 {
		lw = SFXDecayWalk
	}
	bp, bl := norm(p.Pitch), norm(p.Length)
	if d.FixedLength {
		bl, lw = 1.0, 0
	}
	seed := fnv64a(d.Name)
	out := make([]floatBuffer, 0, n)
	for i := range n {
		f := float64(i)/float64(n) - 0.5
		v := variance{
			pitch:  clampScale(bp*(1+pw*f), minPitchScale, maxPitchScale),
			length: clampScale(bl*(1+lw*f), minLengthScale, maxLengthScale),
		}
		out = append(out, renderSound(d, v, rand.New(rand.NewPCG(seed, uint64(i)+1))))
	}
	return out
}

// RenderPreview renders one take at unity variance, bypassing the registry and
// the variant walk. This is the editor's per-keystroke path: a 200ms spec is
// ~9k samples through a handful of O(N) passes, well under a millisecond.
// Validation runs first, so an in-editor mutation that would produce non-finite
// samples is reported rather than played.
//
// The result is the canonical sound, not variant 0 — the variant walk deviates
// from it in both directions. Use RenderVariants to audition the set.
func RenderPreview(d *SoundDef, p SFXParams) ([]float64, error) {
	if err := ValidateSound(d); err != nil {
		return nil, err
	}
	bp, bl := norm(p.Pitch), norm(p.Length)
	if d.FixedLength {
		bl = 1.0
	}
	v := variance{
		pitch:  clampScale(bp, minPitchScale, maxPitchScale),
		length: clampScale(bl, minLengthScale, maxLengthScale),
	}
	return renderSound(d, v, rand.New(rand.NewPCG(fnv64a(d.Name), 1))), nil
}

// renderSound is total: ValidateSound has already excluded every input that
// could fail. It runs on the wiring goroutine, before the mixer exists.
func renderSound(d *SoundDef, v variance, rng *rand.Rand) floatBuffer {
	ts := v.length
	total := samplesOf(d.totalSeconds() * ts)
	master := make(floatBuffer, total)

	buses := make(map[string]floatBuffer, len(d.Bus))
	for i := range d.Bus {
		buses[d.Bus[i].Name] = make(floatBuffer, total)
	}
	named := make(map[string]floatBuffer, len(d.Layer))

	for i := range d.Layer {
		l := &d.Layer[i]
		off := samplesOf(l.Offset * ts)
		n := total - off
		if l.Length > 0 {
			n = min(samplesOf(l.Length*ts), total-off)
		}
		if n <= 0 {
			continue
		}
		buf := renderSource(&l.Source, n, v, ts, rng, named)
		applyChain(buf, l.Chain, v, ts)
		if l.Name != "" {
			named[l.Name] = buf // aliased read-only by mixAt and later refs
		}
		mixAt(busOf(master, buses, l.Bus), buf, off, gainOf(l.Gain))
	}

	for _, bi := range busOrder(d) {
		b := &d.Bus[bi]
		buf := buses[b.Name]
		applyChain(buf, b.Chain, v, ts)
		mixAt(busOf(master, buses, b.To), buf, samplesOf(b.Offset*ts), gainOf(b.Gain))
	}

	applyChain(master, d.Chain, v, ts)
	sanitize(master)
	if !d.Raw {
		normalizePeak(master, normOf(d.Norm))
	}
	return master
}

func busOf(master floatBuffer, buses map[string]floatBuffer, name string) floatBuffer {
	if name == "" {
		return master
	}
	return buses[name]
}

// busOrder returns bus indices deepest-first, so a bus is flushed into its
// target only after everything feeding it has been flushed into it.
func busOrder(d *SoundDef) []int {
	idx := make(map[string]int, len(d.Bus))
	for i := range d.Bus {
		idx[d.Bus[i].Name] = i
	}
	depth := make([]int, len(d.Bus))
	for i := range d.Bus {
		cur := i
		for d.Bus[cur].To != "" {
			cur = idx[d.Bus[cur].To]
			depth[i]++
		}
	}
	order := make([]int, len(d.Bus))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return depth[b] - depth[a] })
	return order
}

func renderSource(s *Source, n int, v variance, ts float64, rng *rand.Rand, named map[string]floatBuffer) floatBuffer {
	buf := make(floatBuffer, n)
	switch s.Kind {
	case "osc":
		renderOsc(buf, waveOf(s.Wave), s.Freq*v.pitch, s.Freq*v.pitch, "", 0, s.Vibrato, rng)
	case "sweep":
		end := s.FreqEnd
		if end <= 0 {
			end = s.Freq
		}
		renderOsc(buf, waveOf(s.Wave), s.Freq*v.pitch, end*v.pitch, s.Curve, s.CurveK, s.Vibrato, rng)
	case "fm":
		mod := s.ModFreq
		if mod <= 0 {
			mod = s.Freq * s.Ratio
		}
		renderFM(buf, s.Freq*v.pitch, mod*v.pitch, s.Index, s.IndexEnd, s.IndexCurve)
	case "noise":
		for i := range buf {
			buf[i] = rng.Float64()*2 - 1
		}
	case "impulse":
		for i := range buf {
			if rng.Float64() < s.Density {
				if rng.Float64() > 0.5 {
					buf[i] = 1.0
				} else {
					buf[i] = -1.0
				}
			}
		}
	case "burst":
		renderBurst(buf, s.Burst, ts, rng)
	case "ref":
		copy(buf, named[s.Ref])
	case "silence":
	}
	return buf
}

// renderOsc generates one partial. f0 == f1 is a static oscillator; otherwise
// the frequency glides, linearly or along end + (start-end)*exp(-k*t).
// Vibrato multiplies the instantaneous frequency, so partials expressed as
// separate layers share one modulation.
func renderOsc(buf floatBuffer, wave int, f0, f1 float64, curve string, k float64, vib *LFO, rng *rand.Rand) {
	sr := float64(AudioSampleRate)
	n := float64(len(buf))
	if k <= 0 {
		k = 8
	}
	var phase, lfo, lfoInc float64
	if vib != nil {
		lfoInc = vib.Rate / sr
	}
	for i := range buf {
		t := float64(i) / n
		var f float64
		switch {
		case f0 == f1:
			f = f0
		case curve == "exp":
			f = f1 + (f0-f1)*math.Exp(-k*t)
		default:
			f = f0 + (f1-f0)*t
		}
		if vib != nil {
			f *= 1 + vib.Depth*math.Sin(2*math.Pi*(lfo+vib.Phase))
			if lfo += lfoInc; lfo >= 1 {
				lfo -= 1
			}
		}
		buf[i] = waveSample(wave, phase, rng)
		if phase += f / sr; phase >= 1 {
			phase -= 1
		}
	}
}

// renderFM is 2-operator FM with an index envelope: a constant index when
// curve is empty, otherwise index -> indexEnd along the named shape.
// The quadratic form is the metal-hit spectrum: rich at onset, pure at tail.
func renderFM(buf floatBuffer, carrier, mod, idx, idxEnd float64, curve string) {
	sr := float64(AudioSampleRate)
	n := float64(len(buf))
	cInc, mInc := carrier/sr, mod/sr
	var cp, mp float64
	for i := range buf {
		k := idx
		if curve != "" {
			k = idx + (idxEnd-idx)*shapeCurve(curve, float64(i)/n)
		}
		buf[i] = math.Sin(2 * math.Pi * (cp + k*math.Sin(2*math.Pi*mp)))
		if cp += cInc; cp >= 1 {
			cp -= 1
		}
		if mp += mInc; mp >= 1 {
			mp -= 1
		}
	}
}

func renderBurst(buf floatBuffer, b *Burst, ts float64, rng *rand.Rand) {
	sr := float64(AudioSampleRate)
	ln := samplesOf(b.Len * ts)
	gp := int(b.Gap * ts * sr)
	tau := b.Tau * ts * sr
	pos := 0
	for k := 0; k < b.Count && pos < len(buf); k++ {
		amp := 1 - float64(k)*b.Decay
		if b.AmpJitter > 0 {
			amp *= 1 - b.AmpJitter*rng.Float64()
		}
		if amp < 0 {
			amp = 0
		}
		for i := 0; i < ln && pos < len(buf); i++ {
			e := 1.0
			if tau > 0 {
				e = math.Exp(-float64(i) / tau)
			}
			buf[pos] = (rng.Float64()*2 - 1) * e * amp
			pos++
		}
		g := gp
		if b.Jitter > 0 {
			g += int(float64(gp) * b.Jitter * (rng.Float64()*2 - 1))
			if g < 0 {
				g = 0
			}
		}
		pos += g
	}
}

// applyChain runs processors in order, in place. Time parameters scale with
// the length variance; frequencies scale with pitch only where track_pitch
// says so; modulation rates never scale, matching the fixed buzz rates the
// hand-written presets used.
func applyChain(buf floatBuffer, chain []Proc, v variance, ts float64) {
	sr := float64(AudioSampleRate)
	for i := range chain {
		p := &chain[i]
		pf := 1.0
		if p.TrackPitch {
			pf = v.pitch
		}
		q := p.Q
		if q <= 0 {
			q = 0.707
		}
		switch p.Kind {
		case "lp":
			filterBiquadLP(buf, p.Freq*pf, q)
		case "hp":
			filterBiquadHP(buf, p.Freq*pf, q)
		case "bp":
			filterBiquadBP(buf, p.Freq*pf, q)
		case "sweepbp":
			end := p.FreqEnd
			if end <= 0 {
				end = p.Freq
			}
			sweepBiquadBP(buf, p.Freq*pf, end*pf, q)
		case "ar":
			applyEnvelope(buf, p.Attack*ts, p.Release*ts)
		case "decay":
			applyDecayEnvelope(buf, p.Attack*ts, p.Tau*ts*sr)
		case "am":
			applyAM(buf, p.Rate, p.Depth, p.Phase)
		case "ringmod":
			applyRingMod(buf, p.Rate, p.Phase)
		case "shape":
			applyWaveshaper(buf, p.Drive)
		case "clip":
			applyOverdrive(buf, p.Drive)
		case "gain":
			g := gainOf(p.Amount)
			for j := range buf {
				buf[j] *= g
			}
		}
	}
}

func mixAt(dst, src floatBuffer, off int, g float64) {
	if off >= len(dst) {
		return
	}
	n := min(len(src), len(dst)-off)
	for i := range n {
		dst[off+i] += src[i] * g
	}
}

// sanitize scrubs non-finite samples before normalization. Validation makes
// this unreachable; it is cheap insurance against a filter corner on input
// that ultimately comes from disk.
func sanitize(b floatBuffer) {
	for i, v := range b {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			b[i] = 0
		}
	}
}

func waveSample(wave int, phase float64, rng *rand.Rand) float64 {
	switch wave {
	case waveSquare:
		if phase < 0.5 {
			return 1.0
		}
		return -1.0
	case waveSaw:
		return 2.0 * (phase - 0.5)
	case waveNoise:
		return rng.Float64()*2 - 1
	default:
		return math.Sin(2 * math.Pi * phase)
	}
}

func waveOf(s string) int {
	switch s {
	case "square":
		return waveSquare
	case "saw":
		return waveSaw
	case "noise":
		return waveNoise
	default:
		return waveSine
	}
}

func shapeCurve(name string, t float64) float64 {
	switch name {
	case "quad":
		return t * t
	case "cube":
		return t * t * t
	case "sqrt":
		return math.Sqrt(t)
	default:
		return t
	}
}

func samplesOf(sec float64) int {
	n := int(sec * float64(AudioSampleRate))
	if n < 1 {
		return 1
	}
	return n
}

func fnv64a(s string) uint64 {
	const offset64, prime64 = 14695981039346656037, 1099511628211
	h := uint64(offset64)
	for i := range len(s) {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}
