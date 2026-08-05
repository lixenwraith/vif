package system

import (
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// BulletSystem manages linear projectile lifecycle
// Bullets travel in a straight line, collide with walls/boundaries/cursor/shield
// Spawned via EventBulletSpawnRequest from any system
type BulletSystem struct {
	world   *engine.World
	enabled bool
}

func NewBulletSystem(world *engine.World) engine.System {
	s := &BulletSystem{world: world}

	s.Init()
	return s
}

func (s *BulletSystem) Init() {
	s.enabled = true
}

func (s *BulletSystem) Name() string { return "bullet" }

// Priority: define parameter.PriorityBullet, schedule after storm and before render
func (s *BulletSystem) Priority() int { return 0 }

func (s *BulletSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventBulletSpawnRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *BulletSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
		return
	}

	if !s.enabled {
		return
	}

	if ev.Type == event.EventBulletSpawnRequest {
		if p, ok := ev.Payload.(*event.BulletSpawnRequestPayload); ok {
			s.spawnBullet(p)
		}
	}
}

func (s *BulletSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	dtSec := dt.Seconds()

	bullets := s.world.Components.Bullet
	if bullets.CountEntities() == 0 {
		return
	}

	// Cache cursor and shield state for the frame
	cursorEntity := s.world.Resources.Player.Entity
	cursorPos, hasCursor := s.world.Positions.GetPosition(cursorEntity)

	shieldComp, shieldOK := s.world.Components.Shield.GetComponent(cursorEntity)
	shieldActive := shieldOK && shieldComp.Active

	var toDestroy []core.Entity

	// Collision events are queued, and destruction is deferred until the live
	// view has been exhausted, so neither component pointer can be invalidated.
	for _, e := range bullets.Entities() {
		bullet, ok := bullets.GetPtr(e)
		if !ok {
			continue
		}
		kinetic, ok := s.world.Components.Kinetic.GetPtr(e)
		if !ok {
			continue
		}

		bullet.Lifetime += dt
		if bullet.Lifetime > bullet.MaxLifetime {
			toDestroy = append(toDestroy, e)
			continue
		}

		prevX, prevY := kinetic.PreciseX, kinetic.PreciseY
		gridX, gridY := physics.IntegratePosition(&kinetic.Kinetic, dtSec)

		destroyed := s.traverseAndCollide(
			bullet, prevX, prevY, kinetic.PreciseX, kinetic.PreciseY,
			hasCursor, shieldActive, cursorPos, shieldComp,
		)
		if destroyed {
			toDestroy = append(toDestroy, e)
			continue
		}

		// Sync grid position
		if pos, ok := s.world.Positions.GetPosition(e); !ok || pos.X != gridX || pos.Y != gridY {
			s.world.Positions.SetPosition(e, component.PositionComponent{X: gridX, Y: gridY})
		}

	}

	s.world.DestroyEntitiesBatch(toDestroy)
}

// traverseAndCollide walks the bullet path checking for wall, boundary, shield, and cursor collisions
// Returns true if bullet should be destroyed
func (s *BulletSystem) traverseAndCollide(
	bullet *component.BulletComponent,
	fromX, fromY, toX, toY float64,
	hasCursor, shieldActive bool,
	cursorPos component.PositionComponent,
	shieldComp component.ShieldComponent,
) bool {
	start := vmath.PointAtF(fromX, fromY)

	traverser := vmath.NewGridTraverserF(fromX, fromY, toX, toY)
	for traverser.Next() {
		cx, cy := traverser.Pos()

		// Skip origin cell
		if cx == start.X && cy == start.Y {
			continue
		}

		if s.world.Positions.IsOutOfBounds(cx, cy) {
			return true
		}

		if s.world.Positions.HasBlockingWallAt(cx, cy, component.WallBlockKinetic) {
			return true
		}

		if !hasCursor {
			continue
		}

		// Shield containment (checked before direct hit; shield area encloses cursor)
		if shieldActive && vmath.EllipseContainsPointF(
			cx, cy, cursorPos.X, cursorPos.Y,
			shieldComp.InvRxSq, shieldComp.InvRySq,
		) {
			s.world.PushEvent(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{
				Value: bullet.Damage.EnergyDrain,
			})
			return true
		}

		// Direct cursor hit without shield
		if !shieldActive && cx == cursorPos.X && cy == cursorPos.Y {
			s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
				Delta: bullet.Damage.HeatDelta,
			})
			return true
		}
	}

	return false
}

func (s *BulletSystem) spawnBullet(p *event.BulletSpawnRequestPayload) {
	e := s.world.CreateEntity()

	s.world.Components.Bullet.SetComponent(e, component.BulletComponent{
		Owner:       p.Owner,
		MaxLifetime: p.MaxLifetime,
		Damage:      p.Damage,
	})

	s.world.Components.Kinetic.SetComponent(e, component.KineticComponent{
		Kinetic: physics.Kinetic{
			PreciseX: p.OriginX,
			PreciseY: p.OriginY,
			VelX:     p.VelX,
			VelY:     p.VelY,
		},
	})

	origin := vmath.PointAtF(p.OriginX, p.OriginY)
	s.world.Positions.SetPosition(e, component.PositionComponent{X: origin.X, Y: origin.Y})
}
