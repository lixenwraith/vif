package component

import "time"

// FlashComponent represents a brief visual flash effect when a character is removed
type FlashComponent struct {
	X         int           // X position of the flash
	Y         int           // Y position of the flash
	Char      rune          // Character that was removed
	Remaining time.Duration // Time remaining
	Duration  time.Duration // Flash duration
}