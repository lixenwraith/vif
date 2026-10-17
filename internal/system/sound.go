//go:build !vif_headless && !vif_noaudio

package system

import (
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/audio"
)

// HandOverSound moves the speakers from a presented world to a copy rebuilt to
// replace it, which replayed without them. The engine plays on rather than
// rewinding, so the viewer's preference and the conductor's state go with it; the
// gates the copy replayed then decide what sounds.
func HandOverSound(to, from *engine.World) {
	ta, fa := systemOf[*AudioSystem](to), systemOf[*AudioSystem](from)
	if ta == nil || fa == nil {
		return
	}
	m, ok := ta.takeOver(fa)
	if !ok {
		return
	}
	if tm, fm := systemOf[*MusicSystem](to), systemOf[*MusicSystem](from); tm != nil && fm != nil {
		tm.takeOver(fm, m&parameter.AudioChanMusic != 0)
	}
}

// systemOf finds a world's system of type T, nil when it has none.
func systemOf[T engine.System](w *engine.World) T {
	var none T
	for _, s := range w.Systems() {
		if t, ok := s.(T); ok {
			return t
		}
	}
	return none
}

// publishAudioMask writes what the device sounds to the status registry. Audio and
// music both publish after device changes, including while gameplay is paused.
func publishAudioMask(world *engine.World, player *audio.AudioEngine) {
	m := parameter.AudioChanNone
	if player != nil && player.IsEnabled() {
		if !player.IsEffectMuted() {
			m |= parameter.AudioChanEffects
		}
		if !player.IsMusicMuted() && player.IsMusicPlaying() {
			m |= parameter.AudioChanMusic
		}
	}
	reg := world.Resources.Status
	mask := int64(m)
	if player == nil {
		mask = -1
	}
	reg.Ints.Get("audio.mask").Store(mask)
	reg.Bools.Get("audio.effect_muted").Store(m&parameter.AudioChanEffects == 0)
	reg.Bools.Get("audio.music_muted").Store(m&parameter.AudioChanMusic == 0)
}
