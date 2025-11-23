package constants

import "time"

// Audio Engine Timing Constants
const (
	// AudioMonitorInterval is the interval for checking audio playback status
	AudioMonitorInterval = 10 * time.Millisecond

	// AudioDrainTimeout is the timeout for draining audio queues
	AudioDrainTimeout = 100 * time.Millisecond

	// MinSoundGap is the minimum gap between consecutive sounds (one clock tick)
	MinSoundGap = 50 * time.Millisecond
)

// Error Sound Timing Constants
const (
	// ErrorSoundDuration is the total duration of the error sound effect
	ErrorSoundDuration = 80 * time.Millisecond

	// ErrorSoundAttack is the attack phase duration for error sound
	ErrorSoundAttack = 5 * time.Millisecond

	// ErrorSoundRelease is the release phase duration for error sound
	ErrorSoundRelease = 20 * time.Millisecond
)

// Bell Sound Timing Constants
const (
	// BellSoundDuration is the total duration of the bell sound effect
	BellSoundDuration = 600 * time.Millisecond

	// BellSoundAttack is the attack phase duration for bell sound
	BellSoundAttack = 5 * time.Millisecond

	// BellSoundFundamentalRelease is the release phase for fundamental frequency
	BellSoundFundamentalRelease = 550 * time.Millisecond

	// BellSoundOvertoneRelease is the release phase for overtone frequency
	BellSoundOvertoneRelease = 200 * time.Millisecond
)

// Whoosh Sound Timing Constants
const (
	// WhooshSoundDuration is the total duration of the whoosh sound effect
	WhooshSoundDuration = 300 * time.Millisecond

	// WhooshSoundAttack is the attack phase duration for whoosh sound
	WhooshSoundAttack = 150 * time.Millisecond

	// WhooshSoundRelease is the release phase duration for whoosh sound
	WhooshSoundRelease = 150 * time.Millisecond
)

// Coin Sound Timing Constants
const (
	// CoinSoundNote1Duration is the duration of the first note
	CoinSoundNote1Duration = 80 * time.Millisecond

	// CoinSoundNote2Duration is the duration of the second note
	CoinSoundNote2Duration = 280 * time.Millisecond

	// CoinSoundAttack is the attack phase duration for both notes
	CoinSoundAttack = 5 * time.Millisecond

	// CoinSoundNote1Release is the release phase for the first note
	CoinSoundNote1Release = 40 * time.Millisecond

	// CoinSoundNote2Release is the release phase for the second note
	CoinSoundNote2Release = 200 * time.Millisecond
)
