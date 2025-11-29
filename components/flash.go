package components

import "time"

// FlashComponent represents a brief visual flash effect when a character is removed.
// Used by decay destruction, cleaner removal, and other visual feedback.
type FlashComponent struct {
	X         int       // X position of the flash
	Y         int       // Y position of the flash
	Char      rune      // Character that was removed
	StartTime time.Time // When the flash started
	Duration  int       // Flash duration in milliseconds
}