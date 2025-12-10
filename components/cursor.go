// @focus: #core { ecs, types } #game { cursor }
package components

import (
	"time"
)

// CursorComponent marks an entity as the player cursor (singleton)
// Contains cursor-specific state that was previously in GameContext
type CursorComponent struct {
	// ErrorFlashRemaining is the duration remaining for the error flash
	// Zero value means no flash active
	ErrorFlashRemaining time.Duration
}