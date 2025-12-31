package engine
// @lixen: #dev{feature[drain(render,system)],feature[quasar(render,system)]}

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
)

// TODO: missing blossom, quasar, ... review
// ZIndexResolver provides fast z-index lookups using cached store pointers
// Initialize once during bootstrap, access via Resources.ZIndex
type ZIndexResolver struct {
	cursors *Store[component.CursorComponent]
	shields *Store[component.ShieldComponent]
	drains  *Store[component.DrainComponent]
	decays  *Store[component.DecayComponent]
	nuggets *Store[component.NuggetComponent]
	glyphs  *Store[component.GlyphComponent]
}

// NewZIndexResolver creates a resolver with cached store references
// Call after all components are registered
func NewZIndexResolver(w *World) *ZIndexResolver {
	z := &ZIndexResolver{
		cursors: GetStore[component.CursorComponent](w),
		shields: GetStore[component.ShieldComponent](w),
		drains:  GetStore[component.DrainComponent](w),
		decays:  GetStore[component.DecayComponent](w),
		nuggets: GetStore[component.NuggetComponent](w),
		glyphs:  GetStore[component.GlyphComponent](w),
	}

	// Wire to PositionStore for hot-path access
	w.Positions.SetZIndexResolver(z)

	return z
}

// GetZIndex returns the Z-index for an entity based on its components
func (z *ZIndexResolver) GetZIndex(e core.Entity) int {
	if z.cursors.Has(e) {
		return constant.ZIndexCursor
	}
	if z.shields.Has(e) {
		return constant.ZIndexShield
	}
	if z.drains.Has(e) {
		return constant.ZIndexDrain
	}
	if z.decays.Has(e) {
		return constant.ZIndexDecay
	}
	if z.nuggets.Has(e) {
		return constant.ZIndexNugget
	}
	return constant.ZIndexGlyph
}

// IsTypeable returns true if the entity is a typeable (glyph) game element
func (z *ZIndexResolver) IsTypeable(e core.Entity) bool {
	return z.glyphs.Has(e)
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