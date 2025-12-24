package core

// SoundType represents different sound effects
type SoundType int

const (
	SoundError  SoundType = iota // Typing error buzz
	SoundBell                    // Nugget collection
	SoundWhoosh                  // Cleaner activation
	SoundCoin                    // Gold complete
	SoundTypeCount
)