package audio

import (
	"errors"
	"math"
	"slices"
)

// RipSound is a recording as Fidelity hears it: its frames through a front end,
// its log-mel bands and its beats, heard once and compared as often as needed.
type RipSound struct {
	Frames *RipFrames
	Mel    [][]float32
	Beats  []float64  // seconds, the first bar's downbeat through the last bar's end
	floors [3]float64 // the mel, salience and onset levels under which a frame is quiet
}

// Hear is a recording as the Ripper's front end reads it, the frames a rip
// decodes; the defaults stand in for zero parameters.
func (rp Ripper) Hear(x []float64, rate int) (*RipFrames, error) {
	if rate <= 0 || len(x) == 0 {
		return nil, errors.New("rip: nothing to hear")
	}
	if len(x) > ripMaxSec*rate {
		x = x[:ripMaxSec*rate]
	}
	fe := rp.FrontEnd
	if fe == nil {
		fe = analysis{}
	}
	return fe.Hear(x, rate, rp.params())
}

// params is the Ripper's, or the defaults for the zero value, which would hear
// one partial and track nothing
func (rp Ripper) params() RipParams {
	if rp.Params == (RipParams{}) {
		return DefaultRipParams()
	}
	return rp.Params
}

// Sound hears x for Fidelity, on the beats given in seconds, a rip's, or on the
// Ripper's Grid when beats is nil, as a render of the engine's own is; one or
// the other says where the bars fall.
func (rp Ripper) Sound(x []float64, rate int, beats []float64) (*RipSound, error) {
	if beats == nil && rp.Grid == nil {
		return nil, errors.New("rip: a sound needs beats or a grid")
	}
	f, err := rp.Hear(x, rate)
	if err != nil {
		return nil, err
	}
	if beats == nil {
		g := f.pinned(rp.Grid)
		beats = make([]float64, BeatsPerBar*g.bars+1)
		for i := range beats {
			beats[i] = float64(g.beats[g.first+i]) * f.Hop
		}
	}
	return &RipSound{Frames: f, Mel: MelFrames(x, rate), Beats: beats}, nil
}

// RipFidelity is how much a render sounds like the recording it stands for,
// heard through one front end rather than through its rip, each part one where
// the two sound alike: Pitch the mean cosine of their pitch salience frame by
// frame, Timbre of their log-mel bands, Onset of their onset strength over each
// beat, the frames paired bar by bar on each one's own beats; Total their mean.
type RipFidelity struct {
	Pitch, Timbre, Onset, Total float64
}

// Fidelity grades got, a render, against want, the recording it stands for, over
// the bars both have. A frame counts where either sounds; where one is quiet and
// the other is not, nothing is alike there.
func Fidelity(got, want *RipSound) RipFidelity {
	var fid RipFidelity
	if got == nil || want == nil || got.Frames == nil || want.Frames == nil {
		return fid
	}
	got.floor()
	want.floor()
	beats := min(len(got.Beats), len(want.Beats)) - 1
	if beats < 1 {
		return fid
	}
	var pitch, timbre, onset float64
	var frames, counted int
	for b := range beats {
		g0, g1 := got.frame(got.Beats[b]), got.frame(got.Beats[b+1])
		w0, w1 := want.frame(want.Beats[b]), want.frame(want.Beats[b+1])
		if g1 <= g0 || w1 <= w0 {
			continue
		}
		gFlux, wFlux := make([]float64, g1-g0), make([]float64, g1-g0)
		for k := g0; k < g1; k++ {
			u := w0 + (k-g0)*(w1-w0)/(g1-g0) // want's frame at the same place in the beat
			gFlux[k-g0], wFlux[k-g0] = got.flux(k), want.flux(u)
			gm, wm := got.mel(k), want.mel(u)
			gq, wq := total(gm) > got.floors[0], total(wm) > want.floors[0]
			if !gq && !wq {
				continue
			}
			frames++
			timbre += alike(gm, wm, gq, wq)
			gs, ws := got.salience(k), want.salience(u)
			pitch += alike(gs, ws, total(gs) > got.floors[1], total(ws) > want.floors[1])
		}
		gq, wq := slices.Max(gFlux) > got.floors[2], slices.Max(wFlux) > want.floors[2]
		if gq || wq {
			counted++
			onset += alike(pooled(gFlux), pooled(wFlux), gq, wq)
		}
	}
	fid.Pitch, fid.Timbre, fid.Onset = 1, 1, 1 // nothing sounding in either is alike
	if frames > 0 {
		fid.Pitch, fid.Timbre = pitch/float64(frames), timbre/float64(frames)
	}
	if counted > 0 {
		fid.Onset = onset / float64(counted)
	}
	fid.Total = (fid.Pitch + fid.Timbre + fid.Onset) / 3
	return fid
}

// floor sets the quiet levels once: a twentieth of the ninetieth percentile of
// each measure over the sound's frames
func (s *RipSound) floor() {
	if s.floors != [3]float64{} {
		return
	}
	n := len(s.Frames.Flux)
	mel, sal := make([]float64, n), make([]float64, n)
	for t := range n {
		mel[t], sal[t] = total(s.mel(t)), total(s.salience(t))
	}
	s.floors = [3]float64{0.05 * percentile(mel, 0.9), 0.05 * percentile(sal, 0.9), 0.05 * percentile(s.Frames.Flux, 0.9)}
	for i := range s.floors {
		s.floors[i] = max(s.floors[i], 1e-9)
	}
}

func (s *RipSound) frame(sec float64) int { return int(math.Round(sec / s.Frames.Hop)) }

func (s *RipSound) mel(t int) []float32 {
	if t < 0 || t >= len(s.Mel) {
		return nil
	}
	return s.Mel[t]
}

func (s *RipSound) salience(t int) []float32 {
	if t < 0 || (t+1)*RipPitches > len(s.Frames.Salience) {
		return nil
	}
	return s.Frames.Salience[t*RipPitches : (t+1)*RipPitches]
}

func (s *RipSound) flux(t int) float64 {
	if t < 0 || t >= len(s.Frames.Flux) {
		return 0
	}
	return s.Frames.Flux[t]
}

// alike is how alike two rows are: their cosine when both sound, one when
// neither does, nothing when only one does
func alike[T float32 | float64](a, b []T, aq, bq bool) float64 {
	switch {
	case !aq && !bq:
		return 1
	case aq != bq:
		return 0
	}
	var dot, na, nb float64
	for i := range min(len(a), len(b)) {
		x, y := float64(a[i]), float64(b[i])
		dot, na, nb = dot+x*y, na+x*x, nb+y*y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}

func total[T float32 | float64](v []T) float64 {
	var s float64
	for _, x := range v {
		s += float64(x)
	}
	return s
}

// pooled is v under a three-frame maximum, so an onset a frame off still meets
// its match
func pooled(v []float64) []float64 {
	out := make([]float64, len(v))
	for i := range v {
		out[i] = v[i]
		if i > 0 {
			out[i] = max(out[i], v[i-1])
		}
		if i+1 < len(v) {
			out[i] = max(out[i], v[i+1])
		}
	}
	return out
}

// percentile is the value a share q of v lies under, zero for none
func percentile(v []float64, q float64) float64 {
	if len(v) == 0 {
		return 0
	}
	sorted := slices.Clone(v)
	slices.Sort(sorted)
	return sorted[min(int(q*float64(len(sorted))), len(sorted)-1)]
}
