package audio

import (
	"io"
	"io/fs"
	"time"
)

// DefaultSfxLevel is the level a sound plays at as sfx when SfxLevels names
// none; the embedder's mix lives in SfxLevels and SoundShapes, not here
const DefaultSfxLevel = 0.7

// AudioConfig holds engine configuration; the sample rate is the constant
// AudioSampleRate.
type AudioConfig struct {
	Enabled bool
	// SfxVolume scales every sound effect, 0..1; music plays at its own level
	SfxVolume float64
	// Keyed by sound name, as SoundIDs are assigned at Start: the level a sound
	// plays at as sfx, and the shaping it renders under, sfx or drum.
	SfxLevels    map[string]float64
	SoundShapes  map[string]SoundShape
	ForceBackend string
	// Sink is the host's speakers: a writer that takes the stream in place of any
	// backend found by name, closed with the engine.
	Sink io.WriteCloser
	// BaseSounds and BasePatterns are the banks registered at Start. The package
	// ships no specs or authored patterns of its own: the embedder owns them, and
	// the sounds document and PatternTOML override them by name.
	BaseSounds   []*SoundDef
	BasePatterns []*Pattern // their roles, groups and tiers are the drawn arrangement
	// SoundFS and SoundFile name the sounds document, read at Start with its
	// includes resolved inside SoundFS; a nil SoundFS reads none
	SoundFS     fs.FS
	SoundFile   string
	PatternTOML []byte // raw music.toml
	// Buffer is the mixer period; zero is AudioBufferDuration.
	Buffer time.Duration
}

// bufferFrames is how many frames one mixer period of d renders.
func bufferFrames(d time.Duration) int {
	return int(int64(AudioSampleRate) * int64(d) / int64(time.Second))
}

// DefaultAudioConfig returns a neutral configuration
func DefaultAudioConfig() *AudioConfig {
	return &AudioConfig{Enabled: false, SfxVolume: 0.5}
}
