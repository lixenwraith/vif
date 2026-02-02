package parameter

import (
	"time"
)

// Shield Defense Costs
const (
	ShieldRadiusX    = 10
	ShieldRadiusY    = 5
	ShieldMaxOpacity = 0.3

	// ShieldPassiveEnergyPercentDrain is the energy percentage of total per second while shield is active
	ShieldPassiveEnergyPercentDrain = 1

	// ShieldPassiveDrainInterval is the interval for passive shield drain
	ShieldPassiveDrainInterval = 1 * time.Second

	// ShieldBoostRotationDuration is the animation speed at which the boost indicator rotates once around the shield
	ShieldBoostRotationDuration = 500 * time.Millisecond
)

// Buff Cooldowns
const (
	BuffCooldownRod      = 500 * time.Millisecond
	BuffCooldownLauncher = 1000 * time.Millisecond
	BuffCooldownChain    = 2000 * time.Millisecond
)

// Buff Orb Configuration
const (
	// OrbChar is the character used for buff orbs
	OrbChar = '●' // U+25CF Black Circle

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
)

// Cleaner Entity
const (
	// CleanerChar is the character used to render the cleaner block
	CleanerChar = '█'

	// CleanerBaseHorizontalSpeed
	CleanerBaseHorizontalSpeedFloat = 80.0
	// CleanerBaseVerticalSpeed
	CleanerBaseVerticalSpeedFloat = 40.0

	// CleanerTrailLength is the number of previous positions tracked for the fade trail effect
	CleanerTrailLength = 10
)