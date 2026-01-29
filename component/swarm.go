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

// // SwarmPatternChars defines visual patterns for swarm composite
// var SwarmPatternChars = [3][2][4]rune{
// 	// Pattern 0: Pulse State A (Bold/Expanded) - "The Aggressor"
// 	{
// 		{'╔', '═', '═', '╗'},
// 		{'╚', '═', '═', '╝'},
// 	},
// 	// Pattern 1: Pulse State B (Thin/Contracted) - "The Drone"
// 	{
// 		{'┌', '─', '─', '┐'},
// 		{'└', '─', '─', '┘'},
// 	},
// 	// Pattern 2: Attack/Transition State (Mix) - "The Glitch"
// 	{
// 		{'╓', '─', '─', '╖'},
// 		{'╙', '─', '─', '╜'},
// 	},
// }

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