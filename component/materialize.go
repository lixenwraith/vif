package component

// MaterializeComponent represents a converging beam effect toward a spawn target
type MaterializeComponent struct {
	// Target area (beams converge to this rectangle)
	TargetX    int // Top-left X
	TargetY    int // Top-left Y
	AreaWidth  int // Target width (1 = single column)
	AreaHeight int // Target height (1 = single row)

	// Animation progress in Q32.32: 0 = start, Scale = complete
	Progress int64

	// Visual parameters
	BeamWidth int // Beam thickness perpendicular to direction (1 = thin)

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