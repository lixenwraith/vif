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
)

// Buff Cooldowns
const (
	BuffCooldownRod      = 500 * time.Millisecond
	BuffCooldownLauncher = 1000 * time.Millisecond
	BuffCooldownChain    = 2000 * time.Millisecond
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