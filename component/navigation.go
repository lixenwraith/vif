package component

import (
	"github.com/lixenwraith/vi-fighter/parameter"
)

// NavigationComponent provides pathfinding state for kinetic entities
type NavigationComponent struct {
	// True when straight-line path to cursor is walkable
	HasDirectPath bool

	// Flow direction from BFS (Q32.32 normalized), valid when HasDirectPath is false
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
	// FlowLookahead is flow field projection distance (Q32.32 cells)
	FlowLookahead int64

	// Oscillation suppression: grid positions from recent ticks
	TabuPos  [2][2]int // [index][x,y], ring buffer of last 2 grid positions
	TabuHead uint8     // Next write index (0 or 1)
	TabuTick uint8     // Entries written (0-2), prevents false matches before buffer fills
}

// SpeciesDimensions holds bounding box for collision detection
type SpeciesDimensions struct {
	Width, Height int
}

// speciesDimensionsLUT indexed by SpeciesType for O(1) lookup
// Index 0 unused (SpeciesType starts at 1)
var SpeciesDimensionsLUT = [SpeciesCount]SpeciesDimensions{
	{1, 1}, // 0: unused
	{1, 1}, // 1: SpeciesDrain
	{parameter.SwarmWidth, parameter.SwarmHeight},   // 2: SpeciesSwarm
	{parameter.QuasarWidth, parameter.QuasarHeight}, // 3: SpeciesQuasar
	{1, 1}, // 4: SpeciesStorm
}