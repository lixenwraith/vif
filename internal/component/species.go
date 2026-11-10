package component

import (
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
)

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

// SubType is a species' own kind, which its events carry beside it: an eye's
// EyeType, a tower's TowerType; zero for a species without kinds
type SubType uint8

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

// SpeciesDomains are the domain each code species' bodies live in: a drain is its
// participant's own, every other species Shared, as every data species is
var SpeciesDomains = [SpeciesCount]core.Domain{
	SpeciesNone:   core.DomainShared,
	SpeciesDrain:  core.DomainPlayer,
	SpeciesSwarm:  core.DomainShared,
	SpeciesQuasar: core.DomainShared,
	SpeciesStorm:  core.DomainShared,
	SpeciesPylon:  core.DomainShared,
	SpeciesSnake:  core.DomainShared,
	SpeciesEye:    core.DomainShared,
	SpeciesTower:  core.DomainShared,
	SpeciesKraken: core.DomainShared,
}

// KillEffects are the visual each code species' kill leaves; an expiry fades the
// body out, and a cancel leaves nothing. A storm's is each circle's, whose scatter
// bursts its shell as a pylon's and a tower's do, and a snake's its head's, the
// body fading behind it. A data species names its own in its prototype.
var KillEffects = [SpeciesCount]DeathEffect{
	SpeciesNone:   DeathSilent,
	SpeciesDrain:  DeathFlash,
	SpeciesSwarm:  DeathScatter,
	SpeciesQuasar: DeathScatter,
	SpeciesStorm:  DeathScatter,
	SpeciesPylon:  DeathScatter,
	SpeciesSnake:  DeathScatter,
	SpeciesEye:    DeathFlash,
	SpeciesTower:  DeathScatter,
	SpeciesKraken: DeathSilent, // its renderer burns it away
}

// Footprints are each code species' body around its header, from its parameters: a
// pylon's and a tower's at their default radii, which a request may change, a
// snake's its head, a storm's one circle and a kraken's its body without its legs.
// A data species declares its own in its prototype.
var Footprints = [SpeciesCount]vmath.Footprint{
	SpeciesNone:  {},
	SpeciesDrain: {Width: 1, Height: 1},
	SpeciesSwarm: {Width: parameter.SwarmWidth, Height: parameter.SwarmHeight,
		HeaderX: parameter.SwarmHeaderOffsetX, HeaderY: parameter.SwarmHeaderOffsetY},
	SpeciesQuasar: {Width: parameter.QuasarWidth, Height: parameter.QuasarHeight,
		HeaderX: parameter.QuasarHeaderOffsetX, HeaderY: parameter.QuasarHeaderOffsetY},
	SpeciesStorm: {Width: 2*parameter.StormCircleRadiusX + 1, Height: 2*parameter.StormCircleRadiusY + 1,
		HeaderX: parameter.StormCircleRadiusX, HeaderY: parameter.StormCircleRadiusY},
	SpeciesPylon: {Width: 2*parameter.PylonDefaultRadiusX + 1, Height: 2*parameter.PylonDefaultRadiusY + 1,
		HeaderX: parameter.PylonDefaultRadiusX, HeaderY: parameter.PylonDefaultRadiusY},
	SpeciesSnake: {Width: parameter.SnakeHeadWidth, Height: parameter.SnakeHeadHeight,
		HeaderX: parameter.SnakeHeadHeaderOffsetX, HeaderY: parameter.SnakeHeadHeaderOffsetY},
	SpeciesEye: {Width: parameter.EyeWidth, Height: parameter.EyeHeight,
		HeaderX: parameter.EyeHeaderOffsetX, HeaderY: parameter.EyeHeaderOffsetY},
	SpeciesTower: {Width: 2*parameter.TowerDefaultRadiusX + 1, Height: 2*parameter.TowerDefaultRadiusY + 1,
		HeaderX: parameter.TowerDefaultRadiusX, HeaderY: parameter.TowerDefaultRadiusY},
	SpeciesKraken: {Width: 4*parameter.KrakenBodyRadius + 1, Height: 2*parameter.KrakenBodyRadius + 1,
		HeaderX: 2 * parameter.KrakenBodyRadius, HeaderY: parameter.KrakenBodyRadius},
}
