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

// BinauralMode is how the binaural bed under the music takes its beat: no bed, a
// preset's beat, or the music's tempo's
type BinauralMode uint8

const (
	BinauralOff BinauralMode = iota
	BinauralFixed
	BinauralDynamic
)

// BinauralModeNames spells the modes as [play] binaural and :binaural do
var BinauralModeNames = [...]string{BinauralOff: "off", BinauralFixed: "fixed", BinauralDynamic: "dynamic"}

// BinauralPreset is a row of BinauralPresets
type BinauralPreset uint8

// BinauralPresets are the beats a fixed bed plays, in Hz, a row a preset, which
// [play] binaural_preset and :binaural name
var BinauralPresets = [...]struct {
	Name string
	Beat float64
}{
	{"relax", 10},
	{"focus", 15},
	{"hyper", 25},
}

// BinauralLevel is the bed's level in each ear, of full scale, whatever its mode
// and preset: some 10 dB under the calm tier's music and 15 under the peak's
const BinauralLevel = 0.03

// BinauralPresetNames lists the presets' names in the table's order
func BinauralPresetNames() []string {
	names := make([]string, len(BinauralPresets))
	for i, p := range BinauralPresets {
		names[i] = p.Name
	}
	return names
}

// AudioMaskCycle advances the rotation: silence -> effects -> music -> all -> silence
func AudioMaskCycle(m uint8) uint8 {
	return (m + 1) & AudioChanAll
}
