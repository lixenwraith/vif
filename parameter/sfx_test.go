package parameter

import (
	"reflect"
	"testing"

	"github.com/lixenwraith/vi-fighter/audio"
)

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

// The game's names must exist in the shipped built-ins. A user sounds.toml
// overrides existing names against a frozen registry; it cannot introduce one,
// so a name absent here fails ResolveSounds and aborts startup.
func TestSoundTableNamesAreBuiltin(t *testing.T) {
	defs, err := audio.BuiltinSounds()
	if err != nil {
		t.Fatalf("builtin sounds: %v", err)
	}
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
