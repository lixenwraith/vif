package parameter

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
