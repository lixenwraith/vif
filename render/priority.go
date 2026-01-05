package render

// RenderPriority determines render order. Lower values render first
type RenderPriority int

const (
	PriorityBackground  RenderPriority = 0
	PriorityGrid        RenderPriority = 100
	PrioritySplash      RenderPriority = 200
	PriorityEntities    RenderPriority = 300
	PriorityCleaner     RenderPriority = 400
	PriorityField       RenderPriority = 500
	PriorityMaterialize RenderPriority = 600
	PriorityParticle    RenderPriority = 700
	PriorityMulti       RenderPriority = 800
	PriorityPostProcess RenderPriority = 900
	PriorityUI          RenderPriority = 1000
	PriorityOverlay     RenderPriority = 1100
	PriorityDebug       RenderPriority = 2000
)