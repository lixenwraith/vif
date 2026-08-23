package system

import (
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// blastArea bounds a set of explosion centers for player-domain cell tests.
// The union box rejects most of the map before any per-center distance work.
type blastArea struct {
	centers                []event.ExplosionCenterEntry
	radiusX, radiusY       int
	radiusSq               float64
	minX, maxX, minY, maxY int
}

// reset rebuilds the union bounding box for a center list
func (a *blastArea) reset(centers []event.ExplosionCenterEntry, radius float64) {
	a.centers = centers
	a.radiusX = int(radius)
	a.radiusY = a.radiusX / 2
	a.radiusSq = radius * radius
	a.minX, a.minY = 1<<30, 1<<30
	a.maxX, a.maxY = -(1 << 30), -(1 << 30)
	for i := range centers {
		a.minX = min(a.minX, centers[i].X-a.radiusX)
		a.maxX = max(a.maxX, centers[i].X+a.radiusX)
		a.minY = min(a.minY, centers[i].Y-a.radiusY)
		a.maxY = max(a.maxY, centers[i].Y+a.radiusY)
	}
}

// find returns the nearest center covering a cell, matching the consumer's aspect correction
func (a *blastArea) find(x, y int) (cx, cy int, ok bool) {
	if x < a.minX || x > a.maxX || y < a.minY || y > a.maxY {
		return 0, 0, false
	}
	best := -1.0
	for i := range a.centers {
		dx := float64(x - a.centers[i].X)
		dyCirc := vmath.ScaleToCircularF(float64(y - a.centers[i].Y))
		distSq := vmath.CircleDistSqF(dx, dyCirc)
		if distSq > a.radiusSq {
			continue
		}
		if best < 0 || distSq < best {
			best = distSq
			cx, cy = a.centers[i].X, a.centers[i].Y
		}
	}
	return cx, cy, best >= 0
}

// contains reports whether a cell falls inside any center
func (a *blastArea) contains(x, y int) bool {
	_, _, ok := a.find(x, y)
	return ok
}

// detonateDust converts every dust particle into an explosion center, resolves the
// player-domain half of the blast, then hands the geometry to ExplosionSystem.
func (s *DustSystem) detonateDust(cursor core.Entity) {
	dusts := s.world.Components.Dust.Entities()
	if len(dusts) == 0 {
		return
	}

	cursorPos, ok := s.world.Positions.GetPosition(cursor)
	if !ok {
		return
	}

	// One center per occupied cell, collected in dense store order so merge order is stable
	clear(s.centerCells)
	s.centerBuf = s.centerBuf[:0]
	s.destroyBuf = append(s.destroyBuf[:0], dusts...)
	for _, e := range s.destroyBuf {
		pos, ok := s.world.Positions.GetPosition(e)
		if !ok {
			continue
		}
		key := posKey(pos.X, pos.Y)
		if _, seen := s.centerCells[key]; seen {
			continue
		}
		s.centerCells[key] = struct{}{}
		if len(s.centerBuf) == parameter.ExplosionCenterCap {
			s.statCentersDropped.Add(1)
			continue
		}
		s.centerBuf = append(s.centerBuf, event.ExplosionCenterEntry{X: pos.X, Y: pos.Y})
	}
	s.buffers.Observe(bufDustCenters, len(s.centerBuf))

	// DustSystem owns dust lifecycle, so destruction needs no death-system round trip
	destroyed := len(s.destroyBuf)
	s.world.DestroyEntitiesBatch(s.destroyBuf)
	s.destroyBuf = s.destroyBuf[:0]
	s.statDestroyed.Add(int64(destroyed))

	if len(s.centerBuf) == 0 {
		return
	}
	s.blast.reset(s.centerBuf, parameter.ExplosionFieldRadius)

	s.convertGlyphs(cursorPos.X, cursorPos.Y, &s.blast)
	s.strikeDrains(cursor)

	// The crossing: geometry and combat family only, no player entity reference
	p := event.AcquireExplosionBatchRequest()
	p.Entity = cursor
	p.Radius = parameter.ExplosionFieldRadius
	p.Duration = parameter.ExplosionFieldDuration
	p.Type = event.ExplosionTypeDust
	p.Attack = component.CombatAttackExplosion
	p.Centers = append(p.Centers, s.centerBuf...)
	s.world.PushEvent(event.EventExplosionBatchRequest, p)
}

// strikeDrains applies the blast to player-domain drains, one event per drain per
// detonation: overlapping centers add only hits the immunity window absorbs.
func (s *DustSystem) strikeDrains(cursor core.Entity) {
	for _, drain := range s.world.Components.Drain.Entities() {
		pos, ok := s.world.Positions.GetPosition(drain)
		if !ok {
			continue
		}
		cx, cy, hit := s.blast.find(pos.X, pos.Y)
		if !hit {
			continue
		}
		s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
			AttackType:   component.CombatAttackExplosion,
			OwnerEntity:  cursor,
			OriginEntity: cursor,
			TargetEntity: drain,
			HasOrigin:    true,
			OriginX:      cx,
			OriginY:      cy,
		})
	}
}

// convertGlyphs turns loose glyphs into dust and flashes the dark ones.
// A nil area converts the whole field; composite members are never converted.
func (s *DustSystem) convertGlyphs(cursorX, cursorY int, area *blastArea) {
	glyphEntities := s.world.Components.Glyph.Entities()
	if len(glyphEntities) == 0 {
		return
	}

	s.transformBuf = s.transformBuf[:0]
	s.flashBuf = s.flashBuf[:0]
	s.destroyBuf = s.destroyBuf[:0]

	for _, glyphEntity := range glyphEntities {
		if s.world.Components.Member.HasEntity(glyphEntity) {
			continue
		}
		glyphComp, ok := s.world.Components.Glyph.GetPtr(glyphEntity)
		if !ok {
			continue
		}

		// Dark glyphs carry no dust, so they die through the death API for the flash
		if glyphComp.Level == component.GlyphDark {
			if area != nil {
				pos, ok := s.world.Positions.GetPosition(glyphEntity)
				if !ok || !area.contains(pos.X, pos.Y) {
					continue
				}
			}
			s.flashBuf = append(s.flashBuf, glyphEntity)
			continue
		}

		glyphPos, ok := s.world.Positions.GetPosition(glyphEntity)
		if !ok {
			continue
		}
		if area != nil && !area.contains(glyphPos.X, glyphPos.Y) {
			continue
		}
		s.destroyBuf = append(s.destroyBuf, glyphEntity)
		s.transformBuf = append(s.transformBuf, glyphTransform{
			x: glyphPos.X, y: glyphPos.Y, char: glyphComp.Rune, level: glyphComp.Level,
		})
	}
	s.buffers.Observe(bufDustTransform, len(s.transformBuf))
	s.buffers.Observe(bufDustFlash, len(s.flashBuf))

	if len(s.flashBuf) > 0 {
		event.EmitDeath(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, s.flashBuf...)
	}
	if len(s.transformBuf) == 0 {
		return
	}

	s.world.DestroyEntitiesBatch(s.destroyBuf)
	s.destroyBuf = s.destroyBuf[:0]

	posBatch := s.world.Positions.BeginBatch()
	for _, gt := range s.transformBuf {
		entity := s.world.CreateEntity()
		s.setDustComponents(entity, gt.x, gt.y, gt.char, gt.level, cursorX, cursorY)
		posBatch.Add(entity, component.PositionComponent{X: gt.x, Y: gt.y})
	}
	posBatch.CommitForce()

	s.statCreated.Add(int64(len(s.transformBuf)))
}
