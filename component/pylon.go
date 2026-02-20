package component

// PylonComponent holds pylon-specific runtime state
// Pylon is a stationary ablative composite that acts as damage sponge
type PylonComponent struct {
	// Spawn parameters preserved for death event position
	SpawnX int // Center X
	SpawnY int // Center Y
	Radius int
	MinHP  int // HP at edge, preserved for renderer
	MaxHP  int // HP at center, preserved for renderer
}