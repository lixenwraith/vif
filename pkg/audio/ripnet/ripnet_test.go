package ripnet

import (
	"bytes"
	"math"
	"testing"

	"github.com/lixenwraith/vif/pkg/audio"
	"github.com/lixenwraith/vif/pkg/transformer"
)

// TestAnUntrainedModelHearsFramesTheRipReads is the contract between the model
// and the decoders: a model of Spec's shape, however weighted, yields frames on
// the rip's grid with every field the rip reads, and the rip runs on them.
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
	if n < 400 || len(f.Salience) != n*audio.RipPitches || len(f.Voice) != n*audio.VoiceGeneCount || len(f.Wave) != n*len(audio.VoiceWaves) || len(f.Onset[2]) != n {
		t.Fatalf("frames %d, salience %d, voice %d, wave %d", n, len(f.Salience), len(f.Voice), len(f.Wave))
	}
	if _, err := (audio.Ripper{Params: audio.DefaultRipParams(), FrontEnd: fe}).Samples(x, audio.AudioSampleRate); err != nil {
		t.Errorf("the rip refused the model's frames: %v", err)
	}
	wrong := cfg
	wrong.Outputs = wrong.Outputs[:3]
	w, _ := transformer.NewRandom(wrong, 1)
	if _, err := New(w); err == nil {
		t.Error("a model without the voice heads passed")
	}
}
