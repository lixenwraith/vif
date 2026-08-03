package audio

// DefaultEffectVolume is the neutral per-sound level shipped by the package.
// Embedders overlay EffectVolumes / EffectShapes at service wiring; no
// game-specific mix lives here
const DefaultEffectVolume = 0.7

// AudioConfig holds engine configuration
// SampleRate field dropped (write-only duplicate of AudioSampleRate)
type AudioConfig struct {
	Enabled      bool
	MasterVolume float64
	// Keyed by sound name: SoundIDs are assigned at Start, so the embedder
	// cannot hold one at config time.
	EffectVolumes map[string]float64
	EffectShapes  map[string]SFXParams
	ForceBackend  string
	PatternTOML   []byte // raw music.toml; nil = built-in patterns only
	SoundTOML     []byte // raw sounds.toml; nil = built-in sounds only
}

// DefaultAudioConfig returns a neutral configuration
func DefaultAudioConfig() *AudioConfig {
	return &AudioConfig{Enabled: false, MasterVolume: 0.5}
}
