package ripmodel

import (
	"bytes"
	"math"
	"testing"

	"github.com/lixenwraith/audio"
	"github.com/lixenwraith/transformer"
)

// TestAnUntrainedModelHearsFramesTheRipReads is the contract between the model
// and the decoders: a model of Spec's shape, however weighted, yields frames on
// the rip's grid with every field the rip reads, and the rip runs on them; one
// from before the strike, release or velocity head loads without them.
func TestAnUntrainedModelHearsFramesTheRipReads(t *testing.T) {
	cfg := Spec()
	cfg.Dim, cfg.Heads, cfg.Layers, cfg.FFN, cfg.Window = 16, 2, 1, 32, 8 // a small one for the test's sake
	m, err := transformer.NewRandom(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := m.Save(&b); err != nil {
		t.Fatal(err)
	}
	fe, err := Load(&b)
	if err != nil {
		t.Fatal(err)
	}
	x := make([]float64, 5*audio.AudioSampleRate)
	for i := range x {
		x[i] = 0.3 * math.Sin(float64(i)*2*math.Pi*220/audio.AudioSampleRate)
	}
	f, err := fe.Hear(x, audio.AudioSampleRate, audio.DefaultRipParams())
	if err != nil {
		t.Fatal(err)
	}
	n := len(f.Flux)
	per := n * audio.RipPitches
	if n < 400 || len(f.Salience) != per || len(f.Strike) != per || len(f.Release) != per || len(f.Velocity) != per ||
		len(f.Voice) != n*audio.VoiceGeneCount || len(f.Wave) != n*len(audio.VoiceWaves) || len(f.Onset[2]) != n {
		t.Fatalf("frames %d, salience %d, strikes %d, releases %d, velocities %d, voice %d, wave %d",
			n, len(f.Salience), len(f.Strike), len(f.Release), len(f.Velocity), len(f.Voice), len(f.Wave))
	}
	if _, err := (audio.Ripper{Params: audio.DefaultRipParams(), FrontEnd: fe}).Samples(x, audio.AudioSampleRate); err != nil {
		t.Errorf("the rip refused the model's frames: %v", err)
	}
	for lack := 1; lack <= optionalHeads; lack++ {
		older := cfg
		older.Outputs = older.Outputs[:len(older.Outputs)-lack]
		o, _ := transformer.NewRandom(older, 1)
		ofe, err := New(o)
		if err != nil {
			t.Errorf("a model without its last %d heads: %v", lack, err)
			continue
		}
		of, err := ofe.Hear(x, audio.AudioSampleRate, audio.DefaultRipParams())
		if err != nil || of.Velocity != nil || (of.Release != nil) != (lack < 2) || (of.Strike != nil) != (lack < 3) {
			t.Errorf("a model without its last %d heads heard strikes %v, releases %v, velocities %v: %v", lack, of.Strike != nil, of.Release != nil, of.Velocity != nil, err)
		}
	}
	wrong := cfg
	wrong.Outputs = wrong.Outputs[:3]
	w, _ := transformer.NewRandom(wrong, 1)
	if _, err := New(w); err == nil {
		t.Error("a model without the voice heads passed")
	}
}

// TestAGainMovesOnlyTheLevel: the model's input keeps a frame's bands level-free,
// so the same sound 20 dB softer differs only in the level input, by 20 dB, while
// it stays well over the floor.
func TestAGainMovesOnlyTheLevel(t *testing.T) {
	loud, soft := make([]float32, audio.RipMelBands), make([]float32, audio.RipMelBands)
	for b := range loud {
		e := math.Pow(10, -float64(b%5)) // bands from full scale to 40 dB down
		loud[b], soft[b] = float32(math.Log1p(100*e)), float32(math.Log1p(100*e/100))
	}
	f := features([][]float32{loud, soft})
	for b := range audio.RipMelBands {
		if d := math.Abs(float64(f[0][b] - f[1][b])); d > 2e-3 {
			t.Fatalf("band %d moved by %.4f with the gain", b, d)
		}
	}
	if d := float64(f[0][audio.RipMelBands]-f[1][audio.RipMelBands]) * 80; math.Abs(d-20) > 0.1 {
		t.Errorf("the level input moved %.2f dB for a 20 dB gain", d)
	}
}
