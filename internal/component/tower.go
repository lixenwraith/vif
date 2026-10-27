package component

// TowerType identifies the visual color variant
type TowerType uint8

const (
	TowerCyan TowerType = iota
	TowerGold
	TowerViolet
	TowerEmerald
	TowerTypeCount // must be last
)

// TowerTypeNames are what a document calls each TowerType
var TowerTypeNames = [TowerTypeCount]string{TowerCyan: "cyan", TowerGold: "gold", TowerViolet: "violet", TowerEmerald: "emerald"}

// TowerComponent holds tower-specific runtime state
// Tower is a stationary ablative composite the players defend, attacked by eyes
type TowerComponent struct {
	// Spawn parameters preserved for death event and renderer
	SpawnX  int
	SpawnY  int
	RadiusX int
	RadiusY int
	MinHP   int
	MaxHP   int
	Type    TowerType
}
