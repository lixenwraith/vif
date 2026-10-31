package audio

import (
	"fmt"
	"sync"

	"github.com/lixenwraith/vif/pkg/audio/model"
)

// SoundID identifies a registered sound. IDs are process-local: assigned by
// registration order, stable across a run, never persisted or transmitted.
// SoundNone is the zero value and always renders silence.
type SoundID = model.SoundID

const SoundNone = model.SoundNone

var (
	soundMu     sync.RWMutex
	soundDefs   = []*SoundDef{nil} // index 0 is the SoundNone sentinel
	soundByName = make(map[string]SoundID)
	soundFrozen bool
)

// RegisteredSounds snapshots the registry in ID order, starting at the first
// real sound (the SoundNone sentinel is omitted). Setup and editor path only.
func RegisteredSounds() []*SoundDef { return registeredSounds()[1:] }

// ResetRegistries clears the sound, voice and pattern registries and unfreezes
// sound registration for a later Start, invalidating every SoundID, PatternID and
// added instrument handed out, an embedder's cached ones included. Process-wide
// and unsynchronized: call it only with no engine running, between a Stop and the
// next Start, as editors and tests do.
func ResetRegistries() {
	resetSoundRegistry()
	resetVoiceRegistry()
	resetPatternRegistry()
}

// RegisterSound validates and installs a spec, replacing a known name in place
// under its ID as RegisterPattern does. The freeze taken at Start refuses new
// names only: the cache, the mixer's per-SoundID tables and the engine's levels
// are sized then, and a replacement changes none of them.
func RegisterSound(d *SoundDef) (SoundID, error) {
	if err := ValidateSound(d); err != nil {
		return SoundNone, err
	}
	soundMu.Lock()
	defer soundMu.Unlock()
	if _, known := soundByName[d.Name]; !known && soundFrozen {
		return SoundNone, fmt.Errorf("sound %q: registry frozen after Start", d.Name)
	}
	return insertLocked(d), nil
}

// defineSound registers or replaces regardless of freeze state. A new name
// after Start yields an ID beyond every table sized at Start, so the caller
// MUST propagate it to all of them. AudioEngine.DefineSound is the only caller
// and does exactly that; nothing else should call this.
func defineSound(d *SoundDef) (SoundID, error) {
	if err := ValidateSound(d); err != nil {
		return SoundNone, err
	}
	soundMu.Lock()
	defer soundMu.Unlock()
	return insertLocked(d), nil
}

func insertLocked(d *SoundDef) SoundID {
	if id, ok := soundByName[d.Name]; ok {
		soundDefs[id] = d
		return id
	}
	id := SoundID(len(soundDefs))
	soundDefs = append(soundDefs, d)
	soundByName[d.Name] = id
	return id
}

// SoundDefByName returns the live registry entry, or nil. The pointer is shared
// with the render path — mutate a Clone, then DefineSound it.
func SoundDefByName(name string) *SoundDef {
	soundMu.RLock()
	defer soundMu.RUnlock()
	id, ok := soundByName[name]
	if !ok {
		return nil
	}
	return soundDefs[id]
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
