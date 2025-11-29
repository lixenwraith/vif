package constants

import "time"

// Audio Engine Timing
const (
	// AudioMonitorInterval is the interval for checking audio playback status
	AudioMonitorInterval = 10 * time.Millisecond

	// AudioDrainTimeout is the timeout for draining audio queues
	AudioDrainTimeout = 100 * time.Millisecond

	// MinSoundGap is the minimum gap between consecutive sounds (one clock tick)
	MinSoundGap = 50 * time.Millisecond
)

// Error Sound Timing
const (
	ErrorSoundDuration = 80 * time.Millisecond
	ErrorSoundAttack   = 5 * time.Millisecond
	ErrorSoundRelease  = 20 * time.Millisecond
)

// Bell Sound Timing
const (
	BellSoundDuration           = 600 * time.Millisecond
	BellSoundAttack             = 5 * time.Millisecond
	BellSoundFundamentalRelease = 550 * time.Millisecond
	BellSoundOvertoneRelease    = 200 * time.Millisecond
)

// Whoosh Sound Timing
const (
	WhooshSoundDuration = 300 * time.Millisecond
	WhooshSoundAttack   = 150 * time.Millisecond
	WhooshSoundRelease  = 150 * time.Millisecond
)

// Coin Sound Timing
const (
	CoinSoundNote1Duration = 80 * time.Millisecond
	CoinSoundNote2Duration = 280 * time.Millisecond
	CoinSoundAttack        = 5 * time.Millisecond
	CoinSoundNote1Release  = 40 * time.Millisecond
	CoinSoundNote2Release  = 200 * time.Millisecond
)
