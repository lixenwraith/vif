package system

import (
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// blastArea bounds a set of explosion centers for player-domain cell tests.
// The union box rejects most of the map before any per-center distance work.
type blastArea struct {
	centers                []event.ExplosionCenterEntry
	one                    [1]event.ExplosionCenterEntry
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

// resetOne rebuilds the area for a single center without allocating
func (a *blastArea) resetOne(x, y int, radius float64) {
	a.one[0] = event.ExplosionCenterEntry{X: x, Y: y}
	a.reset(a.one[:], radius)
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

// strikePlayerTargets applies a blast to player-domain combat entities. A local
// producer calls it before pushing its explosion request, so the shared consumer
// never observes a player entity.
// The drain store is today's whole player-domain combat set. A second player-domain
// combatant replaces this with a domain filter over Components.Combat; nothing else
// on the path has to change.
func strikePlayerTargets(w *engine.World, owner core.Entity, area *blastArea, attack component.CombatAttackType) {
	cursor := w.ResolveCursor(owner)
	if cursor == 0 {
		return
	}
	for _, target := range w.Components.Drain.Entities() {
		pos, ok := w.Positions.GetPosition(target)
		if !ok {
			continue
		}
		cx, cy, hit := area.find(pos.X, pos.Y)
		if !hit {
			continue
		}
		w.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
			AttackType:   attack,
			OwnerEntity:  cursor,
			OriginEntity: cursor,
			TargetEntity: target,
			HasOrigin:    true,
			OriginX:      cx,
			OriginY:      cy,
		})
	}
}
