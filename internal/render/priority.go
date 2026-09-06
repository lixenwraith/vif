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
	PrioritySigil
	PriorityGlyph
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

// ClipsToPlayfield reports whether a layer draws simulation content and so must
// be confined to the cells the map covers.
//
// A map smaller than the viewport is centred inside it, and the margin that
// leaves belongs to no cell any entity, effect, or field can occupy. A layer that
// draws there is drawing outside the world, which is what a ping line, a cleaner
// trail, or a materialize beam reaching the terminal edge on a zoomed pane is.
// Answering it here rather than in each renderer is what makes the bound hold for
// effects nobody has written yet.
//
// Post-processing, UI, and debug layers address the whole screen by design: the
// status bar, the gutters, the overlay panels, and the dim/grayout passes all
// live outside the map and stay unclipped.
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
