package render

// TODO: move to parameter, need code gen change
// RenderPriority determines render order. Lower values render first
type RenderPriority int

const (
	PriorityBackground RenderPriority = iota
	PriorityGrid
	PriorityWall
	PriorityChargeLine
	PriorityGlyph
	PriorityParticle
	PriorityEntities
	PrioritySpecies
	PriorityHealthBar
	PriorityCleaner
	PriorityMaterialize
	PriorityField
	PrioritySplash
	PriorityMarker
	PriorityPostProcess
	PriorityUI
	PriorityOverlay
	PriorityDebug
)