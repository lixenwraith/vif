package parameter

// Pylon Entity
const (
	// PylonDefaultRadiusX is default horizontal radius (cells)
	PylonDefaultRadiusX = 6

	// PylonDefaultRadiusY is default vertical radius (cells, aspect-corrected)
	PylonDefaultRadiusY = 3

	// PylonCollisionRadiusX is horizontal collision zone (cells)
	PylonCollisionRadiusX = 4.0

	// PylonCollisionRadiusY is vertical collision zone (cells, aspect-corrected)
	PylonCollisionRadiusY = 2.0

	// PylonSpawnMaxAttempts is random position attempts before spiral fallback
	PylonSpawnMaxAttempts = 30

	// PylonSpawnSpiralMaxRadius is max search distance for spiral fallback
	PylonSpawnSpiralMaxRadius = 30

	// CombatInitialHPPylonMin is pylon member HP at edge (default)
	CombatInitialHPPylonMin = 10

	// CombatInitialHPPylonMax is pylon member HP at center (default)
	CombatInitialHPPylonMax = 100
)

// Pylon interaction
const (
	// PylonShieldDrain is energy drained per tick when pylon members overlap shield
	PylonShieldDrain = 50

	// PylonDamageHeat is heat removed on cursor contact without shield
	PylonDamageHeat = 10
)
