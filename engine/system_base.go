package engine

// SystemBase provides common dependency for all system
// Embed in system struct to eliminate boilerplate
type SystemBase struct {
	World     *World
	Resource  Resource
	Component ComponentStore
}

// NewSystemBase initializes base dependency from world
// Call once in system constructor
func NewSystemBase(w *World) SystemBase {
	return SystemBase{
		World:     w,
		Resource:  GetResourceStore(w),
		Component: GetComponentStore(w),
	}
}