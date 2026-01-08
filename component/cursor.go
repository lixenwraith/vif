package component

import (
	"time"
)

// CursorComponent marks an entity as the player cursor (singleton)
type CursorComponent struct {
	// ErrorFlashRemaining is the duration remaining for the error flash
	ErrorFlashRemaining time.Duration
}