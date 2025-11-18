package components

import "time"

// RemovalFlashComponent represents a brief visual flash effect when a red character
// is removed by a cleaner. The flash provides visual feedback for the removal action.
type RemovalFlashComponent struct {
	X         int       // X position of the flash
	Y         int       // Y position of the flash
	Char      rune      // Character that was removed
	StartTime time.Time // When the flash started
	Duration  int       // Flash duration in milliseconds
}
