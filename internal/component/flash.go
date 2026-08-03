package component

import "time"

// FlashComponent represents a brief visual flash effect of a character
type FlashComponent struct {
	Rune      rune          // Character that is flashed
	Remaining time.Duration // Time remaining
	Duration  time.Duration // Flash duration
}
