package component

import (
	"time"
)

// ControlKind identifies the producer authorized to drive a cursor
type ControlKind uint8

const (
	ControlHuman  ControlKind = iota // Local input via the mode router
	ControlBot                       // In-simulation bot, emits inside the tick
	ControlRemote                    // Network peer
)

// CursorComponent marks an entity as a player cursor
type CursorComponent struct {
	// ErrorFlashRemaining is the duration remaining for the error flash
	ErrorFlashRemaining time.Duration

	// Slot is the roster index this cursor occupies
	Slot uint8

	// Control identifies what drives this cursor
	Control ControlKind

	// PeerID names the remote owner when Control is ControlRemote
	PeerID uint32
}
