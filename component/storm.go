package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/vmath"
)

const StormCircleCount = 3

// StormComponent marks the root storm controller entity, attached to container header entity (root)
type StormComponent struct {
	Circles      [StormCircleCount]core.Entity
	CirclesAlive [StormCircleCount]bool
}

// StormCircleAttackState represents attack phase for a storm circle
type StormCircleAttackState uint8

const (
	StormCircleAttackIdle StormCircleAttackState = iota
	StormCircleAttackCooldown
	StormCircleAttackActive
)

// StormCircleType derives circle behavior from index
type StormCircleType uint8

const (
	StormCircleGreen StormCircleType = iota // Index 0: area pulse
	StormCircleRed                          // Index 1: cone projectile
	StormCircleBlue                         // Index 2: TBD/no-op
)

// StormCircleComponent holds per-circle 3D physics state, attached to each circle header entity
type StormCircleComponent struct {
	Pos3D vmath.Vec3
	Vel3D vmath.Vec3
	Index int // 0, 1, or 2 - position in parent storm

	IsInvulnerable bool
	// Anti-deadlock: tracks continuous invulnerability duration
	InvulnerableSince int64 // Unix nano timestamp, 0 if vulnerable

	// Attack state machine
	AttackState       StormCircleAttackState
	CooldownRemaining time.Duration
	AttackRemaining   time.Duration

	// Red cone targeting (locked at attack start)
	LockedTargetX int
	LockedTargetY int

	// Visual data for renderer (0.0-1.0 progress)
	AttackProgress float64
}