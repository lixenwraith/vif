// @lixen: #dev{feature[shield(render,system)],feature[spirit(render,system)]}
package render

// RenderPriority determines render order. Lower values render first
type RenderPriority int

const (
	PriorityBackground  RenderPriority = 0
	PriorityGrid        RenderPriority = 100
	PrioritySplash      RenderPriority = 150
	PriorityEntities    RenderPriority = 200
	PriorityEffects     RenderPriority = 300
	PriorityMaterialize RenderPriority = 350
	PriorityDrain       RenderPriority = 400
	PriorityUI          RenderPriority = 450
	PriorityOverlay     RenderPriority = 500
	PriorityDebug       RenderPriority = 1000
)