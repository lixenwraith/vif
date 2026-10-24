//go:build !vif_headless && !vif_noaudio

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

// sfxSpec is one sound effect: the sound it plays, its level and its render shape
type sfxSpec struct {
	name  string
	slot  *audio.SoundID
	level float64
	shape audio.SoundShape
}

var sfxTable = []sfxSpec{
	{"error", &Sfx.Error, 0.7, audio.SoundShape{}},
	{"bell", &Sfx.Bell, 0.9, audio.SoundShape{Length: 0.85}},
	{"whoosh", &Sfx.Whoosh, 0.4, audio.SoundShape{Length: 0.8}},
	{"coin", &Sfx.Coin, 0.5, audio.SoundShape{}},
	{"shield", &Sfx.Shield, 0.7, audio.SoundShape{}},
	{"zap", &Sfx.Zap, 0.45, audio.SoundShape{}},
	{"crackle", &Sfx.Crackle, 0.55, audio.SoundShape{}},
	{"metalhit", &Sfx.MetalHit, 0.6, audio.SoundShape{}},
	{"explosion", &Sfx.Explosion, 0.6, audio.SoundShape{}},
	{"bullet", &Sfx.Bullet, 0.25, audio.SoundShape{}},
	{"ring", &Sfx.Ring, 0.6, audio.SoundShape{}},
}

var (
	SfxLevels = make(map[string]float64, len(sfxTable))
	SfxShapes = make(map[string]audio.SoundShape, len(sfxTable))
)

func init() {
	for i := range sfxTable {
		e := &sfxTable[i]
		SfxLevels[e.name] = e.level
		if e.shape != (audio.SoundShape{}) {
			SfxShapes[e.name] = e.shape
		}
	}
}

// ResolveSfx binds the game's names after the audio registry freezes.
func ResolveSfx() error {
	var missing []string
	for i := range sfxTable {
		e := &sfxTable[i]
		id := audio.SoundIDByName(e.name)
		if id == audio.SoundNone {
			missing = append(missing, e.name)
		}
		*e.slot = id
	}
	if len(missing) > 0 {
		return fmt.Errorf("sfx naming no sound: %s", strings.Join(missing, ", "))
	}
	return nil
}
