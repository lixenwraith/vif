package render

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
	PrioritySigil
	PriorityGlyph
	PriorityGold
	PriorityNugget
	// A destruction flash is the glyph a species ploughed through; the species
	// draws over it
	PriorityFlash

	// === Species (back to front) ===

	// Background species, rendered first,
	// Foreground species with depth, rendered last
	PriorityPylon
	PriorityTower
	PriorityStorm
	PriorityKraken
	PriorityEye
	PrioritySnake
	PriorityDrain
	PriorityQuasar
	PrioritySwarm
	PriorityHealthBar // Body-centered bars must compose over species geometry.

	// === Tracer ===
	PriorityTracer

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
	PriorityRay
	PriorityBullet

	// === Particles ===
	PriorityFadeout
	PriorityExplosion
	PrioritySpirit

	// === Overlays ===
	PriorityButton // Opaque, so every map layer under it passes behind
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
	// Peers before the local cursor, so an overlap resolves in favour of the one
	// the player is steering.
	PriorityPeerCursor
	PriorityCursor

	// === Debug/Overlay ===
	PriorityFlowField
	PriorityPinnedState
	PriorityOverlay
	PriorityDebug
)

// ClipsToPlayfield confines simulation layers and the cursors to the map, including
// when it is smaller than the viewport. Post-processing, heat, indicators, the status
// bar and debug overlays address the whole screen.
func (p RenderPriority) ClipsToPlayfield() bool {
	switch p {
	case PriorityGrayout, PriorityStrobe, PriorityDim,
		PriorityHeat, PriorityIndicator, PriorityStatusBar,
		PriorityFlowField, PriorityPinnedState, PriorityOverlay, PriorityDebug:
		return false
	default:
		return true
	}
}
