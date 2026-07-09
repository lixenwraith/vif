package system

import (
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/physics"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// MissileSystem manages missile lifecycle
type MissileSystem struct {
	world   *engine.World
	enabled bool
}

func NewMissileSystem(world *engine.World) engine.System {
	s := &MissileSystem{world: world}
	s.Init()
	return s
}

func (s *MissileSystem) Init() {
	s.destroyAll()
	s.enabled = true
}

func (s *MissileSystem) Name() string { return "missile" }

func (s *MissileSystem) Priority() int { return parameter.PriorityMissile }

func (s *MissileSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventMissileSpawnRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *MissileSystem) HandleEvent(ev event.GameEvent) {
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

	dt := s.world.Resources.Time.DeltaTime()
	dtFixed := vmath.FromFloat(dt.Seconds())

	missileEntities := s.world.Components.Missile.GetAllEntities()

	var toDestroy []core.Entity

	for _, missileEntity := range missileEntities {
		missileComp, ok := s.world.Components.Missile.GetComponent(missileEntity)
		if !ok {
			continue
		}
		kineticComp, ok := s.world.Components.Kinetic.GetComponent(missileEntity)
		if !ok {
			continue
		}

		missileComp.Lifetime += dt

		if s.updateMissile(&missileComp, &kineticComp, dtFixed) {
			s.world.PushEvent(event.EventExplosionRequest, &event.ExplosionRequestPayload{
				X:      vmath.ToInt(kineticComp.PreciseX),
				Y:      vmath.ToInt(kineticComp.PreciseY),
				Radius: parameter.MissileExplosionRadius,
				Type:   event.ExplosionTypeMissile,
			})
			toDestroy = append(toDestroy, missileEntity)
			continue
		}

		gridX := vmath.ToInt(kineticComp.PreciseX)
		gridY := vmath.ToInt(kineticComp.PreciseY)

		// OOB check only (wall collision handled in traversal)
		if s.world.Positions.IsOutOfBounds(gridX, gridY) {
			toDestroy = append(toDestroy, missileEntity)
			continue
		}

		// Update spatial grid position
		if missilePos, ok := s.world.Positions.GetPosition(missileEntity); !ok || missilePos.X != gridX || missilePos.Y != gridY {
			s.world.Positions.SetPosition(missileEntity, component.PositionComponent{X: gridX, Y: gridY})
		}

		// Trail emission based on elapsed time
		if missileComp.Lifetime-missileComp.LastTrailEmit >= parameter.MissileTrailInterval {
			s.pushTrail(&missileComp, kineticComp.PreciseX, kineticComp.PreciseY)
			missileComp.LastTrailEmit = missileComp.Lifetime
		}
		s.ageTrail(&missileComp, dt)

		s.world.Components.Missile.SetComponent(missileEntity, missileComp)
		s.world.Components.Kinetic.SetComponent(missileEntity, kineticComp)
	}

	for _, e := range toDestroy {
		s.world.DestroyEntity(e)
	}
}

func (s *MissileSystem) updateMissile(m *component.MissileComponent, k *component.KineticComponent, dt int64) (impacted bool) {
	// Lifetime timeout for orphaned missiles
	if m.Lifetime > parameter.MissileMaxLifetime {
		return true
	}

	prevX, prevY := k.PreciseX, k.PreciseY

	// Resolve target for homing
	targetX, targetY, hasTarget := s.resolveTarget(m, k.PreciseX, k.PreciseY)

	if !hasTarget {
		// Ballistic drift if target is lost
		k.PreciseX += vmath.Mul(k.VelX, dt)
		k.PreciseY += vmath.Mul(k.VelY, dt)
	} else {
		// Impact check before homing (specific target proximity)
		dx := targetX - k.PreciseX
		dy := targetY - k.PreciseY
		if vmath.MagnitudeSq(dx, dy) < parameter.MissileImpactRadiusSq {
			k.PreciseX = targetX
			k.PreciseY = targetY
			return true
		}

		// Homing via physics
		physics.ApplyHoming(&k.Kinetic, targetX, targetY, &physics.MissileHoming, dt)
		k.VelX, k.VelY = physics.CapSpeed(k.VelX, k.VelY, parameter.MissileMaxSpeed)

		// Integrate position
		k.PreciseX += vmath.Mul(k.VelX, dt)
		k.PreciseY += vmath.Mul(k.VelY, dt)
	}

	// General Enemy Collision: missile detonates on any combatant contact
	impactX, impactY, hitType := s.traverseForImpact(prevX, prevY, k.PreciseX, k.PreciseY)
	if hitType == impactWall {
		k.PreciseX, k.PreciseY = vmath.CenteredFromGrid(impactX, impactY)
		return true
	}
	if hitType == impactEnemy {
		k.PreciseX, k.PreciseY = vmath.CenteredFromGrid(impactX, impactY)
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
func (s *MissileSystem) traverseForImpact(fromX, fromY, toX, toY int64) (x, y int, hit impactType) {
	fromGridX, fromGridY := vmath.ToInt(fromX), vmath.ToInt(fromY)
	toGridX, toGridY := vmath.ToInt(toX), vmath.ToInt(toY)

	// No movement or same cell
	if fromGridX == toGridX && fromGridY == toGridY {
		return 0, 0, impactNone
	}

	cursorEntity := s.world.Resources.Player.Entity
	traverser := vmath.NewGridTraverser(fromX, fromY, toX, toY)
	lastSafeX, lastSafeY := fromGridX, fromGridY

	for traverser.Next() {
		currX, currY := traverser.Pos()

		// Skip starting cell
		if currX == fromGridX && currY == fromGridY {
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
func (s *MissileSystem) resolveTarget(m *component.MissileComponent, missileX, missileY int64) (int64, int64, bool) {
	// 1. Sticky hit entity
	if m.HitEntity != 0 {
		if pos, ok := s.world.Positions.GetPosition(m.HitEntity); ok {
			x, y := vmath.CenteredFromGrid(pos.X, pos.Y)
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
			x, y := vmath.CenteredFromGrid(pos.X, pos.Y)
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
		x, y := vmath.CenteredFromGrid(pos.X, pos.Y)
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
	sumX, sumY, validCount := int64(0), int64(0), 0
	for _, t := range p.Targets {
		if pos, ok := s.world.Positions.GetPosition(t); ok {
			sumX += vmath.FromInt(pos.X) + vmath.CellCenter
			sumY += vmath.FromInt(pos.Y) + vmath.CellCenter
			validCount++
		}
	}

	originX := vmath.FromInt(p.OriginX) + vmath.CellCenter
	originY := vmath.FromInt(p.OriginY) + vmath.CellCenter

	baseDirX, baseDirY := int64(0), -vmath.Scale // Default UP
	if validCount > 0 {
		centroidX := sumX / int64(validCount)
		centroidY := sumY / int64(validCount)
		dirX, dirY := vmath.Normalize2D(centroidX-originX, centroidY-originY)
		if dirX != 0 || dirY != 0 {
			baseDirX, baseDirY = dirX, dirY
		}
	}

	// Calculate spread arc
	spread := parameter.MissileSpreadAngle
	step := int64(0)
	if p.Count > 1 {
		step = spread / int64(p.Count-1)
	}
	startAngle := -spread / 2

	for i := 0; i < p.Count; i++ {
		angle := startAngle + step*int64(i)
		dirX, dirY := vmath.RotateVector(baseDirX, baseDirY, angle)

		// Stagger initial speed slightly for visual spread
		speedFactor := vmath.Scale - vmath.FromFloat(parameter.MissileStaggerFactor*float64(i))
		speed := vmath.Mul(parameter.MissileMaxSpeed, speedFactor)

		vx := vmath.Mul(dirX, speed)
		vy := vmath.Mul(dirY, speed)

		var target, hit core.Entity
		if len(p.Targets) > 0 {
			target = p.Targets[i%len(p.Targets)]
			hit = p.HitEntities[i%len(p.HitEntities)]
		}

		s.spawnMissile(p.OwnerEntity, p.OriginEntity, originX, originY, vx, vy, target, hit)
	}
}

func (s *MissileSystem) spawnMissile(owner, origin core.Entity, x, y, vx, vy int64, target, hit core.Entity) {
	e := s.world.CreateEntity()

	s.world.Components.Missile.SetComponent(e, component.MissileComponent{
		Owner:        owner,
		Origin:       origin,
		TargetEntity: target,
		HitEntity:    hit,
	})

	s.world.Components.Kinetic.SetComponent(e, component.KineticComponent{
		Kinetic: core.Kinetic{
			PreciseX: x,
			PreciseY: y,
			VelX:     vx,
			VelY:     vy,
		},
	})

	s.world.Positions.SetPosition(e, component.PositionComponent{X: vmath.ToInt(x), Y: vmath.ToInt(y)})
}

// --- Helpers ---

func (s *MissileSystem) pushTrail(m *component.MissileComponent, x, y int64) {
	m.Trail[m.TrailHead] = component.MissileTrailPoint{X: x, Y: y, Age: 0}
	m.TrailHead = (m.TrailHead + 1) % component.TrailCapacity
	if m.TrailLen < component.TrailCapacity {
		m.TrailLen++
	}
}

func (s *MissileSystem) ageTrail(m *component.MissileComponent, dt time.Duration) {
	for i := 0; i < m.TrailLen; i++ {
		idx := (m.TrailHead - m.TrailLen + i + component.TrailCapacity) % component.TrailCapacity
		m.Trail[idx].Age += dt
	}
}

func (s *MissileSystem) destroyAll() {
	for _, e := range s.world.Components.Missile.GetAllEntities() {
		s.world.DestroyEntity(e)
	}
}
