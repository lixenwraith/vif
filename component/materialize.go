package component
// @lixen: #dev{feat[drain(render,system)]}

// MaterializeComponent represents a converging beam effect toward a spawn target
// Single entity manages all 4 cardinal beams via progress-based rendering
type MaterializeComponent struct {
	// Target position (convergence point)
	TargetX, TargetY int

	// Animation progress in Q16.16: 0 = start, Scale = complete
	Progress int32

	// Beam width in cells (1 = single line, 3 = wide beam)
	Width int

	// Type of entity being spawned (for completion event)
	Type SpawnType
}

// SpawnType identifies what entity will be spawned upon materialization completion
type SpawnType int

const (
	SpawnTypeDrain SpawnType = iota
	// Future: SpawnTypeNugget, SpawnTypeBot, etc.
)