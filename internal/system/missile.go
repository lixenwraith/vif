package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/profile"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// MissileSystem manages missile lifecycle
type MissileSystem struct {
	world *engine.World

	statCount   *atomic.Int64
	statSpawned *atomic.Int64
	statImpacts *atomic.Int64
	statExpired *atomic.Int64

	enabled bool
}

func NewMissileSystem(world *engine.World) engine.System {
	s := &MissileSystem{world: world}

	s.statCount = world.Resources.Status.Ints.Get("missile.count")
	s.statSpawned = world.Resources.Status.Ints.Get("missile.spawned")
	s.statImpacts = world.Resources.Status.Ints.Get("missile.impacts")
	s.statExpired = world.Resources.Status.Ints.Get("missile.expired")

	s.Init()
	return s
}

func (s *MissileSystem) Init() {
	s.destroyAll()
	s.statCount.Store(0)
	s.statSpawned.Store(0)
	s.statImpacts.Store(0)
	s.statExpired.Store(0)
	s.enabled = true
}

func (s *MissileSystem) Name() string { return "missile" }

func (s *MissileSystem) Priority() int { return parameter.PriorityMissile }

func (s *MissileSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventMissileSpawnRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *MissileSystem) HandleEvent(ev event.GameEvent) {
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
		return
	}
	if ev.Type == event.EventMissileSpawnRequest {
		if p, ok := ev.Payload.(*event.MissileSpawnRequestPayload); ok {
			s.handleSpawnRequest(p)
		}
	}
}

func (s *MissileSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	dtSec := dt.Seconds()

	missiles := s.world.Components.Missile
	s.statCount.Store(int64(missiles.CountEntities()))
	if missiles.CountEntities() == 0 {
		return
	}

	var toDestroy []core.Entity

	// Live view: no removals during the loop (deferred to toDestroy),
	// no missile spawns during Update (spawn is event-driven)
	for _, missileEntity := range missiles.Entities() {
		missileComp, ok := missiles.GetPtr(missileEntity)
		if !ok {
			continue
		}
		kineticComp, ok := s.world.Components.Kinetic.GetPtr(missileEntity)
		if !ok {
			continue
		}

		missileComp.Lifetime += dt

		if s.updateMissile(missileComp, kineticComp, dtSec) {
			x, y := physics.GridPos(&kineticComp.Kinetic)
			s.world.PushEvent(event.EventExplosionRequest, &event.ExplosionRequestPayload{
				X:      x,
				Y:      y,
				Radius: parameter.MissileExplosionRadius,
				Type:   event.ExplosionTypeMissile,
			})
			toDestroy = append(toDestroy, missileEntity)
			s.statImpacts.Add(1)
			continue
		}

		gridX, gridY := physics.GridPos(&kineticComp.Kinetic)

		// OOB check only (wall collision handled in traversal)
		if s.world.Positions.IsOutOfBounds(gridX, gridY) {
			toDestroy = append(toDestroy, missileEntity)
			s.statExpired.Add(1)
			continue
		}

		// Update spatial grid position
		if missilePos, ok := s.world.Positions.GetPosition(missileEntity); !ok || missilePos.X != gridX || missilePos.Y != gridY {
			s.world.Positions.SetPosition(missileEntity, component.PositionComponent{X: gridX, Y: gridY})
		}

		// Trail emission based on elapsed time
		if missileComp.Lifetime-missileComp.LastTrailEmit >= parameter.MissileTrailInterval {
			s.pushTrail(missileComp, kineticComp.PreciseX, kineticComp.PreciseY)
			missileComp.LastTrailEmit = missileComp.Lifetime
		}
		s.ageTrail(missileComp, dt)
	}

	s.world.DestroyEntitiesBatch(toDestroy)
}

func (s *MissileSystem) updateMissile(m *component.MissileComponent, k *component.KineticComponent, dt float64) (impacted bool) {
	// Lifetime timeout for orphaned missiles
	if m.Lifetime > parameter.MissileMaxLifetime {
		return true
	}

	prevX, prevY := k.PreciseX, k.PreciseY

	// Resolve target for homing
	targetX, targetY, hasTarget := s.resolveTarget(m, k.PreciseX, k.PreciseY)

	if !hasTarget {
		// Ballistic drift if target is lost
		physics.IntegratePosition(&k.Kinetic, dt)
	} else {
		// Impact check before homing (specific target proximity)
		dx := targetX - k.PreciseX
		dy := targetY - k.PreciseY
		if vmath.MagnitudeSqF(dx, dy) < parameter.MissileImpactRadiusSq {
			k.PreciseX = targetX
			k.PreciseY = targetY
			return true
		}

		// Homing via physics
		physics.ApplyHoming(&k.Kinetic, targetX, targetY, &profile.MissileHoming, dt)
		k.VelX, k.VelY = physics.CapSpeed(k.VelX, k.VelY, parameter.MissileMaxSpeed)

		// Integrate position
		physics.IntegratePosition(&k.Kinetic, dt)
	}

	// General Enemy Collision: missile detonates on any combatant contact
	impactX, impactY, hitType := s.traverseForImpact(prevX, prevY, k.PreciseX, k.PreciseY)
	if hitType != impactNone {
		k.PreciseX, k.PreciseY = vmath.Point{X: impactX, Y: impactY}.CenterF()
		return true
	}

	return false
}

type impactType uint8

const (
	impactNone impactType = iota
	impactWall
	impactEnemy
)

// traverseForImpact walks path checking for wall/enemy collisions
func (s *MissileSystem) traverseForImpact(fromX, fromY, toX, toY float64) (x, y int, hit impactType) {
	from := vmath.PointAtF(fromX, fromY)
	to := vmath.PointAtF(toX, toY)

	// No movement or same cell
	if from == to {
		return 0, 0, impactNone
	}

	cursorEntity := s.world.Resources.Player.Entity
	traverser := vmath.NewGridTraverserF(fromX, fromY, toX, toY)
	lastSafeX, lastSafeY := from.X, from.Y

	for traverser.Next() {
		currX, currY := traverser.Pos()

		// Skip starting cell
		if currX == from.X && currY == from.Y {
			continue
		}

		// Wall collision
		if s.world.Positions.HasBlockingWallAt(currX, currY, component.WallBlockKinetic) {
			return lastSafeX, lastSafeY, impactWall
		}

		// Enemy collision
		if HasCombatTargetAt(s.world, currX, currY, 0, cursorEntity) {
			return currX, currY, impactEnemy
		}

		lastSafeX, lastSafeY = currX, currY
	}

	return 0, 0, impactNone
}

// resolveTarget updates target/hit entity state and returns homing coordinates
func (s *MissileSystem) resolveTarget(m *component.MissileComponent, missileX, missileY float64) (float64, float64, bool) {
	// 1. Sticky hit entity
	if m.HitEntity != 0 {
		if pos, ok := s.world.Positions.GetPosition(m.HitEntity); ok {
			x, y := vmath.Point{X: pos.X, Y: pos.Y}.CenterF()
			return x, y, true
		}
		m.HitEntity = 0
	}

	// 2. Parent target — resolve new closest member
	if m.TargetEntity != 0 {
		if s.world.Components.Header.HasEntity(m.TargetEntity) {
			member, x, y, ok := ResolveClosestMember(s.world, m.TargetEntity, missileX, missileY)
			if ok {
				m.HitEntity = member
				return x, y, true
			}
			m.TargetEntity = 0
		} else if pos, ok := s.world.Positions.GetPosition(m.TargetEntity); ok {
			x, y := vmath.Point{X: pos.X, Y: pos.Y}.CenterF()
			m.HitEntity = m.TargetEntity
			return x, y, true
		} else {
			m.TargetEntity = 0
		}
	}

	// 3. Retarget: nearest enemy
	cursorEntity := s.world.Resources.Player.Entity
	targets := FindNearestTargets(s.world, missileX, missileY, 1, cursorEntity)
	if len(targets) == 0 {
		return 0, 0, false
	}

	nearest := targets[0]
	m.TargetEntity = nearest.Target
	m.HitEntity = nearest.Hit

	if pos, ok := s.world.Positions.GetPosition(nearest.Hit); ok {
		x, y := vmath.Point{X: pos.X, Y: pos.Y}.CenterF()
		return x, y, true
	}

	return 0, 0, false
}

// --- Spawning ---

func (s *MissileSystem) handleSpawnRequest(p *event.MissileSpawnRequestPayload) {
	if p.Count <= 0 {
		return
	}

	// Calculate centroid of targets to aim the center of the spread arc
	sumX, sumY, validCount := 0.0, 0.0, 0
	for _, t := range p.Targets {
		if pos, ok := s.world.Positions.GetPosition(t); ok {
			cx, cy := vmath.Point{X: pos.X, Y: pos.Y}.CenterF()
			sumX += cx
			sumY += cy
			validCount++
		}
	}

	originX, originY := vmath.Point{X: p.OriginX, Y: p.OriginY}.CenterF()

	baseDirX, baseDirY := 0.0, -1.0 // Default UP
	if validCount > 0 {
		centroidX := sumX / float64(validCount)
		centroidY := sumY / float64(validCount)
		dirX, dirY := vmath.Normalize2DF(centroidX-originX, centroidY-originY)
		if dirX != 0 || dirY != 0 {
			baseDirX, baseDirY = dirX, dirY
		}
	}

	// Preserve the fixed-path contract: spread is measured in full turns.
	spread := parameter.MissileSpreadTurns
	step := 0.0
	if p.Count > 1 {
		step = spread / float64(p.Count-1)
	}
	startAngle := -spread / 2

	for i := range p.Count {
		angle := startAngle + step*float64(i)
		dirX, dirY := vmath.RotateVectorF(baseDirX, baseDirY, angle*vmath.TwoPi)

		// Stagger initial speed slightly for visual spread
		speedFactor := 1.0 - parameter.MissileStaggerFactor*float64(i)
		speed := parameter.MissileMaxSpeed * speedFactor

		vx := dirX * speed
		vy := dirY * speed

		var target, hit core.Entity
		if len(p.Targets) > 0 {
			target = p.Targets[i%len(p.Targets)]
			hit = p.HitEntities[i%len(p.HitEntities)]
		}

		s.spawnMissile(p.OwnerEntity, p.OriginEntity, originX, originY, vx, vy, target, hit)
	}
}

func (s *MissileSystem) spawnMissile(owner, origin core.Entity, x, y, vx, vy float64, target, hit core.Entity) {
	e := s.world.CreateEntity()

	s.world.Components.Missile.SetComponent(e, component.MissileComponent{
		Owner:        owner,
		Origin:       origin,
		TargetEntity: target,
		HitEntity:    hit,
	})

	s.world.Components.Kinetic.SetComponent(e, component.KineticComponent{
		Kinetic: physics.Kinetic{
			PreciseX: x,
			PreciseY: y,
			VelX:     vx,
			VelY:     vy,
		},
	})

	cell := vmath.PointAtF(x, y)
	s.world.Positions.SetPosition(e, component.PositionComponent{X: cell.X, Y: cell.Y})
	s.statSpawned.Add(1)
}

// --- Helpers ---

func (s *MissileSystem) pushTrail(m *component.MissileComponent, x, y float64) {
	m.Trail[m.TrailHead] = component.MissileTrailPoint{X: x, Y: y, Age: 0}
	m.TrailHead = (m.TrailHead + 1) % component.TrailCapacity
	if m.TrailLen < component.TrailCapacity {
		m.TrailLen++
	}
}

func (s *MissileSystem) ageTrail(m *component.MissileComponent, dt time.Duration) {
	for i := range m.TrailLen {
		idx := (m.TrailHead - m.TrailLen + i + component.TrailCapacity) % component.TrailCapacity
		m.Trail[idx].Age += dt
	}
}

func (s *MissileSystem) destroyAll() {
	// Batch destruction mutates the live missile slice, so detach it first.
	entities := s.world.Components.Missile.GetAllEntities()
	s.world.DestroyEntitiesBatch(entities)
}
