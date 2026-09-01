package parameter

import (
	"time"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

const MaxPlayers = 16

// MaxPredictedCursorCells bounds D-18's outstanding local cursor predictions: the
// cells this instance has requested and not yet seen announced. One playout lead of
// input fits many times over, so reaching it means reconciliation has stopped
// arriving at all — a peer stalled, or a request the barrier never applied. The
// queue is dropped at that point and the local cell falls back to the store, which
// is what the participant saw before prediction existed.
const MaxPredictedCursorCells = 64

// Shield
const (
	// ShieldPassiveEnergyPercentDrain is the energy percentage of total per second while shield is active
	ShieldPassiveEnergyPercentDrain = 1

	// ShieldPassiveDrainInterval is the interval for passive shield drain
	ShieldPassiveDrainInterval = 1 * time.Second

	// ShieldBoostRotationDuration is the animation speed at which the boost indicator rotates once around the shield
	ShieldBoostRotationDuration = 500 * time.Millisecond
)

// Shield visuals
const (
	// PlayerShieldRadiusX is horizontal cell radius for shield and ember
	PlayerShieldRadiusX = 10.0
	// PlayerShieldRadiusY is vertical cell radius (aspect-corrected)
	PlayerShieldRadiusY = 5.0

	// ShieldMaxOpacity is peak alpha at ellipse edge
	ShieldMaxOpacity = 0.3

	// ShieldFeatherStartRatio is normalized distance where fade begins (0.85)
	ShieldFeatherStartRatio = 0.85
	// ShieldFeatherEndRatio is normalized distance where rendering stops (1.10)
	ShieldFeatherEndRatio = 1.10
)

// Weapon Cooldowns
const (
	WeaponCooldownMain      = 250 * time.Millisecond
	WeaponCooldownRod       = 500 * time.Millisecond
	WeaponCooldownLauncher  = 1000 * time.Millisecond
	WeaponCooldownDisruptor = 2000 * time.Millisecond
)

// Weapon Max Charges — component owns Charges storage, parameter owns the cap table
// (component already imports parameter; parameter importing component would cycle)
// Indexed by component.WeaponType ordinal: Rod=0, Launcher=1, Disruptor=2
const (
	WeaponMaxChargeRod       = 10
	WeaponMaxChargeLauncher  = 10
	WeaponMaxChargeDisruptor = 1
)

var WeaponMaxCharges = [3]int{WeaponMaxChargeRod, WeaponMaxChargeLauncher, WeaponMaxChargeDisruptor}

// Weapon Orb Configuration
const (
	// OrbOrbitRadiusX is horizontal orbital radius in cells
	OrbOrbitRadiusX = 12.0

	// OrbOrbitRadiusY is vertical orbital radius in cells (aspect-corrected)
	OrbOrbitRadiusY = 6.0

	// OrbOrbitRotationsPerSec is the orbit rate in full rotations per second
	OrbOrbitRotationsPerSec = 0.5

	// OrbOrbitSpeed is the orbit rate in radians per second
	OrbOrbitSpeed = OrbOrbitRotationsPerSec * vmath.TwoPi

	// OrbRedistributeDuration is time for orbs to animate to new positions
	OrbRedistributeDuration = 200 * time.Millisecond

	// OrbFlashDuration is visual flash duration when orb fires
	OrbFlashDuration = 100 * time.Millisecond

	// OrbCoronaRadiusX is horizontal glow radius in cells
	OrbCoronaRadiusX = 3.0

	// OrbCoronaRadiusY is vertical glow radius in cells (2:1 aspect)
	OrbCoronaRadiusY = 1.5

	// OrbBurstRadiusX is horizontal burst radius in cells
	OrbBurstRadiusX = 3.0

	// OrbBurstRadiusY is vertical burst radius in cells
	OrbBurstRadiusY = 1.5

	// OrbCoronaPeriodMs is corona rotation period (ms)
	OrbCoronaPeriodMs = int64(500)

	// OrbCoronaIntensity is peak corona glow alpha
	OrbCoronaIntensity = 0.6
)
