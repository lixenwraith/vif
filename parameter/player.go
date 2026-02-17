package parameter

import (
	"time"
)

// Shield Defense Costs
const (
	ShieldRadiusXFloat = 10.0
	ShieldRadiusYFloat = 5.0
	ShieldMaxOpacity   = 0.3

	// ShieldPassiveEnergyPercentDrain is the energy percentage of total per second while shield is active
	ShieldPassiveEnergyPercentDrain = 1

	// ShieldPassiveDrainInterval is the interval for passive shield drain
	ShieldPassiveDrainInterval = 1 * time.Second

	// ShieldBoostRotationDuration is the animation speed at which the boost indicator rotates once around the shield
	ShieldBoostRotationDuration = 500 * time.Millisecond
)

// Weapon Cooldowns
const (
	WeaponCooldownMain      = 250 * time.Millisecond
	WeaponCooldownRod       = 500 * time.Millisecond
	WeaponCooldownLauncher  = 1000 * time.Millisecond
	WeaponCooldownDisruptor = 2000 * time.Millisecond
)

// Weapon Orb Configuration
const (
	// OrbOrbitRadiusXFloat is horizontal orbital radius in cells
	OrbOrbitRadiusXFloat = 12.0

	// OrbOrbitRadiusYFloat is vertical orbital radius in cells (aspect-corrected)
	OrbOrbitRadiusYFloat = 6.0

	// OrbOrbitSpeed is rotations per second (Q32.32 Scale = 1 rot/sec)
	OrbOrbitSpeedFloat = 0.5

	// OrbRedistributeDuration is time for orbs to animate to new positions
	OrbRedistributeDuration = 200 * time.Millisecond

	// OrbFlashDuration is visual flash duration when orb fires
	OrbFlashDuration = 100 * time.Millisecond

	// OrbCoronaRadiusXFloat is horizontal glow radius in cells
	OrbCoronaRadiusXFloat = 3.0

	// OrbCoronaRadiusYFloat is vertical glow radius in cells (2:1 aspect)
	OrbCoronaRadiusYFloat = 1.5

	// OrbBurstRadiusXFloat is horizontal burst radius in cells
	OrbBurstRadiusXFloat = 3.0

	// OrbBurstRadiusYFloat is vertical burst radius in cells
	OrbBurstRadiusYFloat = 1.5

	// OrbCoronaPeriodMs is corona rotation period (ms)
	OrbCoronaPeriodMs = int64(500)

	// OrbCoronaIntensity is peak corona glow alpha
	OrbCoronaIntensity = 0.6
)

// Cleaner Entity
const (
	// CleanerBaseHorizontalSpeed
	CleanerBaseHorizontalSpeedFloat = 80.0
	// CleanerBaseVerticalSpeed
	CleanerBaseVerticalSpeedFloat = 40.0

	// CleanerTrailLength is the number of previous positions tracked for the fade trail effect
	CleanerTrailLength = 10
)