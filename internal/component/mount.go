package component

import (
	"time"

	"github.com/lixenwraith/vif/internal/core"
)

// MountTrigger decides when a mounted weapon fires
type MountTrigger uint8

const (
	MountAuto  MountTrigger = iota // whenever ready and a cursor is in range
	MountArmed                     // only while its host holds Armed
)

// MountComponent is one weapon a Shared host carries: a species, a structure or an
// emitter. It is Shared state every instance re-derives; its shots and their hits
// are each instance's own, and a hit counts only on the cursor's owner (D-2, D-6).
type MountComponent struct {
	Weapon   WeaponType
	Trigger  MountTrigger
	Armed    bool          // the host's fire window, read under MountArmed
	Interval time.Duration // between discharges, counted on the host's cooldown timer
	Range    float64       // cells a target may be, horizontally; zero is unbounded
	Muzzle   float64       // cells from the host a shot leaves at, toward the aim
	Damage   CursorDamage  // what a hit costs a cursor; each zero field takes the kind's

	// A pulse kind fires Pulse, each zero field taking the kind's, and no other while
	// Live, the Shared pulse it last fired, lasts
	Pulse PulseSpec
	Live  core.Entity

	// Aim is the cursor cell the mount tracks, for renderers; HasAim is false while none is in range
	AimX, AimY int
	HasAim     bool

	// A ray's Lane fixes its direction (1-8 index vmath.Octants, 0 aims) and Width
	// its cells across; its cycle runs on the host's RayComponent
	Lane  uint8
	Width int
}
