package audio

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The sound and pattern registries are package globals, so these tests are
// order-dependent and must not run in parallel. Each resets what it touches.

func loadBuiltins(t *testing.T) []*SoundDef {
	t.Helper()
	defs, err := BuiltinSounds()
	if err != nil {
		t.Fatalf("builtin sounds: %v", err)
	}
	if len(defs) == 0 {
		t.Fatal("no builtin sounds embedded")
	}
	return defs
}

func checkBuffer(t *testing.T, name string, buf []float64) float64 {
	t.Helper()
	var peak float64
	for _, v := range buf {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("%s: non-finite sample", name)
		}
		if a := math.Abs(v); a > peak {
			peak = a
		}
	}
	return peak
}

func TestBuiltinSoundsRender(t *testing.T) {
	for _, d := range loadBuiltins(t) {
		buf, err := RenderPreview(d, SFXParams{})
		if err != nil {
			t.Fatalf("%s: %v", d.Name, err)
		}
		peak := checkBuffer(t, d.Name, buf)
		if peak < 0.01 {
			t.Errorf("%s: renders silent (peak %g)", d.Name, peak)
		}
		if peak > 1.0 {
			t.Errorf("%s: peak %g exceeds unity", d.Name, peak)
		}
	}
}

// A failure here usually means the encoder lost float precision, not that the
// spec model is wrong.
func TestSoundRoundTrip(t *testing.T) {
	defs := loadBuiltins(t)
	data, err := MarshalSounds(defs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	back, err := LoadSoundsTOML(data)
	if err != nil {
		t.Fatalf("reload: %v\n%s", err, data)
	}
	if len(back) != len(defs) {
		t.Fatalf("round trip produced %d sounds, want %d", len(back), len(defs))
	}
	for i := range defs {
		a, err := RenderPreview(defs[i], SFXParams{})
		if err != nil {
			t.Fatal(err)
		}
		b, err := RenderPreview(back[i], SFXParams{})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(a, b) {
			t.Errorf("%s: audio differs after round trip", defs[i].Name)
		}
	}
}

func TestStrictKeysRejectsTypo(t *testing.T) {
	_, err := LoadSoundsTOML([]byte(`
[[sound]]
name = "typo"
duration = 0.1
[[sound.layer]]
source = { kind = "osc", freq_start = 440 }
`))
	if err == nil {
		t.Fatal("unknown key accepted")
	}
	if !strings.Contains(err.Error(), "freq_start") {
		t.Errorf("error should name the offending key: %v", err)
	}
}

// SFXParams arrives from the embedder after ValidateSound has run, so the
// composed scale is what has to be clamped.
func TestShapingClamp(t *testing.T) {
	maxSamples := int(MaxSoundDuration*maxLengthScale*AudioSampleRate) + 1
	for _, d := range loadBuiltins(t) {
		for _, b := range RenderVariants(d, SFXParams{Pitch: 64, Length: 1e6}) {
			if len(b) > maxSamples {
				t.Fatalf("%s: %d samples exceeds the length clamp", d.Name, len(b))
			}
			checkBuffer(t, d.Name, b)
		}
	}
}

func TestRegistryFreezeAndReset(t *testing.T) {
	t.Cleanup(ResetRegistries)
	ResetRegistries()

	late := func() *SoundDef {
		return &SoundDef{Name: "late", Duration: 0.05, Layer: []Layer{{Source: Source{Kind: "noise"}}}}
	}
	if err := registerBuiltinSounds(); err != nil {
		t.Fatal(err)
	}
	if SoundIDByName("kick") == SoundNone {
		t.Fatal("kick unresolved after builtin registration")
	}
	freezeSounds()
	if _, err := RegisterSound(late()); err == nil {
		t.Error("frozen registry accepted a late insert")
	}
	ResetRegistries()
	if SoundIDByName("kick") != SoundNone {
		t.Error("reset did not clear the name table")
	}
	if _, err := RegisterSound(late()); err != nil {
		t.Errorf("reset did not unfreeze: %v", err)
	}
}

func TestPatternRoundTrip(t *testing.T) {
	t.Cleanup(ResetRegistries)
	ResetRegistries()
	InitDefaultPatterns()

	pats := RegisteredPatterns()
	data, err := MarshalPatterns(pats)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	back, err := LoadPatternsTOML(data)
	if err != nil {
		t.Fatalf("reload: %v\n%s", err, data)
	}
	byName := make(map[string]*Pattern, len(back))
	for _, p := range back {
		byName[p.Name] = p
	}
	for _, p := range pats {
		if p.Name == "" {
			continue
		}
		q, ok := byName[p.Name]
		if !ok {
			t.Errorf("%s: missing after round trip", p.Name)
			continue
		}
		if q.Steps != p.Steps || len(q.Tracks) != len(p.Tracks) {
			t.Errorf("%s: shape differs", p.Name)
			continue
		}
		for i := range p.Tracks {
			a, b := &p.Tracks[i], &q.Tracks[i]
			if a.Instr != b.Instr || a.FollowChord != b.FollowChord ||
				a.Humanize != b.Humanize || !slices.Equal(a.Events, b.Events) {
				t.Errorf("%s: track %d differs", p.Name, i)
			}
		}
	}
}

func TestPatternValidationRejects(t *testing.T) {
	base := func() *Pattern {
		return &Pattern{Name: "t", Steps: 16, Tracks: []Track{
			{Instr: InstrKick, Events: []Step{{Pos: 0, Vel: 1}}},
		}}
	}
	if err := ValidatePattern(base()); err != nil {
		t.Fatalf("baseline rejected: %v", err)
	}
	cases := []struct {
		name string
		mut  func(*Pattern)
	}{
		// This one panics rng.IntN on the mixer goroutine if it gets through.
		{"negative humanize", func(p *Pattern) { p.Tracks[0].Humanize = -1 }},
		{"humanize above one", func(p *Pattern) { p.Tracks[0].Humanize = 2 }},
		{"pos past step count", func(p *Pattern) { p.Tracks[0].Events[0].Pos = 16 }},
		{"pos negative", func(p *Pattern) { p.Tracks[0].Events[0].Pos = -1 }},
		{"velocity unbounded", func(p *Pattern) { p.Tracks[0].Events[0].Vel = 1e9 }},
		{"velocity NaN", func(p *Pattern) { p.Tracks[0].Events[0].Vel = math.NaN() }},
		{"prob above one", func(p *Pattern) { p.Tracks[0].Events[0].Prob = 2 }},
		{"steps above max", func(p *Pattern) { p.Steps = MaxPatternLen + 1 }},
		{"steps zero", func(p *Pattern) { p.Steps = 0 }},
		{"instrument out of range", func(p *Pattern) { p.Tracks[0].Instr = InstrumentCount }},
		{"octave out of range", func(p *Pattern) { p.Tracks[0].Events[0].Oct = 99 }},
	}
	for _, c := range cases {
		p := base()
		c.mut(p)
		if err := ValidatePattern(p); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
		if id := RegisterPattern(p); id != PatternSilence {
			t.Errorf("%s: registered as %d, want PatternSilence", c.name, id)
		}
	}
}

func TestPatternUnknownInstrument(t *testing.T) {
	_, err := LoadPatternsTOML([]byte(`
[[pattern]]
name = "p"
steps = 16
[[pattern.track]]
instr = "cowbell"
`))
	if err == nil || !strings.Contains(err.Error(), "cowbell") {
		t.Errorf("want an error naming the instrument, got %v", err)
	}
}

func TestNullBackendLifecycle(t *testing.T) {
	t.Cleanup(ResetRegistries)
	ResetRegistries()

	cfg := DefaultAudioConfig()
	cfg.Enabled = true
	cfg.ForceBackend = BackendNameNull
	ae, err := NewAudioEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := ae.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer ae.Stop()

	if ae.IsSilent() {
		t.Fatal("null backend latched silent mode")
	}
	if got := ae.BackendName(); got != BackendNameNull {
		t.Errorf("backend %q, want %q", got, BackendNameNull)
	}
	id := ae.SoundID("bell")
	if id == SoundNone {
		t.Fatal("bell unresolved")
	}
	ae.StartMusic()
	if !ae.Play(id) {
		t.Error("Play rejected on a running engine")
	}
	time.Sleep(3 * AudioBufferDuration)
	if played, _ := ae.Stats(); played == 0 {
		t.Error("mixer reported no sounds played")
	}
}

func TestWAVBackendCapture(t *testing.T) {
	t.Cleanup(ResetRegistries)
	ResetRegistries()

	path := filepath.Join(t.TempDir(), "capture.wav")
	cfg := DefaultAudioConfig()
	cfg.Enabled = true
	cfg.ForceBackend = "wav:" + path
	ae, err := NewAudioEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := ae.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	ae.Play(ae.SoundID("coin"))
	time.Sleep(3 * AudioBufferDuration)
	ae.Stop()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) <= wavHeaderSize {
		t.Fatalf("no PCM captured (%d bytes)", len(data))
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		t.Fatal("not a RIFF/WAVE file")
	}
	// Stop waits for the mix goroutine, so the patched size must be exact.
	if n := binary.LittleEndian.Uint32(data[40:44]); int(n) != len(data)-wavHeaderSize {
		t.Errorf("data chunk claims %d bytes, file has %d", n, len(data)-wavHeaderSize)
	}
}

func TestWriteWAVFraming(t *testing.T) {
	buf, err := RenderPreview(&SoundDef{
		Name:     "test_tone",
		Duration: 0.1,
		Layer:    []Layer{{Source: Source{Kind: "osc", Wave: "sine", Freq: 440}}},
	}, SFXParams{})
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := WriteWAV(&b, buf); err != nil {
		t.Fatal(err)
	}
	if want := wavHeaderSize + len(buf)*AudioBytesPerFrame; b.Len() != want {
		t.Errorf("wrote %d bytes, want %d", b.Len(), want)
	}
}

func FuzzLoadSoundsTOML(f *testing.F) {
	if defs, err := BuiltinSounds(); err == nil {
		if data, err := MarshalSounds(defs); err == nil {
			f.Add(data)
		}
	}
	f.Add([]byte("[[sound]]\nname=\"a\"\nduration=0.1\n[[sound.layer]]\nsource={kind=\"noise\"}\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		got, _ := LoadSoundsTOML(data)
		for _, d := range got {
			buf, err := RenderPreview(d, SFXParams{})
			if err != nil {
				t.Fatalf("%s: validated at load but failed to render: %v", d.Name, err)
			}
			for _, v := range buf {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					t.Fatalf("%s: non-finite sample survived validation", d.Name)
				}
			}
		}
	})
}

func FuzzLoadPatternsTOML(f *testing.F) {
	f.Add([]byte("[[pattern]]\nname=\"p\"\nsteps=16\n[[pattern.track]]\ninstr=\"kick\"\n[[pattern.track.event]]\npos=0\nvel=1.0\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		got, _ := LoadPatternsTOML(data)
		for _, p := range got {
			if err := ValidatePattern(p); err != nil {
				t.Fatalf("%s: loaded but invalid: %v", p.Name, err)
			}
		}
	})
}
