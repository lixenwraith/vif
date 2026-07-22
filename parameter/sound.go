package parameter

import (
	"fmt"
	"strings"

	"github.com/lixenwraith/vi-fighter/audio"
)

// SoundSet is the game's sound-effect ID table. The zero value is
// audio.SoundNone throughout, which Play rejects — a build where ResolveSounds
// never ran, or where audio is disabled, is silent rather than broken.
//
// Write/read discipline mirrors audio.fillIDs: written once on the wiring
// goroutine in AudioService.Start, read-only afterward from the tick
// goroutine, which is created later. Happens-before via goroutine creation;
// no lock, no atomic, nothing on the emit path.
type SoundSet struct {
	Error     audio.SoundID
	Bell      audio.SoundID
	Whoosh    audio.SoundID
	Coin      audio.SoundID
	Shield    audio.SoundID
	Zap       audio.SoundID
	Crackle   audio.SoundID
	MetalHit  audio.SoundID
	Explosion audio.SoundID
	Bullet    audio.SoundID
	Ring      audio.SoundID
}

// Sfx is the resolved table. Emit sites read parameter.Sfx.Error.
var Sfx SoundSet

// soundTable is the single source of truth for the game's sound policy: spec
// name, ID destination, mix level, render shaping. Adding a sound is one line
// here plus one field on SoundSet — a wrong field name fails to compile, and a
// name with no matching spec fails at ResolveSounds.
//
// Drums are registered too and addressable by name for shaping, but they are
// triggered by patterns, not events, so they have no SoundSet slot.
var soundTable = []struct {
	name  string
	slot  *audio.SoundID
	vol   float64
	shape audio.SFXParams
}{
	{"error", &Sfx.Error, 0.7, audio.SFXParams{}},
	{"bell", &Sfx.Bell, 0.9, audio.SFXParams{Length: 0.85}},
	{"whoosh", &Sfx.Whoosh, 0.4, audio.SFXParams{Length: 0.8}},
	{"coin", &Sfx.Coin, 0.5, audio.SFXParams{}},
	{"shield", &Sfx.Shield, 0.7, audio.SFXParams{}},
	{"zap", &Sfx.Zap, 0.45, audio.SFXParams{}},
	{"crackle", &Sfx.Crackle, 0.55, audio.SFXParams{}},
	{"metalhit", &Sfx.MetalHit, 0.6, audio.SFXParams{}},
	{"explosion", &Sfx.Explosion, 0.6, audio.SFXParams{}},
	{"bullet", &Sfx.Bullet, 0.25, audio.SFXParams{}},
	{"ring", &Sfx.Ring, 0.6, audio.SFXParams{}},
}

// GameEffectVolumes / GameEffectShapes are the embedder-facing config maps,
// derived from soundTable so the names cannot drift apart. Built at package
// init, read by AudioService.Init before Start.
var (
	GameEffectVolumes = make(map[string]float64, len(soundTable))
	GameEffectShapes  = make(map[string]audio.SFXParams, len(soundTable))
)

func init() {
	for i := range soundTable {
		e := &soundTable[i]
		GameEffectVolumes[e.name] = e.vol
		if e.shape != (audio.SFXParams{}) {
			GameEffectShapes[e.name] = e.shape
		}
	}
}

// ResolveSounds fills Sfx from the audio registry. Must run after
// AudioEngine.Start has registered specs and before any system emits.
// A name with no spec resolves to SoundNone (silent) and is reported: that is
// a build-time mismatch between soundTable and the TOML, not a runtime state.
func ResolveSounds() error {
	var missing []string
	for i := range soundTable {
		e := &soundTable[i]
		id := audio.SoundIDByName(e.name)
		if id == audio.SoundNone {
			missing = append(missing, e.name)
		}
		*e.slot = id
	}
	if len(missing) > 0 {
		return fmt.Errorf("unresolved sound specs: %s", strings.Join(missing, ", "))
	}
	return nil
}
