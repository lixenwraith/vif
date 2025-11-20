package engine

// DrainCollisionHandler defines the interface for systems that need to handle
// collisions with the drain entity. Systems implement this interface to define
// custom behavior when their entities collide with the drain.
//
// The drain system will call HandleDrainCollision for entities at the drain's
// position during its collision detection phase.
type DrainCollisionHandler interface {
	// HandleDrainCollision is called when a drain entity collides with another entity.
	// Parameters:
	//   - world: The ECS world for entity/component access
	//   - entity: The entity that collided with the drain
	//
	// Returns:
	//   - bool: true if the entity should be destroyed, false otherwise
	//
	// The implementing system is responsible for:
	//   1. Determining if the entity should be destroyed
	//   2. Updating any related state (e.g., clearing active nugget, triggering phase transitions)
	//   3. Returning true if the drain system should destroy the entity
	HandleDrainCollision(world *World, entity Entity) bool
}
