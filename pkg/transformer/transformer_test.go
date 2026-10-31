package transformer

import (
	"bytes"
	"math"
	"testing"
)

func tiny() Config {
	return Config{Inputs: 5, Dim: 8, Heads: 2, Layers: 2, FFN: 16, Window: 3,
		Outputs: []Head{{"a", 3}, {"b", 2}}}
}

func frames(T, n int, seed uint64) [][]float32 {
	m, _ := NewRandom(Config{Inputs: 1, Dim: n, Heads: 1, Layers: 0, FFN: 1, Window: 0, Outputs: []Head{{"x", 1}}}, seed)
	out := make([][]float32, T)
	for t := range out {
		out[t] = make([]float32, n)
		for i := range out[t] {
			out[t][i] = m.in.W[(i+t*n)%len(m.in.W)]
		}
	}
	return out
}

// TestAttentionMatchesTheDenseReference checks the windowed heads against a
// plain float64 attention over the same window, slopes included.
func TestAttentionMatchesTheDenseReference(t *testing.T) {
	cfg := tiny()
	m, err := NewRandom(cfg, 7)
	if err != nil {
		t.Fatal(err)
	}
	const T = 9
	q, k, v := frames(T, cfg.Dim, 1), frames(T, cfg.Dim, 2), frames(T, cfg.Dim, 3)
	flat := func(f [][]float32) []float32 {
		var out []float32
		for _, r := range f {
			out = append(out, r...)
		}
		return out
	}
	got := make([]float32, T*cfg.Dim)
	m.attend(flat(q), flat(k), flat(v), T, got)
	dh := cfg.Dim / cfg.Heads
	for h := range cfg.Heads {
		for i := range T {
			var weights []float64
			var js []int
			for j := range T {
				if d := i - j; d > cfg.Window || d < -cfg.Window {
					continue
				}
				var s float64
				for d := range dh {
					s += float64(q[i][h*dh+d]) * float64(k[j][h*dh+d])
				}
				s = s/math.Sqrt(float64(dh)) - float64(m.slopes[h])*math.Abs(float64(i-j))
				weights, js = append(weights, s), append(js, j)
			}
			peak := math.Inf(-1)
			for _, w := range weights {
				peak = max(peak, w)
			}
			var sum float64
			for n, w := range weights {
				weights[n] = math.Exp(w - peak)
				sum += weights[n]
			}
			for d := range dh {
				var want float64
				for n, j := range js {
					want += weights[n] / sum * float64(v[j][h*dh+d])
				}
				if diff := math.Abs(float64(got[i*cfg.Dim+h*dh+d]) - want); diff > 1e-5 {
					t.Fatalf("head %d frame %d dim %d: %g, want %g", h, i, d, got[i*cfg.Dim+h*dh+d], want)
				}
			}
		}
	}
}

// TestLayerNormAndGELU pins the two nonlinearities to their definitions.
func TestLayerNormAndGELU(t *testing.T) {
	ln := newLayerNorm(4)
	out := make([]float32, 4)
	ln.apply([]float32{1, 2, 3, 4}, 1, out)
	var mean, sq float64
	for _, v := range out {
		mean += float64(v)
		sq += float64(v) * float64(v)
	}
	if math.Abs(mean) > 1e-6 || math.Abs(sq/4-1) > 1e-3 {
		t.Errorf("layer norm gave mean %g, power %g", mean, sq/4)
	}
	if g := gelu(1); math.Abs(float64(g)-0.8413447) > 1e-6 {
		t.Errorf("gelu(1) = %g", g)
	}
	if g := gelu(-10); g != 0 && math.Abs(float64(g)) > 1e-6 {
		t.Errorf("gelu(-10) = %g", g)
	}
}

// TestWeightFileRoundTrips is the file's contract: what Save wrote, Load reads
// into the same shape with the same outputs, and a damaged file is refused.
func TestWeightFileRoundTrips(t *testing.T) {
	cfg := tiny()
	m, _ := NewRandom(cfg, 11)
	var b bytes.Buffer
	if err := m.Save(&b); err != nil {
		t.Fatal(err)
	}
	back, err := Load(bytes.NewReader(b.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if back.Parameters() != m.Parameters() || back.Config().OutputSize() != cfg.OutputSize() {
		t.Fatalf("loaded %d parameters of %d", back.Parameters(), m.Parameters())
	}
	in := frames(7, cfg.Inputs, 5)
	a, c := m.Forward(in), back.Forward(in)
	for i := range a {
		for j := range a[i] {
			if a[i][j] != c[i][j] {
				t.Fatalf("frame %d output %d differs after the round trip", i, j)
			}
		}
	}
	if sl := back.Slice(c[0], "b"); len(sl) != 2 || sl[0] != c[0][3] {
		t.Errorf("head b slice %v of %v", sl, c[0])
	}
	bad := bytes.Clone(b.Bytes())
	bad[len(bad)/2] ^= 0x55
	if _, err := Load(bytes.NewReader(bad)); err == nil {
		t.Error("a damaged file loaded")
	}
	if _, err := Load(bytes.NewReader(b.Bytes()[:len(b.Bytes())/2])); err == nil {
		t.Error("a truncated file loaded")
	}
}

// TestForwardShapesAndDeterminism: a frame per input, a head's worth per frame,
// the same twice.
func TestForwardShapesAndDeterminism(t *testing.T) {
	cfg := tiny()
	m, _ := NewRandom(cfg, 3)
	in := frames(20, cfg.Inputs, 9)
	a, b := m.Forward(in), m.Forward(in)
	if len(a) != 20 || len(a[0]) != 5 {
		t.Fatalf("shape %d × %d", len(a), len(a[0]))
	}
	for i := range a {
		for j := range a[i] {
			if a[i][j] != b[i][j] || math.IsNaN(float64(a[i][j])) {
				t.Fatalf("frame %d output %d: %g then %g", i, j, a[i][j], b[i][j])
			}
		}
	}
	if m.Forward(nil) != nil {
		t.Error("no frames gave frames")
	}
	if _, err := New(Config{Inputs: 1, Dim: 6, Heads: 4, FFN: 1, Outputs: []Head{{"x", 1}}}); err == nil {
		t.Error("a width no multiple of its heads passed")
	}
}
