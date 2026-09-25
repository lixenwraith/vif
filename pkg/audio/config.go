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
	// BaseSounds and BasePatterns are the banks registered at Start. The package
	// ships no specs or authored patterns of its own: the embedder owns them, and
	// SoundTOML and PatternTOML override them by name.
	BaseSounds   []*SoundDef
	BasePatterns []*Pattern // their roles, groups and tiers are the drawn arrangement
	SoundTOML    []byte     // raw sounds.toml
	PatternTOML  []byte     // raw music.toml
}

// DefaultAudioConfig returns a neutral configuration
func DefaultAudioConfig() *AudioConfig {
	return &AudioConfig{Enabled: false, MasterVolume: 0.5}
}
