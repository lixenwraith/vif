package engine

import (
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
)

// ZIndexResolver provides fast z-index lookups using cached store pointers
// Initialize once during bootstrap, access via CoreResources.ZIndex
type ZIndexResolver struct {
	cursors *Store[components.CursorComponent]
	shields *Store[components.ShieldComponent]
	drains  *Store[components.DrainComponent]
	decays  *Store[components.DecayComponent]
	nuggets *Store[components.NuggetComponent]
	// Cached for IsInteractable
	chars *Store[components.CharacterComponent]
	seqs  *Store[components.SequenceComponent]
}

// NewZIndexResolver creates a resolver with cached store references
// Call after all components are registered
func NewZIndexResolver(w *World) *ZIndexResolver {
	z := &ZIndexResolver{
		cursors: GetStore[components.CursorComponent](w),
		shields: GetStore[components.ShieldComponent](w),
		drains:  GetStore[components.DrainComponent](w),
		decays:  GetStore[components.DecayComponent](w),
		nuggets: GetStore[components.NuggetComponent](w),
		chars:   GetStore[components.CharacterComponent](w),
		seqs:    GetStore[components.SequenceComponent](w),
	}

	// Wire to PositionStore for hot-path access
	w.Positions.SetZIndexResolver(z)

	return z
}

// GetZIndex returns the Z-index for an entity based on its components
func (z *ZIndexResolver) GetZIndex(e core.Entity) int {
	if z.cursors.Has(e) {
		return constants.ZIndexCursor
	}
	if z.shields.Has(e) {
		return constants.ZIndexShield
	}
	if z.drains.Has(e) {
		return constants.ZIndexDrain
	}
	if z.decays.Has(e) {
		return constants.ZIndexDecay
	}
	if z.nuggets.Has(e) {
		return constants.ZIndexNugget
	}
	return constants.ZIndexSpawnChar
}

// IsInteractable returns true if the entity is an interactable game element
func (z *ZIndexResolver) IsInteractable(e core.Entity) bool {
	if z.nuggets.Has(e) {
		return true
	}
	return z.chars.Has(e) && z.seqs.Has(e)
}

// SelectTopEntityFiltered returns the entity with highest z-index passing filter
func (z *ZIndexResolver) SelectTopEntityFiltered(entities []core.Entity, filter func(core.Entity) bool) core.Entity {
	if len(entities) == 0 {
		return 0
	}

	var top core.Entity
	maxZ := -1

	for _, e := range entities {
		if !filter(e) {
			continue
		}
		zIdx := z.GetZIndex(e)
		if zIdx > maxZ {
			maxZ = zIdx
			top = e
		}
	}
	return top
}