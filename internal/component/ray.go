package component

import (
	"time"

	"github.com/lixenwraith/vif/pkg/vmath"
)

// RayPhase is where a ray is: warning before it strikes, or firing
type RayPhase uint8

const (
	RayWarning RayPhase = iota
	RayFiring
)

// RayComponent is a ray being fired, on the orb a cursor's leaves through or the
// Shared host a mounted one leaves from. Its weapon rewrites it each tick and removes
// it when the ray ends; renderers only read it.
type RayComponent struct {
	Ray         vmath.Ray
	Phase       RayPhase
	Remaining   time.Duration // of this phase
	Duration    time.Duration // of this phase
	HitInterval time.Duration // between strikes; zero strikes every tick
	HitTimer    time.Duration // until the next strike
	Scale       int           // damage multiplier, the charges a cursor fired it with
	Palette     WeaponPalette
}
