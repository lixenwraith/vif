package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/vmath"
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
	s.destroyAll()
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
	dtFixed := vmath.FromFloat(dt.Seconds())

	entities := s.world.Components.Bullet.GetAllEntities()
	if len(entities) == 0 {
		return
	}

	// Cache cursor and shield state for the frame
	cursorEntity := s.world.Resources.Player.Entity
	cursorPos, hasCursor := s.world.Positions.GetPosition(cursorEntity)

	shieldComp, shieldOK := s.world.Components.Shield.GetComponent(cursorEntity)
	shieldActive := shieldOK && shieldComp.Active

	var toDestroy []core.Entity

	for _, e := range entities {
		bullet, ok := s.world.Components.Bullet.GetComponent(e)
		if !ok {
			continue
		}
		kinetic, ok := s.world.Components.Kinetic.GetComponent(e)
		if !ok {
			continue
		}

		bullet.Lifetime += dt
		if bullet.Lifetime > bullet.MaxLifetime {
			toDestroy = append(toDestroy, e)
			continue
		}

		prevX, prevY := kinetic.PreciseX, kinetic.PreciseY
		kinetic.PreciseX += vmath.Mul(kinetic.VelX, dtFixed)
		kinetic.PreciseY += vmath.Mul(kinetic.VelY, dtFixed)

		destroyed := s.traverseAndCollide(
			&bullet, prevX, prevY, kinetic.PreciseX, kinetic.PreciseY,
			hasCursor, shieldActive, cursorPos, shieldComp,
		)
		if destroyed {
			toDestroy = append(toDestroy, e)
			continue
		}

		// Sync grid position
		gridX := vmath.ToInt(kinetic.PreciseX)
		gridY := vmath.ToInt(kinetic.PreciseY)
		if pos, ok := s.world.Positions.GetPosition(e); !ok || pos.X != gridX || pos.Y != gridY {
			s.world.Positions.SetPosition(e, component.PositionComponent{X: gridX, Y: gridY})
		}

		s.world.Components.Bullet.SetComponent(e, bullet)
		s.world.Components.Kinetic.SetComponent(e, kinetic)
	}

	for _, e := range toDestroy {
		s.destroyBullet(e)
	}
}

// traverseAndCollide walks the bullet path checking for wall, boundary, shield, and cursor collisions
// Returns true if bullet should be destroyed
func (s *BulletSystem) traverseAndCollide(
	bullet *component.BulletComponent,
	fromX, fromY, toX, toY int64,
	hasCursor, shieldActive bool,
	cursorPos component.PositionComponent,
	shieldComp component.ShieldComponent,
) bool {
	startGridX, startGridY := vmath.ToInt(fromX), vmath.ToInt(fromY)

	traverser := vmath.NewGridTraverser(fromX, fromY, toX, toY)
	for traverser.Next() {
		cx, cy := traverser.Pos()

		// Skip origin cell
		if cx == startGridX && cy == startGridY {
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
		if shieldActive && vmath.EllipseContainsPoint(
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
		Kinetic: core.Kinetic{
			PreciseX: p.OriginX,
			PreciseY: p.OriginY,
			VelX:     p.VelX,
			VelY:     p.VelY,
		},
	})

	s.world.Positions.SetPosition(e, component.PositionComponent{
		X: vmath.ToInt(p.OriginX),
		Y: vmath.ToInt(p.OriginY),
	})
}

func (s *BulletSystem) destroyBullet(e core.Entity) {
	s.world.Components.Bullet.RemoveEntity(e)
	s.world.Components.Kinetic.RemoveEntity(e)
	s.world.Positions.RemoveEntity(e)
	s.world.DestroyEntity(e)
}

func (s *BulletSystem) destroyAll() {
	for _, e := range s.world.Components.Bullet.GetAllEntities() {
		s.destroyBullet(e)
	}
}