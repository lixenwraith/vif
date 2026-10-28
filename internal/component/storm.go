package component

import (
	"time"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/pkg/machine"
	"github.com/lixenwraith/vif/pkg/vmath"
)

const StormCircleCount = 3

// StormComponent marks the root storm controller entity, attached to container header entity (root)
type StormComponent struct {
	Circles      [StormCircleCount]core.Entity
	CirclesAlive [StormCircleCount]bool
}

// StormCircleState is a storm circle's place in its attack machine
type StormCircleState uint8

const (
	StormCircleCooldown StormCircleState = iota // waits out its cooldown, then for the convex
	StormCircleActive                           // runs its colour's attack, held in the convex
)

// StormCircleType derives circle behavior from index
type StormCircleType uint8

const (
	StormCircleGreen StormCircleType = iota // Index 0: area pulse
	StormCircleRed                          // Index 1: turret mount armed for each burst
	StormCircleBlue                         // Index 2: materializes a swarm in
)

// StormCircleComponent holds per-circle 3D physics state, attached to each circle header entity
type StormCircleComponent struct {
	Pos3D vmath.Vec3
	Vel3D vmath.Vec3
	Index int // 0, 1, or 2 - position in parent storm

	Machine machine.Instance[StormCircleState]

	IsInvulnerable bool // Written from the machine's flags each step: concave and not attacking
	// Anti-deadlock: continuous invulnerability duration, cleared when vulnerable
	InvulnerableElapsed time.Duration

	// Blue's landing, found as its attack opens; red aims through its turret mount
	// instead. The JSON names preserve the wire schema.
	AttackTargetX int `json:"LockedTargetX"`
	AttackTargetY int `json:"LockedTargetY"`
	LandingFound  bool
}
