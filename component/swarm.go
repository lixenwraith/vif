package component

import (
	"time"
)

// SwarmState represents the swarm state machine phase
type SwarmState uint8

const (
	SwarmStateChase SwarmState = iota
	SwarmStateLock
	SwarmStateCharge
	SwarmStateDecelerate
)

// SwarmPatternChars defines visual patterns for swarm composite
// Index: [pattern][row][col]
// ▓ (U+2593) = active cell, ░ (U+2591) = inactive cell
var SwarmPatternChars = [3][2][4]rune{
	// Pattern 0: Solid
	{{'▓', '▓', '▓', '▓'}, {'▓', '▓', '▓', '▓'}},
	// Pattern 1: Hollow center
	{{'▓', '░', '░', '▓'}, {'▓', '░', '░', '▓'}},
	// Pattern 2: Checkerboard
	{{'▓', '░', '▓', '░'}, {'░', '▓', '░', '▓'}},
}

// SwarmPatternActive defines which cells are collision-active per pattern
// true = interacts with entities, false = visual only
var SwarmPatternActive = [3][2][4]bool{
	// Pattern 0: All active
	{{true, true, true, true}, {true, true, true, true}},
	// Pattern 1: Edges only
	{{true, false, false, true}, {true, false, false, true}},
	// Pattern 2: Alternating
	{{true, false, true, false}, {false, true, false, true}},
}

// SwarmComponent holds swarm-specific runtime state
type SwarmComponent struct {
	// State machine
	State SwarmState

	// Pattern cycling
	PatternIndex     int
	PatternRemaining time.Duration

	// Charge cycle timer (counts down to next lock phase)
	ChargeIntervalRemaining time.Duration

	// Lock phase
	LockRemaining time.Duration
	LockedTargetX int
	LockedTargetY int

	// Charge phase (for linear interpolation)
	ChargeRemaining time.Duration
	ChargeStartX    int64 // Q32.32 position at charge start
	ChargeStartY    int64
	ChargeTargetX   int64 // Q32.32 locked target position
	ChargeTargetY   int64

	// Deceleration phase
	DecelRemaining time.Duration

	// Lifecycle
	ChargesCompleted int
}