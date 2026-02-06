package component

type EnemyType int

const (
	EnemySwarm EnemyType = iota
	EnemyQuasar
	EnemyDrain
)

type LootType int

const (
	LootRod LootType = iota
	LootLauncher
	// LootSpray placeholder for future
)

// LootComponent represents a collectible loot drop entity
type LootComponent struct {
	Type LootType

	// Visual
	Rune rune

	// Homing state
	Homing   bool
	PreciseX int64 // Q32.32
	PreciseY int64
	VelX     int64
	VelY     int64

	// Grid tracking
	LastIntX int
	LastIntY int
}