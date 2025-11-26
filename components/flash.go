package components

import "time"

// TODO: Use it for all character destruction effect, not only cleaner, i.e. decay, drain
// FlashComponent represents a brief visual flash effect when a character is removed
type FlashComponent struct {
	X         int       // X position of the flash
	Y         int       // Y position of the flash
	Char      rune      // Character that was removed
	StartTime time.Time // When the flash started
	Duration  int       // Flash duration in milliseconds
}