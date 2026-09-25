package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
)

// BulletSystem manages linear projectile lifecycle. Bullets travel in a straight
// line and stop at walls and bounds; a hostile one strikes cursors and shields, a
// cursor's resolves against species. Spawned via EventBulletSpawnRequest.
type BulletSystem struct {
	world   *engine.World
	enabled bool

	statCount          *atomic.Int64
	statSpawned        *atomic.Int64
	statHits           *atomic.Int64
	statWallCollisions *atomic.Int64
	statBoundaryHits   *atomic.Int64
	statGridSteps      *atomic.Int64
	statDisabled       *atomic.Int64
}

func NewBulletSystem(world *engine.World) engine.System {
	s := &BulletSystem{world: world}
	reg := world.Resources.Status
	s.statCount = reg.Ints.Get("bullet.count")
	s.statSpawned = reg.Ints.Get("bullet.spawned")
	s.statHits = reg.Ints.Get("bullet.hits")
	s.statWallCollisions = reg.Ints.Get("bullet.wall_collisions")
	s.statBoundaryHits = reg.Ints.Get("bullet.boundary_hits")
	s.statGridSteps = reg.Ints.Get("bullet.grid_steps")
	s.statDisabled = reg.Ints.Get("bullet.disabled_rejects")

	s.Init()
	return s
}

func (s *BulletSystem) Init() {
	s.statCount.Store(0)
	s.statSpawned.Store(0)
	s.statHits.Store(0)
	s.statWallCollisions.Store(0)
	s.statBoundaryHits.Store(0)
	s.statGridSteps.Store(0)
	s.statDisabled.Store(0)
	s.enabled = true
}

func (s *BulletSystem) Name() string { return "bullet" }

func (s *BulletSystem) Priority() int { return parameter.PriorityBullet }

func (s *BulletSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventBulletSpawnRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *BulletSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
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
		if ev.Type == event.EventBulletSpawnRequest {
			s.statDisabled.Add(1)
		}
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
	s.statCount.Store(int64(bullets.CountEntities()))
	if bullets.CountEntities() == 0 {
		return
	}

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

		destroyed := s.traverseAndCollide(bullet, prevX, prevY, kinetic.PreciseX, kinetic.PreciseY)
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

// traverseAndCollide walks the bullet path to its first wall, boundary or target;
// returns true if the bullet should be destroyed
func (s *BulletSystem) traverseAndCollide(
	bullet *component.BulletComponent,
	fromX, fromY, toX, toY float64,
) bool {
	start := vmath.PointAtF(fromX, fromY)

	traverser := vmath.NewGridTraverserF(fromX, fromY, toX, toY)
	for traverser.Next() {
		s.statGridSteps.Add(1)
		cx, cy := traverser.Pos()

		// Skip origin cell
		if cx == start.X && cy == start.Y {
			continue
		}

		if s.world.Positions.IsOutOfBounds(cx, cy) {
			s.statBoundaryHits.Add(1)
			return true
		}

		if s.world.Positions.HasBlockingWallAt(cx, cy, component.WallBlockKinetic) {
			s.statWallCollisions.Add(1)
			return true
		}

		if bullet.Hostile {
			if cursor := CursorContactAt(s.world, cx, cy); cursor != 0 {
				strikeCursor(s.world, cursor, bullet.Damage)
				s.statHits.Add(1)
				return true
			}
		} else if target, hit, ok := CombatTargetAt(s.world, cx, cy, engine.ScopeBoth, 0, bullet.Owner); ok {
			// Resolved by the target, not by the firing cursor's domain (D-10)
			s.world.PushEventDomain(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
				AttackType:   bullet.Attack,
				OwnerEntity:  bullet.Owner,
				OriginEntity: bullet.Owner,
				TargetEntity: target,
				HitEntity:    hit,
				HasOrigin:    true,
				OriginX:      cx,
				OriginY:      cy,
			}, target.Domain())
			s.statHits.Add(1)
			return true
		}
	}

	return false
}

func (s *BulletSystem) spawnBullet(p *event.BulletSpawnRequestPayload) {
	e := s.world.CreateEntity(core.DomainPlayer)

	s.world.Components.Bullet.SetComponent(e, component.BulletComponent{
		Owner:       p.Owner,
		MaxLifetime: p.MaxLifetime,
		Hostile:     p.Hostile,
		Damage:      p.Damage,
		Attack:      p.Attack,
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
	s.statSpawned.Add(1)
}
