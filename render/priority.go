package render

// RenderPriority determines render order. Lower values render first
type RenderPriority int

const (
	PriorityBackground  RenderPriority = 0
	PriorityGrid        RenderPriority = 100
	PriorityEntities    RenderPriority = 200
	PriorityCleaner     RenderPriority = 300
	PriorityField       RenderPriority = 400
	PriorityMaterialize RenderPriority = 500
	PriorityParticle    RenderPriority = 600
	PrioritySplash      RenderPriority = 700
	PriorityMulti       RenderPriority = 800
	PriorityPostProcess RenderPriority = 900
	PriorityUI          RenderPriority = 2000
	PriorityOverlay     RenderPriority = 2500
	PriorityDebug       RenderPriority = 9000
)