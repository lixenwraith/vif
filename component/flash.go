package component
// @lixen: #dev{feature[dust(render,system)]}

import "time"

// FlashComponent represents a brief visual flash effect when a character is removed
// Used by decay destruction, cleaner removal, and other visual feedback
type FlashComponent struct {
	X         int           // X position of the flash
	Y         int           // Y position of the flash
	Char      rune          // Character that was removed
	Remaining time.Duration // Time remaining
	Duration  time.Duration // Flash duration
}