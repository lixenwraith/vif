// @focus: #conf { audio } #sys { audio }
package constants

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