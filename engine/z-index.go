// @lixen: #focus{arch[spatial,zindex],render[layer]}
// @lixen: #interact{state[zindex]}
package engine

import (
	"github.com/lixenwraith/vi-fighter/core"
)

// Z-Index constants determine priority for spatial queries and rendering
// Higher values are "on top"
const (
	ZIndexBackground = 0
	ZIndexSpawnChar  = 100
	ZIndexNugget     = 200
	ZIndexDecay      = 300
	ZIndexDrain      = 400
	ZIndexShield     = 500
	ZIndexCursor     = 1000
)

// GetZIndex returns the Z-index for an entity based on its components
// It checks stores in the World to determine the entity type
func GetZIndex(world *World, e core.Entity) int {
	// Check highest priority first for early exit
	if world.Cursors.Has(e) {
		return ZIndexCursor
	}
	if world.Shields.Has(e) {
		return ZIndexShield
	}
	if world.Drains.Has(e) {
		return ZIndexDrain
	}
	if world.Decays.Has(e) {
		return ZIndexDecay
	}
	if world.Nuggets.Has(e) {
		return ZIndexNugget
	}
	// Default to lowest priority (standard characters/spawned entities)
	return ZIndexSpawnChar
}

// SelectTopEntityFiltered returns the entity with highest z-index that passes the filter
// Returns 0 if no entities pass the filter or slice is empty
// Filter receives entity and returns true if entity should be considered
func SelectTopEntityFiltered(entities []core.Entity, world *World, filter func(core.Entity) bool) core.Entity {
	if len(entities) == 0 {
		return 0
	}

	var top core.Entity
	maxZ := -1

	for _, e := range entities {
		if !filter(e) {
			continue
		}
		z := GetZIndex(world, e)
		if z > maxZ {
			maxZ = z
			top = e
		}
	}
	return top
}

// IsInteractable returns true if the entity is an interactable game element
// Interactable entities: Characters (with SequenceComponent), Nuggets
// Non-interactable: Cursor, Drain, Decay, Shield, Flash
func IsInteractable(world *World, e core.Entity) bool {
	if world.Nuggets.Has(e) {
		return true
	}
	return world.Characters.Has(e) && world.Sequences.Has(e)
}