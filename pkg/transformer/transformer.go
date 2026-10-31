// Package transformer runs small transformer models over sequences of frames in
// pure Go, for inference alone: a linear input, pre-norm attention blocks with
// relative-position biases over a window of frames, a feed-forward, and named
// output heads. A weight file written by the trainer (tool/rip-train) loads into
// the same Config, and the arithmetic is float32 throughout, as the trainer's.
package transformer

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
)

// Head is one named slice of the output vector
type Head struct {
	Name string
	Size int
}

// Config is a model's shape: features in, the width, the heads and layers, the
// feed-forward width, the frames attended to either side, and the heads out.
type Config struct {
	Inputs  int
	Dim     int
	Heads   int
	Layers  int
	FFN     int
	Window  int
	Outputs []Head
}

// OutputSize is the output vector's length, the heads laid end to end
func (c Config) OutputSize() int {
	n := 0
	for _, h := range c.Outputs {
		n += h.Size
	}
	return n
}

func (c Config) Validate() error {
	switch {
	case c.Inputs < 1 || c.Dim < 1 || c.Heads < 1 || c.Layers < 0 || c.FFN < 1 || c.Window < 0:
		return errors.New("transformer: a size is not positive")
	case c.Dim%c.Heads != 0:
		return fmt.Errorf("transformer: width %d is not a multiple of %d heads", c.Dim, c.Heads)
	case c.OutputSize() < 1:
		return errors.New("transformer: no output head")
	}
	seen := map[string]bool{}
	for _, h := range c.Outputs {
		if h.Name == "" || h.Size < 1 || seen[h.Name] {
			return fmt.Errorf("transformer: output head %q of %d", h.Name, h.Size)
		}
		seen[h.Name] = true
	}
	return nil
}

// Linear is y = W x + b with W as rows of outputs, the trainer's layout
type Linear struct {
	In, Out int
	W, B    []float32
}

func newLinear(in, out int) Linear {
	return Linear{In: in, Out: out, W: make([]float32, out*in), B: make([]float32, out)}
}

// apply writes x (frames × In) through the layer into y (frames × Out)
func (l *Linear) apply(x []float32, frames int, y []float32) {
	for t := range frames {
		xt, yt := x[t*l.In:(t+1)*l.In], y[t*l.Out:(t+1)*l.Out]
		for o := range l.Out {
			w := l.W[o*l.In : (o+1)*l.In]
			s := l.B[o]
			for i, v := range xt {
				s += w[i] * v
			}
			yt[o] = s
		}
	}
}

// LayerNorm normalizes each frame over the width, then scales and shifts it
type LayerNorm struct {
	G, B []float32
}

func newLayerNorm(dim int) LayerNorm {
	ln := LayerNorm{G: make([]float32, dim), B: make([]float32, dim)}
	for i := range ln.G {
		ln.G[i] = 1
	}
	return ln
}

const layerNormEps = 1e-5

func (ln *LayerNorm) apply(x []float32, frames int, y []float32) {
	dim := len(ln.G)
	for t := range frames {
		xt, yt := x[t*dim:(t+1)*dim], y[t*dim:(t+1)*dim]
		var mean float64
		for _, v := range xt {
			mean += float64(v)
		}
		mean /= float64(dim)
		var variance float64
		for _, v := range xt {
			d := float64(v) - mean
			variance += d * d
		}
		inv := 1 / math.Sqrt(variance/float64(dim)+layerNormEps)
		for i, v := range xt {
			yt[i] = float32((float64(v)-mean)*inv)*ln.G[i] + ln.B[i]
		}
	}
}

// gelu is the exact Gaussian error linear unit, the trainer's default
func gelu(v float32) float32 {
	x := float64(v)
	return float32(0.5 * x * (1 + math.Erf(x/math.Sqrt2)))
}

type layer struct {
	ln1, ln2   LayerNorm
	q, k, v, o Linear
	ff1, ff2   Linear
}

// Model is one transformer with its weights
type Model struct {
	cfg    Config
	in     Linear
	layers []layer
	ln     LayerNorm
	out    Linear
	slopes []float32 // one relative-position slope per head
}

// New makes a model of zero weights, the shape a Load fills.
func New(cfg Config) (*Model, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	m := &Model{cfg: cfg, in: newLinear(cfg.Inputs, cfg.Dim), ln: newLayerNorm(cfg.Dim), out: newLinear(cfg.Dim, cfg.OutputSize())}
	for range cfg.Layers {
		m.layers = append(m.layers, layer{
			ln1: newLayerNorm(cfg.Dim), ln2: newLayerNorm(cfg.Dim),
			q: newLinear(cfg.Dim, cfg.Dim), k: newLinear(cfg.Dim, cfg.Dim), v: newLinear(cfg.Dim, cfg.Dim), o: newLinear(cfg.Dim, cfg.Dim),
			ff1: newLinear(cfg.Dim, cfg.FFN), ff2: newLinear(cfg.FFN, cfg.Dim),
		})
	}
	// ALiBi: head h leans on nearer frames by 2^(-8h/H) per frame of distance
	m.slopes = make([]float32, cfg.Heads)
	for h := range m.slopes {
		m.slopes[h] = float32(math.Pow(2, -8*float64(h+1)/float64(cfg.Heads)))
	}
	return m, nil
}

// NewRandom makes a model of small random weights under seed, for tests and
// for a trainer's shape check.
func NewRandom(cfg Config, seed uint64) (*Model, error) {
	m, err := New(cfg)
	if err != nil {
		return nil, err
	}
	rng := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
	for _, l := range m.linears() {
		scale := float32(1 / math.Sqrt(float64(l.In)))
		for i := range l.W {
			l.W[i] = float32(rng.NormFloat64()) * scale
		}
	}
	return m, nil
}

func (m *Model) Config() Config { return m.cfg }

// Parameters counts the weights
func (m *Model) Parameters() int {
	n := 0
	for _, t := range m.tensors() {
		n += len(t.data)
	}
	return n
}

func (m *Model) linears() []*Linear {
	ls := []*Linear{&m.in}
	for i := range m.layers {
		l := &m.layers[i]
		ls = append(ls, &l.q, &l.k, &l.v, &l.o, &l.ff1, &l.ff2)
	}
	return append(ls, &m.out)
}

// Forward runs frames, each of Inputs features, through the model and returns
// one output vector per frame, the heads laid end to end as Config.Outputs.
func (m *Model) Forward(frames [][]float32) [][]float32 {
	T, cfg := len(frames), m.cfg
	if T == 0 {
		return nil
	}
	x := make([]float32, T*cfg.Inputs)
	for t, f := range frames {
		copy(x[t*cfg.Inputs:], f)
	}
	h := make([]float32, T*cfg.Dim)
	m.in.apply(x, T, h)
	n := make([]float32, T*cfg.Dim)
	q, k, v, att := make([]float32, T*cfg.Dim), make([]float32, T*cfg.Dim), make([]float32, T*cfg.Dim), make([]float32, T*cfg.Dim)
	o := make([]float32, T*cfg.Dim)
	ff := make([]float32, T*cfg.FFN)
	for i := range m.layers {
		l := &m.layers[i]
		l.ln1.apply(h, T, n)
		l.q.apply(n, T, q)
		l.k.apply(n, T, k)
		l.v.apply(n, T, v)
		m.attend(q, k, v, T, att)
		l.o.apply(att, T, o)
		for j := range h {
			h[j] += o[j]
		}
		l.ln2.apply(h, T, n)
		l.ff1.apply(n, T, ff)
		for j := range ff {
			ff[j] = gelu(ff[j])
		}
		l.ff2.apply(ff, T, o)
		for j := range h {
			h[j] += o[j]
		}
	}
	m.ln.apply(h, T, n)
	y := make([]float32, T*cfg.OutputSize())
	m.out.apply(n, T, y)
	out := make([][]float32, T)
	for t := range out {
		out[t] = y[t*cfg.OutputSize() : (t+1)*cfg.OutputSize()]
	}
	return out
}

// attend is multi-head attention over the window: each frame weighs the frames
// within Window of it by their keys' match to its query, less the head's slope
// per frame of distance, and sums their values.
func (m *Model) attend(q, k, v []float32, T int, out []float32) {
	cfg := m.cfg
	dh := cfg.Dim / cfg.Heads
	scale := float32(1 / math.Sqrt(float64(dh)))
	scores := make([]float32, 2*cfg.Window+1)
	for h := range cfg.Heads {
		off := h * dh
		for i := range T {
			lo, hi := max(i-cfg.Window, 0), min(i+cfg.Window, T-1)
			qi := q[i*cfg.Dim+off : i*cfg.Dim+off+dh]
			var peak float32 = -math.MaxFloat32
			for j := lo; j <= hi; j++ {
				kj := k[j*cfg.Dim+off : j*cfg.Dim+off+dh]
				var s float32
				for d := range dh {
					s += qi[d] * kj[d]
				}
				d := i - j
				if d < 0 {
					d = -d
				}
				s = s*scale - m.slopes[h]*float32(d)
				scores[j-lo] = s
				peak = max(peak, s)
			}
			var sum float32
			for j := lo; j <= hi; j++ {
				e := float32(math.Exp(float64(scores[j-lo] - peak)))
				scores[j-lo] = e
				sum += e
			}
			oi := out[i*cfg.Dim+off : i*cfg.Dim+off+dh]
			clear(oi)
			for j := lo; j <= hi; j++ {
				w := scores[j-lo] / sum
				vj := v[j*cfg.Dim+off : j*cfg.Dim+off+dh]
				for d := range dh {
					oi[d] += w * vj[d]
				}
			}
		}
	}
}

// Slice is the part of a Forward output that one head wrote, nil for a head
// the model has not.
func (m *Model) Slice(out []float32, head string) []float32 {
	at := 0
	for _, h := range m.cfg.Outputs {
		if h.Name == head {
			return out[at : at+h.Size]
		}
		at += h.Size
	}
	return nil
}
