package component

// SpeciesType identifies a gameplay species across lifecycle, navigation,
// genetics, loot, and telemetry systems.
type SpeciesType uint8

const (
	SpeciesNone SpeciesType = iota
	SpeciesDrain
	SpeciesSwarm
	SpeciesQuasar
	SpeciesStorm
	SpeciesPylon
	SpeciesSnake
	SpeciesEye
	SpeciesTower
	SpeciesCount
)

// SpeciesNames indexes SpeciesType for telemetry keys and display.
var SpeciesNames = [SpeciesCount]string{
	SpeciesNone:   "none",
	SpeciesDrain:  "drain",
	SpeciesSwarm:  "swarm",
	SpeciesQuasar: "quasar",
	SpeciesStorm:  "storm",
	SpeciesPylon:  "pylon",
	SpeciesSnake:  "snake",
	SpeciesEye:    "eye",
	SpeciesTower:  "tower",
}
