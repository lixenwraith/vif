package parameter

import (
	"fmt"
	"strings"

	"github.com/lixenwraith/vi-fighter/pkg/audio"
)

// SoundSet is the game's sound-effect ID table. The zero value is
// audio.SoundNone throughout, which Play rejects — a build where ResolveSounds
// never ran, or where audio is disabled, is silent rather than broken.
//
// That silence is indistinguishable from a correctly muted engine at the emit
// site: AudioEngine.Play discards a SoundNone before any counter, so nothing
// in played/dropped moves. AudioService.Start therefore treats an unresolved
// table as fatal, and sfx_test.go asserts every field has a soundTable row.
//
// Write/read discipline: written once on the wiring goroutine in
// AudioService.Start, read-only afterward from the tick and render goroutines,
// which are created later. Happens-before via goroutine creation; no lock, no
// atomic, nothing on the emit path.
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
//
// Systems must read through this variable at emit time, not cache a field into
// their own struct at construction: manifest.BuildSystems runs during
// App.init, before Hub.StartAll resolves the table.
var Sfx SoundSet

// soundSpec binds one registry name to its ID destination and mix policy.
type soundSpec struct {
	name  string
	slot  *audio.SoundID
	vol   float64
	shape audio.SFXParams
}

// soundTable is the single source of truth for the game's sound policy: spec
// name, ID destination, mix level, render shaping. Adding a sound is one line
// here plus one field on SoundSet — a wrong field name fails to compile, a
// field with no row fails in sfx_test.go, and a name with no matching spec
// fails at ResolveSounds.
//
// Drums are registered too and addressable by name for shaping, but they are
// triggered by patterns, not events, so they have no SoundSet slot.
var soundTable = []soundSpec{
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
// AudioEngine.Start has registered specs and frozen the registry, and before
// any system emits EventSoundRequest.
//
// Registration completes before backend selection inside AudioEngine.Start, so
// this resolves on the ErrNoAudioBackend path too: IDs are valid even when the
// engine latched silent mode, and a later failover is audible without a
// re-resolve.
//
// A name with no spec is a mismatch between soundTable and the built-in TOML —
// a build-time error, not a runtime state. The user's sounds.toml overrides
// existing names and cannot remove one, so it cannot produce this. Unresolved
// slots are still written (SoundNone), keeping the call idempotent and correct
// across a registry reset.
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
