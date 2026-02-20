package engine

import (
	"github.com/lixenwraith/vi-fighter/core"
)

// SpeciesCacheEntry holds cached position for soft collision checks
type SpeciesCacheEntry struct {
	Entity core.Entity
	X, Y   int
}

// SpeciesCache provides centralized per-tick position cache for all combat species
// Lazy refresh: first access per tick rebuilds, subsequent accesses reuse
type SpeciesCache struct {
	Drains  []SpeciesCacheEntry
	Swarms  []SpeciesCacheEntry
	Quasars []SpeciesCacheEntry
	// Storms  []SpeciesCacheEntry // Storm circle headers (not root)
	Pylons []SpeciesCacheEntry

	lastTick uint64
	world    *World
}

// NewSpeciesCache creates cache with pre-allocated slices
func NewSpeciesCache(world *World) *SpeciesCache {
	return &SpeciesCache{
		Drains:  make([]SpeciesCacheEntry, 0, 16),
		Swarms:  make([]SpeciesCacheEntry, 0, 8),
		Quasars: make([]SpeciesCacheEntry, 0, 4),
		// Storms:   make([]SpeciesCacheEntry, 0, 4),
		Pylons:   make([]SpeciesCacheEntry, 0, 4),
		lastTick: ^uint64(0), // Max value to force first Refresh() rebuild, edge miss of first tick
		world:    world,
	}
}

// Refresh rebuilds cache if tick has advanced
// Safe to call multiple times per tick - only first call does work
func (c *SpeciesCache) Refresh() {
	currentTick := c.world.Resources.Game.State.GetGameTicks()
	if c.lastTick == currentTick {
		return
	}
	c.lastTick = currentTick
	c.rebuild()
}

// rebuild populates all species caches from component stores
func (c *SpeciesCache) rebuild() {
	c.Drains = c.Drains[:0]
	c.Swarms = c.Swarms[:0]
	c.Quasars = c.Quasars[:0]
	// c.Storms = c.Storms[:0]
	c.Pylons = c.Pylons[:0]

	// Drains
	for _, entity := range c.world.Components.Drain.GetAllEntities() {
		if pos, ok := c.world.Positions.GetPosition(entity); ok {
			c.Drains = append(c.Drains, SpeciesCacheEntry{Entity: entity, X: pos.X, Y: pos.Y})
		}
	}

	// Swarms (header positions)
	for _, entity := range c.world.Components.Swarm.GetAllEntities() {
		if pos, ok := c.world.Positions.GetPosition(entity); ok {
			c.Swarms = append(c.Swarms, SpeciesCacheEntry{Entity: entity, X: pos.X, Y: pos.Y})
		}
	}

	// Quasars (header positions)
	for _, entity := range c.world.Components.Quasar.GetAllEntities() {
		if pos, ok := c.world.Positions.GetPosition(entity); ok {
			c.Quasars = append(c.Quasars, SpeciesCacheEntry{Entity: entity, X: pos.X, Y: pos.Y})
		}
	}

	// TODO: commented to not waste cycles building unused cache, future potential use
	// // Storms (circle header positions, not root)
	// for _, rootEntity := range c.world.Components.Storm.GetAllEntities() {
	// 	stormComp, ok := c.world.Components.Storm.GetComponent(rootEntity)
	// 	if !ok {
	// 		continue
	// 	}
	// 	for i := 0; i < component.StormCircleCount; i++ {
	// 		if !stormComp.CirclesAlive[i] {
	// 			continue
	// 		}
	// 		if pos, ok := c.world.Positions.GetPosition(stormComp.Circles[i]); ok {
	// 			c.Storms = append(c.Storms, SpeciesCacheEntry{Entity: stormComp.Circles[i], X: pos.X, Y: pos.Y})
	// 		}
	// 	}
	// }

	// Pylons (header positions)
	for _, entity := range c.world.Components.Pylon.GetAllEntities() {
		pylonComp, ok := c.world.Components.Pylon.GetComponent(entity)
		if !ok {
			continue
		}
		// Pylon uses spawn position (stationary)
		c.Pylons = append(c.Pylons, SpeciesCacheEntry{Entity: entity, X: pylonComp.SpawnX, Y: pylonComp.SpawnY})
	}
}

// Reset clears cache state (call on game reset)
func (c *SpeciesCache) Reset() {
	c.Drains = c.Drains[:0]
	c.Swarms = c.Swarms[:0]
	c.Quasars = c.Quasars[:0]
	// c.Storms = c.Storms[:0]
	c.Pylons = c.Pylons[:0]
	c.lastTick = ^uint64(0)
}