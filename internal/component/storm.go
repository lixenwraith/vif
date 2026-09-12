package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
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
	StormCircleRed                          // Index 1: directional projectile burst
	StormCircleBlue                         // Index 2: TBD/no-op
)

// StormCircleComponent holds per-circle 3D physics state, attached to each circle header entity
type StormCircleComponent struct {
	Pos3D vmath.Vec3F
	Vel3D vmath.Vec3F
	Index int // 0, 1, or 2 - position in parent storm

	IsInvulnerable bool
	// Anti-deadlock: continuous invulnerability duration, cleared when vulnerable
	InvulnerableElapsed time.Duration

	// Attack state machine
	AttackState       StormCircleAttackState
	CooldownRemaining time.Duration
	AttackRemaining   time.Duration

	// Red refreshes these Shared aim coordinates as its target moves. Blue reuses
	// them for its fixed spawn target. The JSON names preserve the wire schema.
	AttackTargetX int `json:"LockedTargetX"`
	AttackTargetY int `json:"LockedTargetY"`

	// Visual data for renderer (0.0-1.0 progress)
	AttackProgress float64
}
