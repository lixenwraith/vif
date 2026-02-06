package render

// TODO: move to parameter, need code gen change
// RenderPriority determines render order. Lower values render first
type RenderPriority int

const (
	PriorityBackground  RenderPriority = 0
	PriorityGrid        RenderPriority = 100
	PriorityChargeLine  RenderPriority = 150
	PriorityEntities    RenderPriority = 200
	PriorityCleaner     RenderPriority = 300
	PriorityField       RenderPriority = 400
	PriorityMaterialize RenderPriority = 500
	PriorityParticle    RenderPriority = 600
	PrioritySplash      RenderPriority = 700
	PriorityMulti       RenderPriority = 800
	PriorityMarker      RenderPriority = 900
	PriorityWall        RenderPriority = 1400
	PriorityPostProcess RenderPriority = 1500
	PriorityUI          RenderPriority = 2000
	PriorityOverlay     RenderPriority = 2500
	PriorityDebug       RenderPriority = 9000
)