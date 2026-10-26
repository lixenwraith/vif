package component

import "github.com/lixenwraith/vif/internal/parameter"

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
	SpeciesKraken
	SpeciesCount
)

// The data species: programs a scenario declares take SpeciesDataFirst onward in
// sorted-name order, so every table indexed by SpeciesType holds SpeciesMax rows
const (
	SpeciesDataFirst = SpeciesCount
	SpeciesMax       = SpeciesCount + parameter.BehaviorProgramsMax
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
	SpeciesKraken: "kraken",
}
