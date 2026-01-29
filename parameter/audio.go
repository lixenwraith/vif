package parameter

import "time"

// Audio Hardware Settings
const (
	AudioSampleRate    = 44100
	AudioChannels      = 2
	AudioBitDepth      = 16
	AudioBytesPerFrame = AudioChannels * (AudioBitDepth / 8) // 4 bytes
)

// Audio Engine Timing
const (
	// AudioBufferDuration determines latency and mixer tick rate
	// 50ms aligns with game tick
	AudioBufferDuration = 50 * time.Millisecond

	// AudioBufferSamples is frames per mixer tick at 44.1kHz
	AudioBufferSamples = (AudioSampleRate * 50) / 1000 // 2205

	// AudioDrainTimeout for queue cleanup on stop
	AudioDrainTimeout = 100 * time.Millisecond

	// MinSoundGap between consecutive sounds
	MinSoundGap = 50 * time.Millisecond
)

// TODO: standard format for sound effects
// Error Sound
const (
	ErrorSoundDuration = 80 * time.Millisecond
	ErrorSoundAttack   = 5 * time.Millisecond
	ErrorSoundRelease  = 20 * time.Millisecond
)

// Bell Sound
const (
	BellSoundDuration           = 600 * time.Millisecond
	BellSoundAttack             = 5 * time.Millisecond
	BellSoundFundamentalRelease = 550 * time.Millisecond
	BellSoundOvertoneRelease    = 200 * time.Millisecond
)

// Whoosh Sound
const (
	WhooshSoundDuration = 300 * time.Millisecond
	WhooshSoundAttack   = 150 * time.Millisecond
	WhooshSoundRelease  = 150 * time.Millisecond
)

// Coin Sound
const (
	CoinSoundNote1Duration = 80 * time.Millisecond
	CoinSoundNote2Duration = 280 * time.Millisecond
	CoinSoundAttack        = 5 * time.Millisecond
	CoinSoundNote1Release  = 40 * time.Millisecond
	CoinSoundNote2Release  = 200 * time.Millisecond
)

// Shield Deflect Sound
const (
	ShieldSoundDuration = 100 * time.Millisecond
	ShieldSoundAttack   = 2 * time.Millisecond
	ShieldSoundRelease  = 80 * time.Millisecond
	ShieldStartFreq     = 160.0 // Hz - Raised slightly for audibility
	ShieldEndFreq       = 40.0  // Hz
)

// Lightning Zap Sound (continuous)
const (
	ZapSoundDuration    = 180 * time.Millisecond // Halved for rapid re-triggering
	ZapSoundAttack      = 5 * time.Millisecond
	ZapSoundRelease     = 30 * time.Millisecond
	ZapModulationRate   = 25.0 // Hz - Faster buzz to fit shorter duration
	ZapCrackleIntensity = 0.3
)

// Lightning Crackle Sound (Short Bolt/Spark)
const (
	CrackleSoundDuration = 80 * time.Millisecond
	// Bursts replaced by impulse generation logic in generator
)

// Metal Hit Sound
const (
	MetalHitSoundDuration   = 120 * time.Millisecond
	MetalHitTransientLength = 4 * time.Millisecond
	MetalHitAttack          = 1 * time.Millisecond
	MetalHitDecayRate       = 40 * time.Millisecond
)