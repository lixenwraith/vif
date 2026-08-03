package audio

import (
	"fmt"
	"math"
	"strings"
)

// ValidateSound rejects anything that could fail, stall, or produce non-finite
// samples at render time. Rendering is total: it runs on the wiring goroutine
// before the mixer exists and has no error path.
func ValidateSound(d *SoundDef) error {
	if d == nil {
		return fmt.Errorf("sound: nil definition")
	}
	if d.Name == "" || len(d.Name) > MaxSoundNameLen || strings.ContainsAny(d.Name, "\x00\n\r\t") {
		return fmt.Errorf("sound %q: invalid name", d.Name)
	}
	e := func(f string, a ...any) error {
		return fmt.Errorf("sound %q: "+f, append([]any{d.Name}, a...)...)
	}

	if d.Variants < 0 || d.Variants > MaxSoundVariants {
		return e("variants %d outside [0,%d]", d.Variants, MaxSoundVariants)
	}
	for _, w := range []struct {
		n string
		v float64
	}{{"pitch_walk", d.PitchWalk}, {"length_walk", d.LengthWalk}} {
		if !finite(w.v) || w.v < 0 || w.v >= 2 {
			return e("%s %v outside [0,2)", w.n, w.v)
		}
	}
	if d.Raw && d.Norm != 0 {
		return e("norm set together with raw")
	}
	if !d.Raw && (!finite(d.Norm) || d.Norm < 0 || d.Norm > 1) {
		return e("norm %v outside [0,1]", d.Norm)
	}
	if n := len(d.Layer); n == 0 || n > MaxSoundLayers {
		return e("%d layers, want 1..%d", n, MaxSoundLayers)
	}
	if len(d.Bus) > MaxSoundBuses {
		return e("%d buses, want at most %d", len(d.Bus), MaxSoundBuses)
	}

	if d.Duration == 0 {
		for i := range d.Layer {
			if d.Layer[i].Length <= 0 {
				return e("layer %d: length required when the sound omits duration", i)
			}
		}
	}
	dur := d.totalSeconds()
	if !finite(dur) || dur <= 0 || dur > MaxSoundDuration {
		return e("duration %v outside (0,%v]", dur, MaxSoundDuration)
	}

	buses := make(map[string]int, len(d.Bus))
	for i := range d.Bus {
		b := &d.Bus[i]
		if b.Name == "" {
			return e("bus %d: empty name", i)
		}
		if _, dup := buses[b.Name]; dup {
			return e("bus %q: duplicate", b.Name)
		}
		buses[b.Name] = i
	}
	for i := range d.Bus {
		b := &d.Bus[i]
		if b.To != "" {
			if b.To == b.Name {
				return e("bus %q: targets itself", b.Name)
			}
			if _, ok := buses[b.To]; !ok {
				return e("bus %q: unknown target %q", b.Name, b.To)
			}
		}
		if err := checkMix(b.Gain, b.Offset, dur); err != nil {
			return e("bus %q: %w", b.Name, err)
		}
		if err := checkChain(b.Chain); err != nil {
			return e("bus %q: %w", b.Name, err)
		}
	}
	// Bus tree acyclicity: a walk longer than the bus count implies a cycle.
	for i := range d.Bus {
		cur, n := i, 0
		for d.Bus[cur].To != "" {
			cur = buses[d.Bus[cur].To]
			if n++; n > len(d.Bus) {
				return e("bus %q: cycle in bus routing", d.Bus[i].Name)
			}
		}
	}

	var ops float64
	named := make(map[string]bool, len(d.Layer))
	for i := range d.Layer {
		l := &d.Layer[i]
		if l.Bus != "" {
			if _, ok := buses[l.Bus]; !ok {
				return e("layer %d: unknown bus %q", i, l.Bus)
			}
		}
		if l.Name != "" {
			if named[l.Name] {
				return e("layer %d: duplicate name %q", i, l.Name)
			}
			named[l.Name] = true
		}
		if err := checkMix(l.Gain, l.Offset, dur); err != nil {
			return e("layer %d: %w", i, err)
		}
		if !finite(l.Length) || l.Length < 0 || l.Length > dur {
			return e("layer %d: length %v outside [0,%v]", i, l.Length, dur)
		}
		if err := checkSource(&l.Source, named); err != nil {
			return e("layer %d: %w", i, err)
		}
		if err := checkChain(l.Chain); err != nil {
			return e("layer %d: %w", i, err)
		}
		span := l.Length
		if span == 0 {
			span = dur - l.Offset
		}
		ops += span * float64(AudioSampleRate) * float64(1+len(l.Chain))
	}
	if err := checkChain(d.Chain); err != nil {
		return e("master chain: %w", err)
	}

	n := d.Variants
	if n < 1 {
		n = SFXVariants
	}
	ops = (ops + dur*float64(AudioSampleRate)*float64(1+len(d.Chain))) * float64(n)
	if ops > maxRenderOps {
		return e("render cost %.0f sample-ops exceeds %d", ops, maxRenderOps)
	}
	return nil
}

func checkMix(gain, offset, dur float64) error {
	if !finite(gain) || gain < 0 || gain > 64 {
		return fmt.Errorf("gain %v outside [0,64]", gain)
	}
	if !finite(offset) || offset < 0 || offset >= dur {
		return fmt.Errorf("offset %v outside [0,%v)", offset, dur)
	}
	return nil
}

func checkSource(s *Source, named map[string]bool) error {
	switch s.Kind {
	case "osc", "sweep":
		if err := checkWave(s.Wave); err != nil {
			return err
		}
		if err := checkFreq("freq", s.Freq); err != nil {
			return err
		}
		if s.Kind == "sweep" {
			if s.FreqEnd != 0 {
				if err := checkFreq("freq_end", s.FreqEnd); err != nil {
					return err
				}
			}
			switch s.Curve {
			case "", "lin", "exp":
			default:
				return fmt.Errorf("unknown sweep curve %q", s.Curve)
			}
			if !finite(s.CurveK) || s.CurveK < 0 || s.CurveK > 64 {
				return fmt.Errorf("curve_k %v outside [0,64]", s.CurveK)
			}
		}
	case "fm":
		if err := checkFreq("freq", s.Freq); err != nil {
			return err
		}
		if s.ModFreq != 0 {
			if err := checkFreq("mod_freq", s.ModFreq); err != nil {
				return err
			}
		} else if !finite(s.Ratio) || s.Ratio <= 0 || s.Ratio*s.Freq >= nyquist {
			return fmt.Errorf("fm needs mod_freq or a ratio yielding < %.0f Hz", nyquist)
		}
		if !finite(s.Index) || s.Index < 0 || s.Index > 64 {
			return fmt.Errorf("index %v outside [0,64]", s.Index)
		}
		switch s.IndexCurve {
		case "", "lin", "quad", "cube", "sqrt":
		default:
			return fmt.Errorf("unknown index_curve %q", s.IndexCurve)
		}
		if s.IndexCurve != "" && (!finite(s.IndexEnd) || s.IndexEnd < 0 || s.IndexEnd > 64) {
			return fmt.Errorf("index_end %v outside [0,64]", s.IndexEnd)
		}
	case "noise", "silence":
	case "impulse":
		if !finite(s.Density) || s.Density <= 0 || s.Density > 1 {
			return fmt.Errorf("density %v outside (0,1]", s.Density)
		}
	case "burst":
		b := s.Burst
		if b == nil {
			return fmt.Errorf("burst source needs a burst table")
		}
		if b.Count < 1 || b.Count > 256 {
			return fmt.Errorf("burst count %d outside [1,256]", b.Count)
		}
		if !finite(b.Len) || b.Len <= 0 || b.Len > MaxSoundDuration {
			return fmt.Errorf("burst len %v outside (0,%v]", b.Len, MaxSoundDuration)
		}
		for _, f := range []struct {
			n string
			v float64
		}{{"gap", b.Gap}, {"tau", b.Tau}} {
			if !finite(f.v) || f.v < 0 || f.v > MaxSoundDuration {
				return fmt.Errorf("burst %s %v outside [0,%v]", f.n, f.v, MaxSoundDuration)
			}
		}
		for _, f := range []struct {
			n string
			v float64
		}{{"decay", b.Decay}, {"jitter", b.Jitter}, {"amp_jitter", b.AmpJitter}} {
			if !finite(f.v) || f.v < 0 || f.v > 1 {
				return fmt.Errorf("burst %s %v outside [0,1]", f.n, f.v)
			}
		}
	case "ref":
		if !named[s.Ref] {
			return fmt.Errorf("ref %q: no earlier named layer", s.Ref)
		}
	default:
		return fmt.Errorf("unknown source kind %q", s.Kind)
	}

	if s.Ref != "" && s.Kind != "ref" {
		return fmt.Errorf("ref set on %q source", s.Kind)
	}
	if s.Burst != nil && s.Kind != "burst" {
		return fmt.Errorf("burst table set on %q source", s.Kind)
	}
	if s.Vibrato != nil {
		if s.Kind != "osc" && s.Kind != "sweep" {
			return fmt.Errorf("vibrato set on %q source", s.Kind)
		}
		if err := checkLFO(s.Vibrato); err != nil {
			return err
		}
	}
	return nil
}

func checkChain(c []Proc) error {
	if len(c) > MaxSoundChain {
		return fmt.Errorf("%d processors, want at most %d", len(c), MaxSoundChain)
	}
	for i := range c {
		if err := checkProc(&c[i]); err != nil {
			return fmt.Errorf("proc %d: %w", i, err)
		}
	}
	return nil
}

func checkProc(p *Proc) error {
	switch p.Kind {
	case "lp", "hp", "bp", "sweepbp":
		if err := checkFreq("freq", p.Freq); err != nil {
			return err
		}
		if p.Kind == "sweepbp" && p.FreqEnd != 0 {
			if err := checkFreq("freq_end", p.FreqEnd); err != nil {
				return err
			}
		}
		if !finite(p.Q) || p.Q < 0 || p.Q > 64 {
			return fmt.Errorf("q %v outside [0,64]", p.Q)
		}
	case "ar":
		for _, f := range []struct {
			n string
			v float64
		}{{"attack", p.Attack}, {"release", p.Release}} {
			if !finite(f.v) || f.v < 0 || f.v > MaxSoundDuration {
				return fmt.Errorf("%s %v outside [0,%v]", f.n, f.v, MaxSoundDuration)
			}
		}
	case "decay":
		if !finite(p.Attack) || p.Attack < 0 || p.Attack > MaxSoundDuration {
			return fmt.Errorf("attack %v outside [0,%v]", p.Attack, MaxSoundDuration)
		}
		if !finite(p.Tau) || p.Tau <= 0 || p.Tau > MaxSoundDuration {
			return fmt.Errorf("tau %v outside (0,%v]", p.Tau, MaxSoundDuration)
		}
	case "am", "ringmod":
		if !finite(p.Rate) || p.Rate <= 0 || p.Rate >= nyquist {
			return fmt.Errorf("rate %v outside (0,%.0f)", p.Rate, nyquist)
		}
		if p.Kind == "am" && (!finite(p.Depth) || p.Depth < 0 || p.Depth > 1) {
			return fmt.Errorf("depth %v outside [0,1]", p.Depth)
		}
		if !finite(p.Phase) || p.Phase < 0 || p.Phase > 1 {
			return fmt.Errorf("phase %v outside [0,1]", p.Phase)
		}
	case "shape", "clip":
		if !finite(p.Drive) || p.Drive <= 0 || p.Drive > 64 {
			return fmt.Errorf("drive %v outside (0,64]", p.Drive)
		}
	case "gain":
		if !finite(p.Amount) || p.Amount < 0 || p.Amount > 64 {
			return fmt.Errorf("amount %v outside [0,64]", p.Amount)
		}
	default:
		return fmt.Errorf("unknown proc kind %q", p.Kind)
	}
	return nil
}

func checkLFO(l *LFO) error {
	if !finite(l.Rate) || l.Rate < 0 || l.Rate >= nyquist {
		return fmt.Errorf("vibrato rate %v outside [0,%.0f)", l.Rate, nyquist)
	}
	if !finite(l.Depth) || l.Depth < 0 || l.Depth > 1 {
		return fmt.Errorf("vibrato depth %v outside [0,1]", l.Depth)
	}
	if !finite(l.Phase) || l.Phase < 0 || l.Phase > 1 {
		return fmt.Errorf("vibrato phase %v outside [0,1]", l.Phase)
	}
	return nil
}

func checkWave(w string) error {
	switch w {
	case "", "sine", "square", "saw", "noise":
		return nil
	}
	return fmt.Errorf("unknown wave %q", w)
}

// checkFreq keeps every frequency below Nyquist after the widest pitch walk,
// so no biquad can be handed a cutoff that produces NaN.
func checkFreq(name string, f float64) error {
	if !finite(f) || f <= 0 || f*maxPitchScale >= nyquist {
		return fmt.Errorf("%s %v outside (0,%.0f) after pitch scaling", name, f, nyquist/maxPitchScale)
	}
	return nil
}

const (
	nyquist       = float64(AudioSampleRate) / 2
	maxPitchScale = 2.0
)

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
