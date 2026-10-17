package system

import (
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// blastArea bounds a set of explosion centers for player-domain cell tests.
// The union box rejects most of the map before any per-center distance work.
type blastArea struct {
	centers                []event.ExplosionCenterEntry
	one                    [1]event.ExplosionCenterEntry
	aspect                 vmath.Aspect
	radiusX, radiusY       int
	radiusSq               float64
	minX, maxX, minY, maxY int
}

// reset rebuilds the union bounding box for a center list at a cell aspect
func (a *blastArea) reset(centers []event.ExplosionCenterEntry, radius float64, aspect vmath.Aspect) {
	a.centers = centers
	a.aspect = aspect
	a.radiusX = int(radius)
	a.radiusY = int(aspect.FromCircular(radius))
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
func (a *blastArea) resetOne(x, y int, radius float64, aspect vmath.Aspect) {
	a.one[0] = event.ExplosionCenterEntry{X: x, Y: y}
	a.reset(a.one[:], radius, aspect)
}

// find returns the nearest center covering a cell
func (a *blastArea) find(x, y int) (cx, cy int, ok bool) {
	if x < a.minX || x > a.maxX || y < a.minY || y > a.maxY {
		return 0, 0, false
	}
	best := -1.0
	for i := range a.centers {
		dx := float64(x - a.centers[i].X)
		dyCirc := a.aspect.Circular(float64(y - a.centers[i].Y))
		distSq := vmath.CircleDistSq(dx, dyCirc)
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

// Player targets resolve before geometry crosses; the shared resolver never reads them.
func strikePlayerTargets(w *engine.World, owner core.Entity, area *blastArea, attack component.CombatAttackType, charged bool) {
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
		w.PushLocal(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
			AttackType:   attack,
			OwnerEntity:  cursor,
			OriginEntity: cursor,
			TargetEntity: target,
			HasOrigin:    true,
			OriginX:      cx,
			OriginY:      cy,
			Charged:      charged,
		})
	}
}

// contains reports whether a cell is inside any center's blast
func (a *blastArea) contains(x, y int) bool {
	_, _, ok := a.find(x, y)
	return ok
}
