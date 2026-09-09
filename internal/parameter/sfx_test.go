package parameter

import (
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/lixenwraith/vi-fighter/pkg/audio"
)

func loadBuiltins(t *testing.T) []*audio.SoundDef {
	t.Helper()
	defs, err := BuiltinSounds()
	if err != nil {
		t.Fatalf("builtin sounds: %v", err)
	}
	if len(defs) == 0 {
		t.Fatal("shipped sound bank is empty")
	}
	return defs
}

// A SoundSet field with no soundTable row stays SoundNone forever, and
// AudioEngine.Play discards it without touching played or dropped. That is the
// exact regression this test exists to catch: silent at runtime, invisible in
// telemetry, no compile error.
func TestSoundTableCoversSoundSet(t *testing.T) {
	bound := make(map[uintptr]string, len(soundTable))
	for i := range soundTable {
		bound[reflect.ValueOf(soundTable[i].slot).Pointer()] = soundTable[i].name
	}

	v := reflect.ValueOf(&Sfx).Elem()
	tp := v.Type()
	idType := reflect.TypeOf(audio.SoundID(0))

	for i := range tp.NumField() {
		f := tp.Field(i)
		if f.Type != idType {
			t.Errorf("SoundSet.%s: type %s, want audio.SoundID", f.Name, f.Type)
			continue
		}
		if _, ok := bound[v.Field(i).Addr().Pointer()]; !ok {
			t.Errorf("SoundSet.%s has no soundTable row; it will never resolve", f.Name)
		}
	}
}

// The game's names must exist in the shipped bank. A user sounds.toml
// overrides existing names against a frozen registry; it cannot introduce one,
// so a name absent here fails ResolveSounds and aborts startup.
func TestSoundTableNamesAreBuiltin(t *testing.T) {
	defs := loadBuiltins(t)
	known := make(map[string]bool, len(defs))
	for _, d := range defs {
		known[d.Name] = true
	}
	for i := range soundTable {
		if !known[soundTable[i].name] {
			t.Errorf("sound %q has no built-in spec", soundTable[i].name)
		}
	}
}

// Two rows for one name silently collapse the volume and shape maps to the
// last writer. Aliasing is expressed by pointing two rows at one slot, never
// by repeating a name.
func TestSoundTableNamesUnique(t *testing.T) {
	seen := make(map[string]int, len(soundTable))
	for i := range soundTable {
		if j, dup := seen[soundTable[i].name]; dup {
			t.Errorf("sound %q duplicated at rows %d and %d", soundTable[i].name, j, i)
			continue
		}
		seen[soundTable[i].name] = i
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

func TestBuiltinSoundsRender(t *testing.T) {
	for _, d := range loadBuiltins(t) {
		buf, err := audio.RenderPreview(d, audio.SFXParams{})
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
func TestBuiltinSoundsRoundTrip(t *testing.T) {
	defs := loadBuiltins(t)
	data, err := audio.MarshalSounds(defs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	back, err := audio.LoadSoundsTOML(data)
	if err != nil {
		t.Fatalf("reload: %v\n%s", err, data)
	}
	if len(back) != len(defs) {
		t.Fatalf("round trip produced %d sounds, want %d", len(back), len(defs))
	}
	for i := range defs {
		a, err := audio.RenderPreview(defs[i], audio.SFXParams{})
		if err != nil {
			t.Fatal(err)
		}
		b, err := audio.RenderPreview(back[i], audio.SFXParams{})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(a, b) {
			t.Errorf("%s: audio differs after round trip", defs[i].Name)
		}
	}
}
