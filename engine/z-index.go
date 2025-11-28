package engine

// Z-Index constants determine priority for spatial queries and rendering
// Higher values are "on top"
const (
	ZIndexSpawnChar = 0
	ZIndexNugget    = 100
	ZIndexDecay     = 200
	ZIndexShield    = 300 // TODO: integrate
	ZIndexDrain     = 400
	ZIndexCleaner   = 500
	ZIndexCursor    = 1000
)

// GetZIndex returns the Z-index for an entity based on its components
// It checks stores in the World to determine the entity type
func GetZIndex(world *World, e Entity) int {
	// Check highest priority first to fail fast
	if world.Cursors.Has(e) {
		return ZIndexCursor
	}
	if world.Cleaners.Has(e) {
		return ZIndexCleaner
	}
	if world.Drains.Has(e) {
		return ZIndexDrain
	}
	// Shield check would go here (future)
	if world.Decays.Has(e) {
		return ZIndexDecay
	}
	if world.Nuggets.Has(e) {
		return ZIndexNugget
	}
	// Default to lowest priority (standard characters/spawned entities)
	return ZIndexSpawnChar
}

// SelectTopEntity returns the entity with the highest Z-index from a slice
// Returns 0 if the slice is empty
func SelectTopEntity(entities []Entity, world *World) Entity {
	if len(entities) == 0 {
		return 0
	}
	if len(entities) == 1 {
		return entities[0]
	}

	var top Entity
	maxZ := -1

	for _, e := range entities {
		z := GetZIndex(world, e)
		if z > maxZ {
			maxZ = z
			top = e
		}
	}
	return top
}