package component

// TowerComponent holds tower-specific runtime state
// Tower is a stationary ablative composite owned by player, attacked by eyes
type TowerComponent struct {
	// Spawn parameters preserved for death event and renderer
	SpawnX  int
	SpawnY  int
	RadiusX int
	RadiusY int
	MinHP   int
	MaxHP   int
}