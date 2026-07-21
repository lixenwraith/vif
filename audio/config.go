package audio

// DefaultEffectVolume is the neutral per-sound level shipped by the package.
// Embedders overlay EffectVolumes / EffectShapes at service wiring; no
// game-specific mix lives here
const DefaultEffectVolume = 0.7

// AudioConfig holds engine configuration
// SampleRate field dropped (write-only duplicate of AudioSampleRate)
type AudioConfig struct {
	Enabled       bool
	MasterVolume  float64
	EffectVolumes map[SoundType]float64
	EffectShapes  map[SoundType]SFXParams // per-sound render shaping
	ForceBackend  string
	PatternTOML   []byte // raw music.toml content; nil = built-ins only
}

// DefaultAudioConfig returns a neutral configuration
// CHANGED: uniform DefaultEffectVolume; the game's per-sound levels moved to
// parameter.GameEffectVolumes, applied by service/audio.go
func DefaultAudioConfig() *AudioConfig {
	vols := make(map[SoundType]float64, SoundTypeCount)
	for st := SoundType(0); st < SoundTypeCount; st++ {
		vols[st] = DefaultEffectVolume
	}
	return &AudioConfig{
		Enabled:       false,
		MasterVolume:  0.5,
		EffectVolumes: vols,
		EffectShapes:  nil,
	}
}
