package parameter

import "github.com/lixenwraith/vi-fighter/audio"

// Game audio mix policy. The audio package ships neutral defaults; these maps
// overlay them at service wiring (service/audio.go). Per-sound shaping lets the
// game shorten or retune any preset without touching the audio package.

// MusicConfigFile is the pattern override file, loaded at audio service start
const MusicConfigFile = "music.toml"

// GameEffectVolumes: per-sound levels, multiplied into MasterVolume at Play
// Bullet is the rapid-fire sound: lowest level; mixer dampening does the rest
var GameEffectVolumes = map[audio.SoundType]float64{
	audio.SoundError:     0.7,
	audio.SoundBell:      0.9,
	audio.SoundWhoosh:    0.4,
	audio.SoundCoin:      0.5,
	audio.SoundShield:    0.7,
	audio.SoundZap:       0.45,
	audio.SoundCrackle:   0.55,
	audio.SoundMetalHit:  0.6,
	audio.SoundExplosion: 0.6,
	audio.SoundBullet:    0.35,
	audio.SoundRing:      0.6,
}

// GameEffectShapes is per-sound preset shaping applied once at render time
// Only presets whose duration derives from the decay multiplier honor Length
var GameEffectShapes = map[audio.SoundType]audio.SFXParams{
	audio.SoundWhoosh: {Length: 0.8},
	audio.SoundBell:   {Length: 0.85},
}

// Audio channel mask: a set bit means the channel is audible. The engine's
// per-channel mute flags stay authoritative; this is the composed view.
const (
	AudioChanEffects uint8 = 1 << 0
	AudioChanMusic   uint8 = 1 << 1
	AudioChanAll           = AudioChanEffects | AudioChanMusic
	AudioChanNone    uint8 = 0
)

// AudioMaskCycle advances the rotation: all -> music -> effects -> silence -> all
func AudioMaskCycle(m uint8) uint8 { return (m - 1) & AudioChanAll }
