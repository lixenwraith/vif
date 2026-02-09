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
	PriorityHealthBar
	PriorityEntities
	PriorityCleaner
	PriorityField
	PriorityMaterialize
	PriorityParticle
	PrioritySplash
	PriorityMulti
	PriorityMarker
	PriorityPostProcess
	PriorityUI
	PriorityOverlay
	PriorityDebug
)