// @lixen: #dev{base(render),feature[lightning(render)],feature[shield(render,system)],feature[spirit(render,system)]}
package render

// RenderPriority determines render order. Lower values render first
type RenderPriority int

const (
	PriorityBackground  RenderPriority = 0
	PriorityGrid        RenderPriority = 100
	PrioritySplash      RenderPriority = 150
	PriorityEntities    RenderPriority = 200
	PriorityCleaner     RenderPriority = 300
	PriorityField       RenderPriority = 310
	PriorityMaterialize RenderPriority = 350
	PriorityMulti       RenderPriority = 400
	PriorityParticle    RenderPriority = 420
	PriorityPostProcess RenderPriority = 440
	PriorityUI          RenderPriority = 450
	PriorityOverlay     RenderPriority = 500
	PriorityDebug       RenderPriority = 1000
)