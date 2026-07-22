package audio

import (
	"fmt"
	"sync"
)

// SoundID identifies a registered sound. IDs are process-local: assigned by
// registration order, stable across a run, never persisted or transmitted.
// SoundNone is the zero value and always renders silence.
type SoundID int32

const SoundNone SoundID = 0

var (
	soundMu     sync.RWMutex
	soundDefs   = []*SoundDef{nil} // index 0 is the SoundNone sentinel
	soundByName = make(map[string]SoundID)
	soundFrozen bool
)

// RegisteredSounds snapshots the registry in ID order, starting at the first
// real sound (the SoundNone sentinel is omitted). Setup and editor path only.
func RegisteredSounds() []*SoundDef { return registeredSounds()[1:] }

// ResetRegistries clears the sound and pattern registries and unfreezes sound
// registration, so a later AudioEngine.Start rebuilds both from scratch.
//
// Process-wide, unsynchronized against a running mixer, and it invalidates
// every SoundID and PatternID previously handed out — including the ones cached
// in parameter.Sfx, which must be re-resolved. Call it only with no engine
// running: after Stop, before the next Start. The game never calls this; it
// exists for editors and tests, which build and tear down engines repeatedly in
// one process.
func ResetRegistries() {
	resetSoundRegistry()
	resetPatternRegistry()
}

// RegisterSound validates and installs a spec. An existing name is replaced in
// place, keeping its ID, mirroring RegisterPattern. Registration closes before
// rendering: the cache and mixer size their tables from the registry, so a late
// insert would race the mix goroutine.
func RegisterSound(d *SoundDef) (SoundID, error) {
	if err := ValidateSound(d); err != nil {
		return SoundNone, err
	}
	soundMu.Lock()
	defer soundMu.Unlock()
	if soundFrozen {
		return SoundNone, fmt.Errorf("sound %q: registry frozen after Start", d.Name)
	}
	if id, ok := soundByName[d.Name]; ok {
		soundDefs[id] = d
		return id, nil
	}
	id := SoundID(len(soundDefs))
	soundDefs = append(soundDefs, d)
	soundByName[d.Name] = id
	return id, nil
}

// SoundIDByName resolves a name; SoundNone if absent. Setup path only —
// callers cache the result and pass IDs on the hot path.
func SoundIDByName(name string) SoundID {
	soundMu.RLock()
	defer soundMu.RUnlock()
	return soundByName[name]
}

func SoundName(id SoundID) string {
	soundMu.RLock()
	defer soundMu.RUnlock()
	if id <= 0 || int(id) >= len(soundDefs) {
		return ""
	}
	return soundDefs[id].Name
}

// registeredSounds snapshots the registry in ID order; index 0 is nil.
func registeredSounds() []*SoundDef {
	soundMu.RLock()
	defer soundMu.RUnlock()
	out := make([]*SoundDef, len(soundDefs))
	copy(out, soundDefs)
	return out
}

func freezeSounds() {
	soundMu.Lock()
	soundFrozen = true
	soundMu.Unlock()
}

// resetSoundRegistry exists for tests.
func resetSoundRegistry() {
	soundMu.Lock()
	soundDefs = []*SoundDef{nil}
	soundByName = make(map[string]SoundID)
	soundFrozen = false
	soundMu.Unlock()
}
