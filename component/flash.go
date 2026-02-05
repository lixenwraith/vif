package component

import "time"

// FlashComponent represents a brief visual flash effect of a character
type FlashComponent struct {
	X         int           // X position of the flash
	Y         int           // Y position of the flash
	Rune      rune          // Character that is flashed
	Remaining time.Duration // Time remaining
	Duration  time.Duration // Flash duration
}