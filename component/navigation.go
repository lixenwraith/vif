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

	// Band routing genes (Q32.32, written by GeneticSystem)

	// BudgetMultiplier: max acceptable distance as ratio of optimal
	// Scale (1.0) = optimal only, 2.0*Scale = accepts paths up to 2× optimal length
	BudgetMultiplier int64

	// ExplorationBias: among valid band neighbors, preference toward divergent vs progressive paths
	// 0 = prefer progress toward target, Scale (1.0) = prefer maximum divergence within budget
	ExplorationBias int64

	// FlowLookahead: aspect-weighted distance threshold below which band routing disables
	// Entity converges via optimal flow within this distance of target (Q32.32)
	FlowLookahead int64
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
}