package component

// MaterializeComponent represents a converging ray effect toward a spawn target
type MaterializeComponent struct {
	// Target area (rays converge to this rectangle)
	TargetX    int // Top-left X
	TargetY    int // Top-left Y
	AreaWidth  int // Target width (1 = single column)
	AreaHeight int // Target height (1 = single row)

	// Animation progress: 0.0 = start, 1.0 = complete
	Progress float64

	// Visual parameters
	RayWidth int // Ray thickness perpendicular to direction (1 = thin)

	// Type of entity being spawned (for completion event)
	Type SpawnType
}

// SpawnType identifies what entity will be spawned upon materialization completion
type SpawnType int

const (
	SpawnTypeDrain SpawnType = iota
	SpawnTypeSwarm
	// Future: SpawnTypeQuasar, SpawnTypeBoss
)
