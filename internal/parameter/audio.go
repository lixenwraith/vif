package parameter

// Audio channel mask: a set bit means the channel is audible. The engine's
// per-channel mute flags stay authoritative; this is the composed view.
const (
	AudioChanSfx   uint8 = 1 << 0
	AudioChanMusic uint8 = 1 << 1
	AudioChanAll         = AudioChanSfx | AudioChanMusic
	AudioChanNone  uint8 = 0
)

// AudioBufferMsMin, Max and Step bound [audio] buffer_ms, the mixer period in
// milliseconds, 0 being the mixer's own: audio.AudioBufferMin to Max in Step, which
// a build without audio does not link, so these hold them and a test keeps them equal
const (
	AudioBufferMsMin  = 10
	AudioBufferMsMax  = 200
	AudioBufferMsStep = 10
)

// AudioMaskCycle advances the rotation: silence -> effects -> music -> all -> silence
func AudioMaskCycle(m uint8) uint8 {
	return (m + 1) & AudioChanAll
}
