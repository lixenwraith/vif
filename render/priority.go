package render

// TODO: move to parameter, need code gen change
// RenderPriority determines render order. Lower values render first
type RenderPriority int

const (
	PriorityBackground RenderPriority = iota
	PriorityGrid
	PriorityChargeLine
	PriorityEntities
	PriorityCleaner
	PriorityField
	PriorityMaterialize
	PriorityParticle
	PrioritySplash
	PriorityMulti
	PriorityMarker
	PriorityWall
	PriorityPostProcess
	PriorityUI
	PriorityOverlay
	PriorityDebug
)