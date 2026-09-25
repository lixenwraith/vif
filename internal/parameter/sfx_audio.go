//go:build !vif_headless && !vif_noaudio && !wasm

package parameter

import (
	"fmt"
	"strings"

	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/pkg/audio"
)

// BuiltinSounds parses the shipped sound bank.
func BuiltinSounds() ([]*audio.SoundDef, error) {
	return audio.LoadSoundsFS(asset.DefaultSounds, asset.DefaultSoundFiles...)
}

type soundSpec struct {
	name  string
	slot  *audio.SoundID
	vol   float64
	shape audio.SFXParams
}

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

// ResolveSounds binds the game's names after the audio registry freezes.
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
