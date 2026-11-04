//go:build !vif_headless && !vif_noaudio

package parameter

import (
	"reflect"
	"testing"

	"github.com/lixenwraith/audio"
	"github.com/lixenwraith/audio/bank"
)

// A SfxSet field with no sfxTable row stays SoundNone forever, and
// AudioEngine.Play discards it without touching played or dropped. That is the
// exact regression this test exists to catch: silent at runtime, invisible in
// telemetry, no compile error.
func TestSfxTableCoversSfxSet(t *testing.T) {
	bound := make(map[uintptr]string, len(sfxTable))
	for i := range sfxTable {
		bound[reflect.ValueOf(sfxTable[i].slot).Pointer()] = sfxTable[i].name
	}

	v := reflect.ValueOf(&Sfx).Elem()
	tp := v.Type()
	idType := reflect.TypeOf(audio.SoundID(0))

	for i := range tp.NumField() {
		f := tp.Field(i)
		if f.Type != idType {
			t.Errorf("SfxSet.%s: type %s, want audio.SoundID", f.Name, f.Type)
			continue
		}
		if _, ok := bound[v.Field(i).Addr().Pointer()]; !ok {
			t.Errorf("SfxSet.%s has no sfxTable row; it will never resolve", f.Name)
		}
	}
}

// The game's names must exist in the shipped bank. A user sounds.toml
// overrides existing names against a frozen registry; it cannot introduce one,
// so a name absent here fails ResolveSfx and aborts startup.
func TestSfxTableNamesAreBuiltin(t *testing.T) {
	shipped, err := bank.Sounds()
	if err != nil {
		t.Fatalf("bank sounds: %v", err)
	}
	known := make(map[string]bool, len(shipped.Sound))
	for _, d := range shipped.Sound {
		known[d.Name] = true
	}
	for i := range sfxTable {
		if !known[sfxTable[i].name] {
			t.Errorf("sound %q has no built-in spec", sfxTable[i].name)
		}
	}
}

// Two rows for one name silently collapse the volume and shape maps to the
// last writer. Aliasing is expressed by pointing two rows at one slot, never
// by repeating a name.
func TestSoundTableNamesUnique(t *testing.T) {
	seen := make(map[string]int, len(sfxTable))
	for i := range sfxTable {
		if j, dup := seen[sfxTable[i].name]; dup {
			t.Errorf("sound %q duplicated at rows %d and %d", sfxTable[i].name, j, i)
			continue
		}
		seen[sfxTable[i].name] = i
	}
}
