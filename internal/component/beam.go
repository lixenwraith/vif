package component

import (
	"time"

	"github.com/lixenwraith/vif/pkg/vmath"
)

// BeamPhase is where a beam is: warning before it strikes, or firing
type BeamPhase uint8

const (
	BeamWarning BeamPhase = iota
	BeamFiring
)

// BeamComponent is a beam being fired, on the orb a cursor's leaves through or the
// Shared host a mounted one leaves from. Its weapon rewrites it each tick and removes
// it when the beam ends; renderers only read it.
type BeamComponent struct {
	Ray         vmath.Ray
	Phase       BeamPhase
	Remaining   time.Duration // of this phase
	Duration    time.Duration // of this phase
	HitInterval time.Duration // between strikes; zero strikes every tick
	HitTimer    time.Duration // until the next strike
	Scale       int           // damage multiplier, the charges a cursor fired it with
	Palette     WeaponPalette
}
