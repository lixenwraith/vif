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

	// GA-optimized cornering parameters (Q32.32)

	// TurnThreshold: alignment below which cornering drag activates (0.5–0.95)
	TurnThreshold int64
	// BrakeIntensity: drag multiplier during turns (1.0–6.0)
	BrakeIntensity int64
	// FlowLookahead: flow field projection distance (Q32.32 cells)
	FlowLookahead int64

	// GA-evolved path diversity parameters (Q32.32)

	// PathDeviation: probability of choosing non-optimal flow direction [0, Scale]
	PathDeviation int64
	// FlowBlend: blend factor toward direct-to-target when no LOS [0, Scale]
	FlowBlend int64
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