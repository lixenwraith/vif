package component

import (
	"github.com/lixenwraith/vi-fighter/parameter"
)

// NavigationComponent provides pathfinding state for kinetic entities
type NavigationComponent struct {
	// True when straight-line path to target is walkable
	HasDirectPath bool

	// Flow direction (Q32.32 normalized), valid when HasDirectPath is false
	FlowX int64
	FlowY int64

	// Entity dimensions for area-based LOS (set at spawn)
	Width  int
	Height int

	// FlowLookahead: aspect-weighted distance threshold below which flow routing yields to direct homing
	// Entity converges via optimal flow within this distance of target (Q32.32)
	FlowLookahead int64

	// UseRouteGraph enables per-route flow field navigation instead of shared optimal field
	// Default false: entity uses shared group flow field (backward compatible)
	UseRouteGraph bool

	// RouteGraphID selects which pre-computed route graph to use (0 = none)
	// Set by spawning system when route graph is available for entity's gateway-target pair
	RouteGraphID uint32

	// RouteID selects which route's flow field to follow within the graph (-1 = unassigned, use shared field)
	RouteID int
}

// SpeciesDimensions holds bounding box for collision detection
type SpeciesDimensions struct {
	Width, Height int
}

// speciesDimensionsLUT indexed by SpeciesType for O(1) lookup
var SpeciesDimensionsLUT = [SpeciesCount]SpeciesDimensions{
	{1, 1}, // 0: SpeciesNone (unused)
	{1, 1}, // 1: SpeciesDrain
	{parameter.SwarmWidth, parameter.SwarmHeight},                                            // 2: SpeciesSwarm
	{parameter.QuasarWidth, parameter.QuasarHeight},                                          // 3: SpeciesQuasar
	{int(parameter.StormCircleRadiusXFloat * 2), int(parameter.StormCircleRadiusYFloat * 2)}, // 4: SpeciesStorm
	{1, 1}, // 5: SpeciesPylon
	{parameter.SnakeHeadWidth, parameter.SnakeHeadHeight}, // 6: SpeciesSnake
	{parameter.EyeWidth, parameter.EyeHeight},             // 7: SpeciesEye
	{1, 1}, // 8: SpeciesTower (stationary, dimensions from spawn params)
}