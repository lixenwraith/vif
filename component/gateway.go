package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/core"
)

// GatewayComponent manages timed species spawning anchored to a parent entity
// Gateway does not perform spawning directly — emits spawn request events for species systems
type GatewayComponent struct {
	// Anchor entity (e.g. pylon header). Gateway despawns if anchor dies
	AnchorEntity core.Entity

	// Species routing
	Species SpeciesType // Which species to spawn
	SubType uint8       // Species-specific variant (e.g. EyeType)
	GroupID uint8       // Target group ID assigned to spawned entities

	// Spawn timing
	BaseInterval time.Duration // Base time between spawns
	Accumulated  time.Duration // Time accumulated toward next spawn
	Active       bool          // False pauses accumulation without despawn

	// Rate acceleration
	RateMultiplier    float64       // Multiplier applied to interval (1.0 = no change)
	RateAccelInterval time.Duration // How often multiplier is applied (0 = disabled)
	RateAccelElapsed  time.Duration // Time accumulated toward next acceleration step
	MinInterval       time.Duration // Floor for interval after acceleration

	// Spawn position
	OffsetX int // Offset from anchor position (0 = spawn at anchor)
	OffsetY int

	// --- Future: Route Distribution (placeholder, not connected) ---
	// RouteDistID will reference a per-gateway route weight table
	// populated by NavigationSystem when alternative routes are computed
	// GeneticSystem will read this to assign route-selection genes at spawn
	RouteDistID uint32

	// --- Future: Spawn Pool (placeholder, not connected) ---
	// SpawnPoolSize will track batched spawn allocation for route assignment
	// Allows pre-sampling N route genes then distributing across spawns
	SpawnPoolSize int
}