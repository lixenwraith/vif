package render

// RenderPriority determines render order. Lower values render first
type RenderPriority int

const (
	PriorityBackground RenderPriority = 0
	PriorityGrid       RenderPriority = 100
	PrioritySplash     RenderPriority = 150
	PriorityEntities   RenderPriority = 200
	PriorityEffects    RenderPriority = 300
	PriorityDrain      RenderPriority = 350
	PriorityUI         RenderPriority = 400
	PriorityOverlay    RenderPriority = 500
	PriorityDebug      RenderPriority = 1000
)