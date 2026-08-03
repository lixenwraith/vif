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
	SwarmStateTeleport
	SwarmStateDecelerate
)

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

	// Teleport phase
	TeleportRemaining time.Duration
	TeleportStartX    int
	TeleportStartY    int
	TeleportTargetX   int
	TeleportTargetY   int

	// Lifecycle
	ChargesCompleted int
}