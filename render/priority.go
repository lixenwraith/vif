package render

// TODO: move to parameter, need code gen change
// RenderPriority determines render order. Lower values render first
type RenderPriority int

const (
	// === Background Layer ===
	PriorityBackground RenderPriority = iota
	PriorityGrid
	PriorityPing

	// === Environment ===
	PriorityWall
	PriorityChargeLine

	// === Base Entities ===
	PriorityGlyph
	PrioritySigil
	PriorityGold
	PriorityNugget
	PriorityHealthBar

	// === Species (back to front) ===

	// Background species, rendered first,
	// Foreground species with depth, rendered last
	PriorityPylon
	PriorityTower
	PriorityStorm
	PriorityEye
	PrioritySnake
	PriorityDrain
	PriorityQuasar
	PrioritySwarm

	// === Cleaner ===
	PriorityCleaner

	// === Materialize Effects ===
	PriorityMaterialize
	PriorityTeleportLine

	// === Field Effects ===
	PriorityShield
	PriorityEmber
	PriorityOrb
	PriorityLightning
	PriorityMissile
	PriorityPulse
	PriorityBullet

	// === Particles ===
	PriorityFlash
	PriorityFadeout
	PriorityExplosion
	PrioritySpirit

	// === Overlays ===
	PrioritySplash
	PriorityMarker

	// === Post-Processing (order matters) ===
	PriorityGrayout
	PriorityStrobe
	PriorityDim

	// === UI Layer ===
	PriorityHeat
	PriorityIndicator
	PriorityStatusBar
	PriorityCursor

	// === Debug/Overlay ===
	PriorityOverlay
	PriorityFlowField
	PriorityDebug
)