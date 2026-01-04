package core

// SoundType represents different sound effects
type SoundType int

const (
	SoundError    SoundType = iota // Typing error buzz
	SoundBell                      // Nugget collection
	SoundWhoosh                    // Cleaner activation
	SoundCoin                      // Gold complete
	SoundShield                    // Shield deflect
	SoundZap                       // Continuous lightning
	SoundCrackle                   // Short lightning bolt
	SoundMetalHit                  // Bullet on armor
	SoundTypeCount
)