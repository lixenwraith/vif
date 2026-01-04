package constant

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
	ShieldSoundAttack   = 3 * time.Millisecond
	ShieldSoundRelease  = 70 * time.Millisecond
	ShieldStartFreq     = 120.0 // Hz - lowered from 220
	ShieldEndFreq       = 35.0  // Hz - lowered from 55
)

// Lightning Zap Sound (continuous)
const (
	ZapSoundDuration    = 400 * time.Millisecond
	ZapSoundAttack      = 10 * time.Millisecond
	ZapSoundRelease     = 40 * time.Millisecond
	ZapModulationRate   = 14.0 // Hz - creates "zzZZzz" pulse
	ZapCrackleIntensity = 0.4
)

// Lightning Crackle Sound (short bolt)
const (
	CrackleSoundDuration = 60 * time.Millisecond
	CrackleBurstCount    = 5
	CrackleBurstDuration = 4 * time.Millisecond
	CrackleGapDuration   = 6 * time.Millisecond
)

// Metal Hit Sound
const (
	MetalHitSoundDuration   = 70 * time.Millisecond  // Reduced from 220ms
	MetalHitTransientLength = 5 * time.Millisecond   // Reduced from 8ms
	MetalHitAttack          = 500 * time.Microsecond // Faster attack
	MetalHitDecayRate       = 25 * time.Millisecond  // Fast decay constant
)