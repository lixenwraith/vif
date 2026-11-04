package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"math/cmplx"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

// The sound and pattern registries are package globals, so these tests are
// order-dependent and must not run in parallel. Each resets what it touches.

// testBank is the package's own sound set: the shipped bank is embedder data,
// so these tests own specs exercising what they assert on — a drum name, a
// master chain, a per-layer chain, vibrato.
func testBank() []*SoundDef {
	return []*SoundDef{
		{
			Name: "kick", Duration: 0.15, Raw: true, Variants: 6,
			Layer: []Layer{{
				Source: Source{Kind: "sweep", Wave: "sine", Freq: 150, FreqEnd: 40, Curve: "exp", CurveK: 8},
				Chain:  []Proc{{Kind: "decay", Tau: 0.03}, {Kind: "shape", Drive: 2.0}},
			}},
		},
		{
			Name: "bell", Duration: 0.35, Norm: 0.9,
			Layer: []Layer{{
				Source: Source{Kind: "osc", Wave: "sine", Freq: 880},
				Chain:  []Proc{{Kind: "ar", Attack: 0.004, Release: 0.32}},
			}},
		},
		{
			Name: "coin", Duration: 0.24, Norm: 0.85,
			Chain: []Proc{{Kind: "lp", Freq: 6000, Q: 0.707}},
			Layer: []Layer{{
				Length: 0.08,
				Source: Source{Kind: "osc", Wave: "square", Freq: 987.77},
				Chain:  []Proc{{Kind: "ar", Attack: 0.005, Release: 0.04}},
			}},
		},
		{
			Name: "ring", Duration: 0.7, Norm: 0.85,
			Chain: []Proc{{Kind: "bp", Freq: 1600, Q: 0.8, TrackPitch: true}},
			Layer: []Layer{{
				Source: Source{Kind: "osc", Wave: "sine", Freq: 660,
					Vibrato: &LFO{Rate: 2.5, Depth: 0.06, Phase: 0.25}},
				Chain: []Proc{{Kind: "am", Rate: 2.5, Depth: 0.55, Phase: 0.5}},
			}},
		},
	}
}

// testPatterns is the package's own pattern set, for the same reason: a beat, a
// chord-following melody and a fill, each owning every field a round trip carries.
func testPatterns() []*Pattern {
	return []*Pattern{
		{Name: "beat", Desc: "four on the floor", Steps: 16, Role: RoleRhythm, Groups: []string{"g"}, Tiers: 0b11, Tracks: []Track{
			{Instr: InstrKick, Events: []Step{{Pos: 0, Vel: 1}, {Pos: 4, Vel: 1}, {Pos: 8, Vel: 1}, {Pos: 12, Vel: 1}}},
			{Instr: InstrHihat, Humanize: 0.5, Events: []Step{{Pos: 2, Vel: 0.4, Prob: 0.8}}},
		}},
		{Name: "tune", Steps: 32, Role: RoleMelody, Groups: []string{"g", "h"}, Tiers: 0b1, Motif: "tune", Tracks: []Track{
			{Instr: InstrBass, FollowChord: true, Events: []Step{{Pos: 1, Vel: 0.8, Dur: 1}}},
			{Instr: InstrPiano, FollowChord: true, Events: []Step{{Pos: 16, Vel: 0.5, Deg: 4, Oct: 2, Dur: 2}}},
		}},
		{Name: "roll", Steps: 16, Role: RoleFill, Tiers: 0b10000, Tracks: []Track{
			{Instr: InstrSnare, Events: []Step{{Pos: 15, Vel: 0.9}}},
		}},
	}
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

// A SoundShape arrives from the embedder after ValidateSound has run, so the
// composed scale is what has to be clamped.
func TestShapingClamp(t *testing.T) {
	maxSamples := int(MaxSoundDuration*maxLengthScale*AudioSampleRate) + 1
	for _, d := range testBank() {
		for _, b := range RenderVariants(d, SoundShape{Pitch: 64, Length: 1e6}) {
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
	for _, d := range testBank() {
		if _, err := RegisterSound(d); err != nil {
			t.Fatal(err)
		}
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
	pats := testPatterns()
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
		if q.Steps != p.Steps || q.Desc != p.Desc || len(q.Tracks) != len(p.Tracks) ||
			q.Role != p.Role || q.Tiers != p.Tiers || !slices.Equal(q.Groups, p.Groups) {
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
		{"accidental out of range", func(p *Pattern) { p.Tracks[0].Events[0].Acc = 3 }},
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

// TestEngineResolvesSoundIncludes: the sounds document an embedder names loads
// with its includes, as the editor that wrote it read them.
func TestEngineResolvesSoundIncludes(t *testing.T) {
	t.Cleanup(ResetRegistries)
	ResetRegistries()

	cfg := DefaultAudioConfig()
	cfg.Enabled = true
	cfg.BaseSounds = testBank()
	cfg.BasePatterns = testPatterns()
	cfg.ForceBackend = BackendNameNull
	cfg.SoundFS = fstest.MapFS{
		"sounds.toml":    {Data: []byte("include = [\"extra/inc.toml\"]\n")},
		"extra/inc.toml": {Data: []byte("[[sound]]\nname = \"included\"\nduration = 0.05\n[[sound.layer]]\nsource = { kind = \"osc\", wave = \"sine\", freq = 440 }\n")},
	}
	cfg.SoundFile = "sounds.toml"
	ae, err := NewAudioEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := ae.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer ae.Stop()
	if err := ae.SpecError(); err != nil {
		t.Fatalf("spec error: %v", err)
	}
	if ae.SoundID("included") == SoundNone {
		t.Fatal("the included sound did not register")
	}
}

func TestNullBackendLifecycle(t *testing.T) {
	t.Cleanup(ResetRegistries)
	ResetRegistries()

	cfg := DefaultAudioConfig()
	cfg.Enabled = true
	cfg.BaseSounds = testBank()
	cfg.BasePatterns = testPatterns()
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
	cfg.BaseSounds = testBank()
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

// TestMusicRecordingIsTheMusicAlone: sfx never reach the recording, and it
// grows only while the sequencer plays.
func TestMusicRecordingIsTheMusicAlone(t *testing.T) {
	t.Cleanup(ResetRegistries)
	ResetRegistries()

	cfg := DefaultAudioConfig()
	cfg.Enabled = true
	cfg.BaseSounds = testBank()
	cfg.BasePatterns = testPatterns()
	cfg.ForceBackend = BackendNameNull
	ae, err := NewAudioEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := ae.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	dir := t.TempDir()
	silent, played := filepath.Join(dir, "silent.wav"), filepath.Join(dir, "played.wav")
	pcm := func(path string) int {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return len(data) - wavHeaderSize
	}

	if err := ae.RecordMusic(silent); err != nil {
		t.Fatal(err)
	}
	ae.Play(ae.SoundID("coin"))
	time.Sleep(3 * AudioBufferDuration)
	if err := ae.RecordMusic(played); err != nil { // closes silent.wav
		t.Fatal(err)
	}
	if n := pcm(silent); n != 0 {
		t.Fatalf("an sfx with no music playing recorded %d bytes", n)
	}
	ae.StartMusic()
	time.Sleep(3 * AudioBufferDuration)
	ae.Stop()
	if pcm(played) == 0 {
		t.Fatal("playing music recorded nothing")
	}
}

func TestWriteWAVFraming(t *testing.T) {
	buf, err := RenderPreview(&SoundDef{
		Name:     "test_tone",
		Duration: 0.1,
		Layer:    []Layer{{Source: Source{Kind: "osc", Wave: "sine", Freq: 440}}},
	}, SoundShape{})
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
	if data, err := MarshalSounds(testBank()); err == nil {
		f.Add(data)
	}
	f.Add([]byte("[[sound]]\nname=\"a\"\nduration=0.1\n[[sound.layer]]\nsource={kind=\"noise\"}\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		got, _ := LoadSoundsTOML(data)
		if got == nil {
			return
		}
		for _, d := range got.Sound {
			buf, err := RenderPreview(d, SoundShape{})
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

// testStopWait outlasts a loaded tick, which Stop's own bound may not
const testStopWait = 10 * time.Second

// startNullEngine brings up an engine on the discard backend with testBank
// registered.
func startNullEngine(t *testing.T) *AudioEngine {
	t.Helper()
	t.Cleanup(ResetRegistries)
	ResetRegistries()
	cfg := DefaultAudioConfig()
	cfg.Enabled = true
	cfg.BaseSounds = testBank()
	cfg.BasePatterns = testPatterns()
	cfg.ForceBackend = BackendNameNull
	ae, err := NewAudioEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := ae.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(ae.Stop)
	return ae
}

func TestDefineSoundReplacesInPlace(t *testing.T) {
	ae := startNullEngine(t)
	id := ae.SoundID("bell")
	if id == SoundNone {
		t.Fatal("bell unresolved")
	}
	d := SoundDefByName("bell").Clone()
	d.Layer[0].Source.Freq = 220
	got, err := ae.DefineSound(d)
	if err != nil {
		t.Fatalf("define: %v", err)
	}
	if got != id {
		t.Errorf("id changed on replace: %d -> %d", id, got)
	}
	if SoundDefByName("bell").Layer[0].Source.Freq != 220 {
		t.Error("registry did not take the replacement")
	}
}

func TestDefineSoundAppendsAfterFreeze(t *testing.T) {
	ae := startNullEngine(t)
	d := &SoundDef{
		Name: "editor_scratch", Duration: 0.05,
		Layer: []Layer{{Source: Source{Kind: "osc", Wave: "sine", Freq: 440}}},
	}
	// RegisterSound must still refuse a new name after Start; DefineSound is
	// the door that also propagates the ID.
	if _, err := RegisterSound(d); err == nil {
		t.Error("frozen registry accepted a new name via RegisterSound")
	}
	id, err := ae.DefineSound(d)
	if err != nil {
		t.Fatalf("define: %v", err)
	}
	time.Sleep(3 * AudioBufferDuration)
	if !ae.Play(id) {
		t.Error("Play rejected a sound defined after Start")
	}
	time.Sleep(3 * AudioBufferDuration)
	if played, _ := ae.Stats(); played == 0 {
		t.Error("mixer never admitted the new sound")
	}
}

// Drum reload must reach every DrumVoice in every PatternPlayer, which is why
// the voice reads through the kit instead of caching the slice.
func TestDefineSoundRebindsDrumKit(t *testing.T) {
	ae := startNullEngine(t)
	d := SoundDefByName("kick").Clone()
	d.Duration = 0.25
	if _, err := ae.DefineSound(d); err != nil {
		t.Fatalf("define: %v", err)
	}
	time.Sleep(3 * AudioBufferDuration)

	// Stop before inspecting: Wait establishes happens-before against the mix
	// goroutine, the only writer of kit and cache.
	ae.Stop()
	if !ae.mixer.Wait(testStopWait) {
		t.Fatal("mix goroutine still running; kit inspection would race")
	}

	vars := ae.mixer.kit.takes[InstrKick].layers[0]
	if len(vars) == 0 {
		t.Fatal("kit lost its kick variants")
	}
	// Every variant of the new 0.25s take is longer than any variant of the
	// old 0.15s one, whatever the length walk does.
	if len(vars[0]) <= samplesOf(0.15) {
		t.Errorf("kit still holds the old take: %d samples", len(vars[0]))
	}
}

// A harder hit plays a brighter take: the kit picks the velocity layer nearest
// the hit, whose low-pass opens with the velocity.
func TestDrumHitPlaysTheLayerNearestItsVelocity(t *testing.T) {
	d := &SoundDef{Name: "kick", Duration: 0.05, Variants: 2, Velocities: []float64{0.3, 1},
		Layer: []Layer{{Source: Source{Kind: "noise"}, Chain: []Proc{{Kind: "lp", Freq: 8000, VelTrack: 1}}}}}
	if err := ValidateSound(d); err != nil {
		t.Fatal(err)
	}
	k := &kit{}
	k.takes[InstrKick] = renderTakes(d, SoundShape{}, nil)
	layers := k.takes[InstrKick].layers
	soft, hard := k.take(InstrKick, 0.6, 1), k.take(InstrKick, 0.7, 1)
	if &soft[0] != &layers[0][1][0] || &hard[0] != &layers[1][1][0] {
		t.Fatal("a hit did not play the layer nearest its velocity")
	}
	// the mean step between samples against the mean level: a darker take moves less
	bright := func(x floatBuffer) float64 {
		var dx, ax float64
		for i := 1; i < len(x); i++ {
			dx += math.Abs(x[i] - x[i-1])
			ax += math.Abs(x[i])
		}
		return dx / ax
	}
	if bright(soft) >= bright(hard) {
		t.Errorf("soft take %.3f as bright as the hard one %.3f", bright(soft), bright(hard))
	}
}

func TestDefinePatternSwapsUnderPlayback(t *testing.T) {
	ae := startNullEngine(t)
	ae.StartMusic()
	beat := PatternIDByName("beat")
	ae.SetPattern(0, beat, MinCrossfadeSamples, false)
	time.Sleep(3 * AudioBufferDuration)

	p := GetPattern(beat).Clone()
	p.Tracks[0].Events = []Step{{Pos: 0, Vel: 0.5}, {Pos: 8, Vel: 0.5}}
	id, err := ae.DefinePattern(p)
	if err != nil {
		t.Fatalf("define: %v", err)
	}
	if id != beat {
		t.Errorf("name override changed id: %d", id)
	}
	time.Sleep(3 * AudioBufferDuration)
	ae.Stop()
	if !ae.mixer.Wait(testStopWait) {
		t.Fatal("mix goroutine still running; slot inspection would race")
	}

	if got := len(ae.mixer.sequencer.slots[0].player().patternData.Tracks[0].Events); got != 2 {
		t.Errorf("slot still on the old pattern: %d kick events", got)
	}
}

func TestPatternCloneIsDeep(t *testing.T) {
	src := testPatterns()[0]
	c := src.Clone()
	c.Tracks[0].Events[0].Vel = 0.01
	c.Tracks[0].Humanize = 0.99
	if src.Tracks[0].Events[0].Vel == 0.01 || src.Tracks[0].Humanize == 0.99 {
		t.Error("Clone aliases the original")
	}
}

func TestSoundDefCloneIsDeep(t *testing.T) {
	ring := testBank()[3]
	c := ring.Clone()
	c.Layer[0].Chain[0].Depth = 0.01
	c.Layer[0].Source.Vibrato.Rate = 99
	if ring.Layer[0].Chain[0].Depth == 0.01 || ring.Layer[0].Source.Vibrato.Rate == 99 {
		t.Error("Clone aliases the original")
	}
}

func TestPlayBufferAuditions(t *testing.T) {
	ae := startNullEngine(t)
	buf, err := RenderPreview(&SoundDef{
		Name: "unsaved", Duration: 0.1,
		Layer: []Layer{{Source: Source{Kind: "osc", Wave: "sine", Freq: 440}}},
	}, SoundShape{})
	if err != nil {
		t.Fatal(err)
	}
	if SoundIDByName("unsaved") != SoundNone {
		t.Fatal("preview leaked into the registry")
	}
	if !ae.PlayBuffer(buf, 1.0) {
		t.Fatal("PlayBuffer rejected")
	}
	time.Sleep(3 * AudioBufferDuration)
	if played, _ := ae.Stats(); played == 0 {
		t.Error("mixer never admitted the preview")
	}
}

// TestHeldTierVariesDrawnSlots is the rule that a tier held for a phrase moves on to
// another member of a pool, while a slot placed explicitly stays where it was put.
func TestHeldTierVariesDrawnSlots(t *testing.T) {
	t.Cleanup(ResetRegistries)
	ResetRegistries()
	var ids [4]PatternID
	for i := range ids {
		ids[i] = registerDrawn(string(rune('a'+i)), Role(i/2+1), "g")
	}
	s := NewSequencer(MaxBPM, nil)
	s.arr = buildArrangement()
	s.SetIntensity(IntensityCalm, 0, false, false)
	s.SetPattern(1, ids[2], 0, false)
	rhythm := s.slots[0].activeID()
	s.Start()

	// Phrase downbeats take melody (bar 8), rhythm (bar 16) and bass (bar 24) in turn
	s.Generate(make([]float64, 3*PhraseBars*SamplesPerBar(MaxBPM)+1))
	if got := s.slots[0].activeID(); got == rhythm {
		t.Errorf("drawn rhythm still on %d after three phrases", got)
	}
	if got := s.slots[1].activeID(); got != ids[2] {
		t.Errorf("explicit melody moved to %d", got)
	}
}

// registerDrawn registers a one-note calm pattern in one group for a role
func registerDrawn(name string, r Role, group string) PatternID {
	return RegisterPattern(&Pattern{Name: name, Steps: 16, Role: r, Groups: []string{group}, Tiers: 1 << IntensityCalm,
		Tracks: []Track{{Instr: InstrBass, Events: []Step{{Pos: 0, Vel: 0.5, Dur: 1}}}}})
}

// TestDrawsHoldOneGroup is the cohesion rule: both drawn slots always come from one
// group, the melody resting only through the bar before a move, and after
// GroupPhrases they move to another group together.
func TestDrawsHoldOneGroup(t *testing.T) {
	t.Cleanup(ResetRegistries)
	ResetRegistries()
	groupOf := map[PatternID]string{}
	for _, g := range []string{"x", "y"} {
		for i, r := range []Role{RoleRhythm, RoleRhythm, RoleMelody, RoleMelody} {
			groupOf[registerDrawn(fmt.Sprint(g, i), r, g)] = g
		}
	}
	s := NewSequencer(MaxBPM, nil)
	s.arr = buildArrangement()
	s.SetIntensity(IntensityCalm, 0, false, false)
	first := groupOf[s.slots[0].activeID()]
	s.Start()

	bar := make([]float64, SamplesPerBar(MaxBPM))
	for b := range GroupPhrases * PhraseBars {
		s.Generate(bar)
		if r, m := groupOf[s.slots[0].activeID()], groupOf[s.slots[1].activeID()]; r != m && s.slots[1].activeID() != PatternSilence {
			t.Fatalf("bar %d plays rhythm from %q under melody from %q", b, r, m)
		}
	}
	s.Generate(bar[:1]) // the last downbeat falls on the next sample
	if got := groupOf[s.slots[0].activeID()]; got == first || got != groupOf[s.slots[1].activeID()] {
		t.Errorf("still in group %q after %d phrases", got, GroupPhrases)
	}
}

// TestMutedSlotPlaysOnInTime is the rule of a slot's mute: the slot sounds nothing
// while its pattern plays on, so once the mute has faded back out it sounds as if
// it had never been muted.
func TestMutedSlotPlaysOnInTime(t *testing.T) {
	t.Cleanup(ResetRegistries)
	ResetRegistries()
	var evs []Step
	for pos := 0; pos < 16; pos += 4 {
		evs = append(evs, Step{Pos: pos, Vel: 0.8, Dur: 2})
	}
	id := RegisterPattern(&Pattern{Name: "pulse", Steps: 16, Tracks: []Track{{Instr: InstrBass, Events: evs}}})
	// run plays four bars, the second and third muted when mute is set, and
	// returns the third and fourth
	run := func(mute bool) (muted, back []float64) {
		s := NewSequencer(MaxBPM, nil)
		s.SetPattern(3, id, 0, false)
		s.Start()
		bar := SamplesPerBar(MaxBPM)
		s.Generate(make([]float64, bar))
		s.SetSlotMuted(3, mute)
		s.Generate(make([]float64, bar))
		muted, back = make([]float64, bar), make([]float64, bar)
		s.Generate(muted)
		s.SetSlotMuted(3, false)
		s.Generate(back)
		return muted, back
	}
	muted, back := run(true)
	sounding, ref := run(false)
	var loud, silent float64
	for i := range muted {
		silent, loud = max(silent, math.Abs(muted[i])), max(loud, math.Abs(sounding[i]))
	}
	if silent != 0 || loud == 0 {
		t.Fatalf("a muted bar peaked at %g, the same bar unmuted at %g", silent, loud)
	}
	for i := 2 * MinCrossfadeSamples; i < len(back); i++ {
		if back[i] != ref[i] {
			t.Fatalf("sample %d after the unmute is %g, %g never muted", i, back[i], ref[i])
		}
	}
}

// TestTrackMaskHoldsForItsPattern is the rule of a slot's track mask: it masks the
// pattern it names whenever that begins in the slot, on the bar line after it was
// set too, and never the pattern playing until then or another after.
func TestTrackMaskHoldsForItsPattern(t *testing.T) {
	t.Cleanup(ResetRegistries)
	ResetRegistries()
	two := []Track{{Instr: InstrBass}, {Instr: InstrBass}}
	a := RegisterPattern(&Pattern{Name: "a", Steps: 16, Tracks: two})
	b := RegisterPattern(&Pattern{Name: "b", Steps: 16, Tracks: two})
	s := NewSequencer(MaxBPM, nil)
	mask := func() uint32 { return s.slots[3].player().mask }
	s.SetPattern(3, a, 0, false)
	s.Start()
	s.SetPattern(3, b, 0, true)
	s.SetMask(3, b, 1<<1)
	if mask() != ^uint32(0) {
		t.Fatalf("a, playing to the bar line, took b's mask: %b", mask())
	}
	s.Generate(make([]float64, SamplesPerBar(MaxBPM)+1))
	if s.slots[3].activeID() != b || mask() != 1<<1 {
		t.Fatalf("b began on the bar line under mask %b", mask())
	}
	s.SetPattern(3, a, 0, false)
	if mask() != ^uint32(0) {
		t.Fatalf("a began under b's mask: %b", mask())
	}
	s.SetPattern(3, b, 0, false)
	if mask() != 1<<1 {
		t.Fatalf("b began again under mask %b", mask())
	}
}

// TestTransitionKeepsIncomingWhole is the rule a tier change broke: the incoming
// pattern's downbeat sounds at full level whatever the fade, and a build-up starts
// from as many tracks as the outgoing pattern sounded.
func TestTransitionKeepsIncomingWhole(t *testing.T) {
	t.Cleanup(ResetRegistries)
	ResetRegistries()
	note := []Step{{Pos: 0, Vel: 0.8, Dur: 4}}
	quiet := RegisterPattern(&Pattern{Name: "quiet", Steps: 16, Tracks: []Track{
		{Instr: InstrBass}, {Instr: InstrBass},
	}})
	full := RegisterPattern(&Pattern{Name: "full", Steps: 16, Tracks: []Track{
		{Instr: InstrBass, Events: note}, {Instr: InstrPad}, {Instr: InstrPad},
	}})
	peak := func(fade int) (float64, *Sequencer) {
		s := NewSequencer(MaxBPM, nil)
		s.SetPattern(1, quiet, 0, false)
		s.Start()
		s.setPattern(1, full, fade, true, true) // lands on the next downbeat, as a tier does
		s.Generate(make([]float64, SamplesPerBar(MaxBPM)))
		buf := make([]float64, SamplesPerStep(MaxBPM))
		s.Generate(buf)
		var p float64
		for _, v := range buf {
			p = max(p, math.Abs(v))
		}
		return p, s
	}
	snap, _ := peak(0)
	faded, s := peak(AudioSampleRate)
	if faded < 0.99*snap {
		t.Errorf("a one-second fade plays the incoming downbeat at %.3f, a snap at %.3f", faded, snap)
	}
	if got := s.slots[1].revealN; got != 2 {
		t.Errorf("build-up starts from %d tracks, want the 2 the outgoing pattern sounded", got)
	}
}

// closeBuffer is a host's speakers in a test: it keeps what the mixer wrote and
// whether the engine closed it.
type closeBuffer struct {
	mu     sync.Mutex
	n      int
	closed bool
}

func (b *closeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.n += len(p)
	return len(p), nil
}

func (b *closeBuffer) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return nil
}

// TestAHostSinkReplacesBackendDetection: a host that supplies its speakers gets
// the stream whatever backend name is set, and keeps them when the engine stops:
// the next run, after a scenario change, streams to the same speakers.
func TestAHostSinkReplacesBackendDetection(t *testing.T) {
	t.Cleanup(ResetRegistries)
	ResetRegistries()

	sink := &closeBuffer{}
	cfg := DefaultAudioConfig()
	cfg.Enabled = true
	cfg.BaseSounds = testBank()
	cfg.ForceBackend = BackendNameNull
	cfg.Sink = sink
	ae, err := NewAudioEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := ae.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := ae.BackendName(); got != BackendNameHost {
		t.Fatalf("backend %q, want %q", got, BackendNameHost)
	}
	ae.Play(ae.SoundID("coin"))
	time.Sleep(3 * AudioBufferDuration)
	ae.Stop()

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.n == 0 || sink.closed {
		t.Fatalf("sink took %d bytes, closed %v; want written and left open", sink.n, sink.closed)
	}
}

// oneNote registers a one-note pattern for a role at the given tiers
func oneNote(name string, r Role, tiers uint8) PatternID {
	return RegisterPattern(&Pattern{Name: name, Steps: 16, Role: r, Groups: []string{"g"}, Tiers: tiers,
		Tracks: []Track{{Instr: InstrBass, Events: []Step{{Pos: 0, Vel: 0.6, Dur: 8}}}}})
}

// TestTransitionMidFadeKeepsTails is the rule a quick second transition broke: a
// slot replaced again while its last pattern still fades keeps both tails ringing.
func TestTransitionMidFadeKeepsTails(t *testing.T) {
	t.Cleanup(ResetRegistries)
	ResetRegistries()
	a, b, c := oneNote("a", RoleNone, 0), oneNote("b", RoleNone, 0), oneNote("c", RoleNone, 0)
	s := NewSequencer(MaxBPM, nil)
	s.SetPattern(1, a, 0, false)
	s.Start()
	step := make([]float64, SamplesPerStep(MaxBPM))
	s.Generate(step)
	s.SetPattern(1, b, AudioSampleRate, false)
	s.Generate(step)
	s.SetPattern(1, c, AudioSampleRate, false)
	ss := &s.slots[1]
	ringing := 0
	for i := range ss.players {
		if i != ss.live && ss.gain[i] > 0 {
			ringing++
		}
	}
	if ss.activeID() != c || ringing != 2 {
		t.Errorf("live %d with %d tails ringing, want %d with 2", ss.activeID(), ringing, c)
	}
}

// TestBassHoldsThroughTiers is the anchor rule: a tier change keeps the bass whose
// pattern still covers the tier while the other drawn slots redraw.
func TestBassHoldsThroughTiers(t *testing.T) {
	t.Cleanup(ResetRegistries)
	ResetRegistries()
	calm, normal := uint8(1<<IntensityCalm), uint8(1<<IntensityNormal)
	oneNote("r1", RoleRhythm, calm)
	r2 := oneNote("r2", RoleRhythm, normal)
	oneNote("m", RoleMelody, calm|normal)
	b1 := oneNote("b1", RoleBass, calm|normal)
	oneNote("b2", RoleBass, normal)
	s := NewSequencer(MaxBPM, nil)
	s.arr = buildArrangement()
	s.SetIntensity(IntensityCalm, 0, false, false)
	s.Start()
	s.Generate(make([]float64, SamplesPerBar(MaxBPM)))
	s.SetIntensity(IntensityNormal, 0, false, false)
	if got := s.slots[slotBass].activeID(); got != b1 {
		t.Errorf("bass moved to %d, want %d held", got, b1)
	}
	if got := s.slots[0].activeID(); got != r2 {
		t.Errorf("rhythm on %d, want the tier's %d", got, r2)
	}
}

// TestDrawsWorkThroughThePool is the rule that a pool's members are each drawn once
// before any repeats, so a held tier does not replay one pattern.
func TestDrawsWorkThroughThePool(t *testing.T) {
	s := NewSequencer(MaxBPM, nil)
	pool := []PatternID{101, 102, 103}
	seen := map[PatternID]bool{}
	for range pool {
		seen[s.draw(pool, PatternSilence, true)] = true
	}
	if len(seen) != len(pool) {
		t.Errorf("three draws from three members hit %d of them", len(seen))
	}
}

// TestDrawsFollowTheMotif is the continuity rule: a tier change draws the playing
// motif's member of the new pool, and a phrase variation draws another motif's.
func TestDrawsFollowTheMotif(t *testing.T) {
	t.Cleanup(ResetRegistries)
	ResetRegistries()
	motif := func(name, m string) PatternID {
		return RegisterPattern(&Pattern{Name: name, Steps: 16, Motif: m,
			Tracks: []Track{{Instr: InstrBass, Events: []Step{{Pos: 0, Vel: 0.5}}}}})
	}
	a1, a2, b1, b2 := motif("a1", "a"), motif("a2", "a"), motif("b1", "b"), motif("b2", "b")
	s := NewSequencer(MaxBPM, nil)
	pool := []PatternID{a2, b1, b2}
	for range 3 {
		if got := s.draw(pool, a1, true); got != a2 {
			t.Fatalf("a tier change on motif a drew %d, want %d", got, a2)
		}
		if got := s.draw(pool, a1, false); got == a2 {
			t.Fatal("a phrase variation on motif a drew motif a again")
		}
	}
}

// TestPatternMacrosExpand is the rule that a music document's macros expand before
// its keys are checked: a note line written once arrives as its notes.
func TestPatternMacrosExpand(t *testing.T) {
	doc := `
[macros.Note]
params = { pos = "int", deg = "int" }
body = { pos = "${pos}", deg = "${deg}", vel = 0.5 }
[[pattern]]
name = "m"
steps = 16
[[pattern.track]]
instr = "lead"
event = [{ macro = "Note", each = { pos = [0, 4, 8], deg = [0, 2, 4] } }]
`
	pats, err := LoadPatternsTOML([]byte(doc))
	if err != nil || len(pats) != 1 {
		t.Fatalf("load: %v, %d patterns", err, len(pats))
	}
	ev := pats[0].Tracks[0].Events
	if len(ev) != 3 || ev[2].Pos != 8 || ev[2].Deg != 4 {
		t.Errorf("events %v, want three notes ending on pos 8 deg 4", ev)
	}
}

// TestVoiceDocumentAddsAnInstrument is the rule that a [[voice]] in the sound
// document is an instrument patterns name: a model name retunes that instrument in
// place, a new name adds one, and a pattern's def carries the name both ways.
func TestVoiceDocumentAddsAnInstrument(t *testing.T) {
	t.Cleanup(ResetRegistries)
	ResetRegistries()
	f, err := LoadSoundsTOML([]byte(`
[[voice]]
name = "lead"
wave = "square"
attack = 0.01
sustain = 0.5
[[voice]]
name = "rhodes"
wave = "modal"
partials = 20
decay_time = 1.2
level = 0.9
`))
	if err != nil || len(f.Voice) != 2 {
		t.Fatalf("load: %v, %d voices", err, len(f.Voice))
	}
	for _, v := range f.Voice {
		if _, err := RegisterVoice(v); err != nil {
			t.Fatal(err)
		}
	}
	if id, _ := InstrumentByName("lead"); id != InstrLead {
		t.Errorf("lead resolved to %d, want its model number %d", id, InstrLead)
	}
	id, ok := InstrumentByName("rhodes")
	if !ok || id < InstrumentCount || InstrumentName(id) != "rhodes" {
		t.Fatalf("rhodes resolved to %d (%v), named %q", id, ok, InstrumentName(id))
	}
	def := &PatternDef{Name: "p", Steps: 16, Track: []TrackDef{{Instr: "rhodes", Event: []StepDef{{Pos: 0, Vel: 0.5}}}}}
	p, err := def.Pattern()
	if err != nil {
		t.Fatalf("a pattern naming an added voice: %v", err)
	}
	if got := p.Def().Track[0].Instr; got != "rhodes" {
		t.Errorf("def names the track %q, want rhodes", got)
	}
	out, err := MarshalSoundsFile(f)
	if err != nil {
		t.Fatal(err)
	}
	again, err := LoadSoundsTOML(out)
	if err != nil {
		t.Fatal(err)
	}
	if back, _ := MarshalSoundsFile(again); !bytes.Equal(out, back) {
		t.Errorf("voices do not round-trip:\n%s\n%s", out, back)
	}
}

// TestDefineVoiceReachesTheNextNote is the hot swap: a voice defined after Start
// is what the next trigger of its instrument plays, a redefined one and an added
// one alike, while a note already sounding keeps the voice it began on.
func TestDefineVoiceReachesTheNextNote(t *testing.T) {
	ae := startNullEngine(t)
	lead := &VoiceDef{Name: "lead", Wave: "square", Attack: 0.01, Sustain: 1}
	glass := &VoiceDef{Name: "glass", Wave: "modal", Partials: 12, DecayTime: 2}
	if _, err := ae.DefineVoice(lead); err != nil {
		t.Fatal(err)
	}
	id, err := ae.DefineVoice(glass)
	if err != nil || id < InstrumentCount {
		t.Fatalf("define glass: %v, id %d", err, id)
	}
	time.Sleep(3 * AudioBufferDuration)
	ae.Stop()
	if !ae.mixer.Wait(testStopWait) {
		t.Fatal("mix goroutine still running; kit inspection would race")
	}
	k := ae.mixer.kit
	if k.voice(InstrLead) != lead || k.voice(id) != glass {
		t.Fatal("the kit does not hold the defined voices")
	}
	v := NewTonalVoice(k)
	v.Trigger(VoiceParams{Note: 60, Velocity: 1, Instrument: id})
	k.setVoice(id, &VoiceDef{Name: "glass", Wave: "sine"})
	if v.spec != glass {
		t.Error("a sounding note lost the voice it began on")
	}
	v.Trigger(VoiceParams{Note: 60, Velocity: 1, Instrument: id})
	if v.spec == glass {
		t.Error("the next trigger kept the superseded voice")
	}
}

// TestVoiceValidationRejects holds ValidateVoice to the mixer's bounds.
func TestVoiceValidationRejects(t *testing.T) {
	for _, d := range []*VoiceDef{
		{Name: "kick"},
		{Name: "v", Wave: "triangle"},
		{Name: "v", Unison: 4},
		{Name: "v", Detune: 3},
		{Name: "v", Sustain: 1.5},
		{Name: "v", Cutoff: math.Inf(1)},
		{Name: "v", Partials: maxPartials + 1},
	} {
		if ValidateVoice(d) == nil {
			t.Errorf("%+v validated", d)
		}
	}
	if err := ValidateVoice(&VoiceDef{Name: "v", Wave: "saw", Unison: 2, Detune: 1.01, Attack: 0.01}); err != nil {
		t.Errorf("a plain voice failed: %v", err)
	}
}

// TestVoiceVibratoAndVowel: a vibrato moves the pitch of an oscillator voice,
// so its periods vary where a plain one's do not; a vowel's formant bank
// renders finite and changes the sound; a vowel the bank does not have and a
// vibrato past a semitone are refused.
func TestVoiceVibratoAndVowel(t *testing.T) {
	plain := &VoiceDef{Name: "t", Wave: "sine", Attack: 0.001, Decay: 0.01, Sustain: 1, Release: 0.01, Level: 1}
	wobble := *plain
	wobble.VibratoHz, wobble.VibratoCents = 6, 60
	periods := func(x []float64) (mean, spread float64) {
		var last int
		var ps []float64
		for i := 1; i < len(x); i++ {
			if x[i-1] < 0 && x[i] >= 0 {
				if last > 0 {
					ps = append(ps, float64(i-last))
				}
				last = i
			}
		}
		for _, p := range ps {
			mean += p
		}
		mean /= float64(max(len(ps), 1))
		for _, p := range ps {
			spread += (p - mean) * (p - mean)
		}
		return mean, math.Sqrt(spread / float64(max(len(ps), 1)))
	}
	_, flat := periods(RenderNote(plain, 60, 1, 0.5, 0))
	mean, moved := periods(RenderNote(&wobble, 60, 1, 0.5, 0))
	if flat > 0.6 || moved < 0.02*mean {
		t.Errorf("period spread %.3f plain, %.3f with vibrato over a mean of %.1f samples", flat, moved, mean)
	}
	vowel := *plain
	vowel.Wave, vowel.Vowel = "saw", "a"
	a, b := RenderNote(plain, 60, 1, 0.3, 0), RenderNote(&vowel, 60, 1, 0.3, 0)
	var diff float64
	for i := range min(len(a), len(b)) {
		if math.IsNaN(b[i]) || math.IsInf(b[i], 0) {
			t.Fatalf("the vowel rendered %v at %d", b[i], i)
		}
		diff += math.Abs(a[i] - b[i])
	}
	if diff == 0 {
		t.Error("the vowel changed nothing")
	}
	bad := *plain
	bad.Vowel = "y"
	wide := *plain
	wide.VibratoCents = 200
	if ValidateVoice(&bad) == nil || ValidateVoice(&wide) == nil || ValidateVoice(&vowel) != nil || ValidateVoice(&wobble) != nil {
		t.Errorf("validation: vowel y %v, 200 cents %v, a vowel %v, a vibrato %v", ValidateVoice(&bad), ValidateVoice(&wide), ValidateVoice(&vowel), ValidateVoice(&wobble))
	}
}

// TestStringKeepsTuneAndRingAcrossTheKeyboard: the string wave's fundamental
// sits within a cent of equal temperament from C2 to C7 and falls 60 dB in the
// voice's ring at middle C, at every pitch with no decay_track and shorter up
// the keyboard by it, read from its phase and magnitude in frames 64 samples
// apart, then up to 0.4 s apart.
func TestStringKeepsTuneAndRingAcrossTheKeyboard(t *testing.T) {
	const win, near = 4096, 64
	for _, track := range []float64{0, 1.1} {
		v := &VoiceDef{Name: "s", Wave: "string", Ring: 1, Bright: 0.2, DecayTrack: track, Attack: 0.001, Release: 0.1}
		for _, m := range []int{36, 50, 60, 72, 84, 91, 96} {
			f := NoteFreq(m)
			want := v.Ring * math.Pow(keyRefHz/f, track)
			// read where the note has fallen 15 dB and over a further 20 at most
			t1 := min(AudioSampleRate/5, int(want*AudioSampleRate/4))
			far := min(AudioSampleRate*2/5, int(want*AudioSampleRate/3))
			x := RenderNote(v, m, 1, 1, 0)
			if len(x) < t1+far+win {
				t.Fatalf("track %.1f, midi %d: the note ended after %d samples", track, m, len(x))
			}
			frame := func(at int) complex128 {
				var s complex128
				for i := range win {
					w := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/win)
					s += complex(x[at+i]*w, 0) * cmplx.Rect(1, -2*math.Pi*f*float64(at+i)/AudioSampleRate)
				}
				return s
			}
			turn := func(a, b complex128) float64 { return math.Remainder(cmplx.Phase(b)-cmplx.Phase(a), 2*math.Pi) }
			a, b, c := frame(t1), frame(t1+near), frame(t1+far)
			guess := turn(a, b) * float64(far) / near
			hz := (guess + math.Remainder(turn(a, c)-guess, 2*math.Pi)) * AudioSampleRate / (2 * math.Pi * float64(far))
			cents := 1200 * math.Log2(1+hz/f)
			t60 := 60 * float64(far) / AudioSampleRate / (20 * math.Log10(cmplx.Abs(a)/cmplx.Abs(c)))
			if math.Abs(cents) > 1 || math.Abs(t60-want) > 0.05*want {
				t.Errorf("track %.1f, midi %d: %+.2f cents off, T60 %.3f s where %.3f is due", track, m, cents, t60, want)
			}
		}
	}
}

// TestPluckedAndStruckNotesFadeOnTheirOwn: a string's or a modal bank's note is
// never held at a sustain: an ADSR that would hold an oscillator at half level
// plays it as one with none, and a note held past its ring ends by itself.
func TestPluckedAndStruckNotesFadeOnTheirOwn(t *testing.T) {
	for _, wave := range []string{"string", "modal"} {
		free := &VoiceDef{Name: "v", Wave: wave, Ring: 0.3, DecayTime: 0.05, Attack: 0.001, Release: 0.05}
		held := *free
		held.Decay, held.Sustain = 0.01, 0.5
		a, b := RenderNote(free, 60, 1, 10, 0), RenderNote(&held, 60, 1, 10, 0)
		if !slices.Equal(a, b) {
			t.Errorf("%s: the ADSR's sustain shaped the note", wave)
		}
		if len(a) >= 10*AudioSampleRate {
			t.Errorf("%s: held 10 s, the note sounded all of it rather than ringing out", wave)
		}
	}
}

// TestOscillatorsRunFree: an oscillator voice's unison starts each note at
// phases drawn from the note and its trigger order, so a replay is the same
// note and the next trigger is not; a detuned stack wanders in pitch within
// its share of the detune, and an undetuned one holds it.
func TestOscillatorsRunFree(t *testing.T) {
	k := &kit{voices: make([]*VoiceDef, InstrLead+1), tuning: 1}
	tv := NewTonalVoice(k)
	play := func(v *VoiceDef, serial uint64) (out []float64, lo, hi float64) {
		k.voices[InstrLead] = v
		tv.serial = serial
		tv.Trigger(VoiceParams{Note: 60, Velocity: 1, Duration: 4 * AudioSampleRate, Instrument: InstrLead})
		lo, hi = math.Inf(1), math.Inf(-1)
		for range 4 * AudioSampleRate {
			out = append(out, tv.Sample())
			lo, hi = min(lo, tv.wander[0]/tv.ratio[0]), max(hi, tv.wander[0]/tv.ratio[0])
		}
		return out, lo, hi
	}
	saw := &VoiceDef{Name: "v", Wave: "saw", Unison: 3, Detune: 1.01, Attack: 0.01, Sustain: 1, Release: 0.1}
	a, lo, hi := play(saw, 1)
	b, _, _ := play(saw, 1)
	c, _, _ := play(saw, 2)
	if !slices.Equal(a, b) || slices.Equal(a, c) {
		t.Errorf("a replay differs (%v) or the next trigger repeats it (%v)", !slices.Equal(a, b), slices.Equal(a, c))
	}
	if d := driftShare * 0.01; hi-1 > d || 1-lo > d || hi-lo < d/4 {
		t.Errorf("the wander spans %.5f to %.5f of the pitch, want within ±%.4f and moving", lo, hi, d)
	}
	plain := *saw
	plain.Detune = 1
	if _, lo, hi := play(&plain, 1); lo != 1 || hi != 1 {
		t.Errorf("an undetuned stack wandered from %.5f to %.5f", lo, hi)
	}
}

// TestFilterEnvelopeFollowsVelocity: a softer note opens the filter less, so it
// is darker as well as quieter; a voice with no envelope sweep plays every
// velocity as one sound scaled.
func TestFilterEnvelopeFollowsVelocity(t *testing.T) {
	hiShare := func(x []float64) float64 {
		var all, hp, prev float64
		for _, s := range x {
			all += s * s
			hp += (s - prev) * (s - prev)
			prev = s
		}
		return hp / all
	}
	swept := &VoiceDef{Name: "v", Wave: "saw", Cutoff: 300, EnvAmount: 3000, EnvDecay: 0.2, Q: 0.7, Attack: 0.005, Sustain: 1, Release: 0.1}
	soft, loud := RenderNote(swept, 60, 0.3, 0.3, 0), RenderNote(swept, 60, 0.9, 0.3, 0)
	if hiShare(soft) > 0.7*hiShare(loud) {
		t.Errorf("the soft note's edge share %.4f is not under the loud one's %.4f", hiShare(soft), hiShare(loud))
	}
	still := *swept
	still.EnvAmount = 0
	soft, loud = RenderNote(&still, 60, 0.3, 0.3, 0), RenderNote(&still, 60, 0.9, 0.3, 0)
	for i := range soft {
		if math.Abs(soft[i]*3-loud[i]) > 1e-12 {
			t.Fatalf("with no sweep, velocity changed the sound at sample %d", i)
		}
	}
}

// TestBankBodyAndStrikeShapeItsPartials: a modal voice's body sounds the
// partials under bright·bodyHz weakly, as a piano's soundboard does its bass
// fundamentals, and a softer strike leaves the upper partials weaker.
func TestBankBodyAndStrikeShapeItsPartials(t *testing.T) {
	k := &kit{voices: make([]*VoiceDef, InstrLead+1), tuning: 1}
	tv := NewTonalVoice(k)
	partials := func(v *VoiceDef, vel float64) []float64 {
		k.voices[InstrLead] = v
		tv.Trigger(VoiceParams{Note: 36, Velocity: vel, Instrument: InstrLead})
		return slices.Clone(tv.mre)
	}
	db := func(a, b float64) float64 { return 20 * math.Log10(a/b) }
	bare := &VoiceDef{Name: "v", Wave: "modal", Partials: 24, HammerHz: 2000, DecayTime: 1, Release: 0.1}
	body := *bare
	body.Bright = 0.3
	p, q := partials(bare, 1), partials(&body, 1)
	if drop := db(p[0], p[5]) - db(q[0], q[5]); drop < 25 {
		t.Errorf("the body took %.1f dB off C2's fundamental against its sixth partial, want 25", drop)
	}
	soft, hard := partials(bare, 0.3), partials(bare, 0.9)
	if lift := db(hard[15], hard[0]) - db(soft[15], soft[0]); lift < 12 {
		t.Errorf("a hard strike lifts the sixteenth partial %.1f dB over a soft one, want 12", lift)
	}
}

// TestPairNeverCancelsAPartial: a modal bank's two strings start in phase and are
// a quarter cycle apart where the slower one overtakes the faster, across the
// keyboard, so no note loses a paired partial to the beat at the hand-over
func TestPairNeverCancelsAPartial(t *testing.T) {
	k := &kit{voices: make([]*VoiceDef, InstrLead+1), tuning: 1}
	tv := NewTonalVoice(k)
	k.voices[InstrLead] = &VoiceDef{Name: "v", Wave: "modal", Partials: 24, Bright: 0.19, HammerHz: 1670,
		DecayTime: 0.4, DecayTrack: 0.67, Pair: 3, PairLevel: 0.2, PairSlow: 0.17, Release: 0.1}
	var pairs int
	for note := 36; note <= 96; note++ {
		tv.Trigger(VoiceParams{Note: note, Velocity: 0.6, Instrument: InstrLead})
		for i := 0; i+1 < len(tv.mre); i++ {
			w1, w2 := math.Atan2(tv.mci[i], tv.mcr[i]), math.Atan2(tv.mci[i+1], tv.mcr[i+1])
			if math.Abs(w2-w1) > 0.01*w1 {
				continue
			}
			pairs++
			d1, d2 := math.Hypot(tv.mcr[i], tv.mci[i]), math.Hypot(tv.mcr[i+1], tv.mci[i+1])
			a1, a2 := tv.mre[i], tv.mre[i+1]
			if tv.mim[i] != 0 || tv.mim[i+1] != 0 || a2 >= a1 || d2 <= d1 {
				t.Fatalf("note %d: the strings start at %v and %v, decaying %v and %v", note, a1, a2, d1, d2)
			}
			n := math.Log(a1/a2) / math.Log(d2/d1) // samples to the hand-over
			x, y := a1*math.Pow(d1, n), a2*math.Pow(d2, n)
			sum := math.Hypot(x+y*math.Cos((w2-w1)*n), y*math.Sin((w2-w1)*n))
			if sum < 1.35*x {
				t.Errorf("note %d: partial at %.1f Hz falls to %.2f of one string at the hand-over, want a quarter cycle's 1.41",
					note, w1*AudioSampleRate/(2*math.Pi), sum/x)
			}
		}
	}
	if pairs < 61 {
		t.Errorf("found %d paired partials over 61 notes, want one a note at least", pairs)
	}
}
