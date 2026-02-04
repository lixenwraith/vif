package system

import (
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// MissileSystem manages cluster missile lifecycle
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
			s.spawnClusterParent(p)
		}
	}
}

func (s *MissileSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	dtFixed := vmath.FromFloat(dt.Seconds())

	missileEntities := s.world.Components.Missile.GetAllEntities()

	var toDestroy []core.Entity
	type splitRequest struct {
		m component.MissileComponent
		k component.KineticComponent
	}
	var pendingSplits []splitRequest

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

		switch missileComp.Type {
		case component.MissileTypeClusterParent:
			shouldSplit, earlyImpact := s.updateParent(&missileComp, &kineticComp, dtFixed)
			if earlyImpact {
				// Parent hit wall or enemy before split
				s.world.PushEvent(event.EventExplosionRequest, &event.ExplosionRequestPayload{
					X:      vmath.ToInt(kineticComp.PreciseX),
					Y:      vmath.ToInt(kineticComp.PreciseY),
					Radius: parameter.MissileExplosionRadius,
					Type:   event.ExplosionTypeMissile,
				})
				toDestroy = append(toDestroy, missileEntity)
				continue
			}
			if shouldSplit {
				pendingSplits = append(pendingSplits, splitRequest{missileComp, kineticComp})
				toDestroy = append(toDestroy, missileEntity)
				continue
			}

		case component.MissileTypeClusterChild:
			if s.updateSeeker(&missileComp, &kineticComp, dtFixed) {
				s.world.PushEvent(event.EventExplosionRequest, &event.ExplosionRequestPayload{
					X:      vmath.ToInt(kineticComp.PreciseX),
					Y:      vmath.ToInt(kineticComp.PreciseY),
					Radius: parameter.MissileExplosionRadius,
					Type:   event.ExplosionTypeMissile,
				})
				toDestroy = append(toDestroy, missileEntity)
				continue
			}
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

	for _, req := range pendingSplits {
		s.performSplit(&req.m, &req.k)
	}

	for _, e := range toDestroy {
		s.destroyMissile(e)
	}
}

func (s *MissileSystem) updateParent(m *component.MissileComponent, k *component.KineticComponent, dt int64) (split bool, earlyImpact bool) {
	prevX, prevY := k.PreciseX, k.PreciseY

	// Linear movement toward destination
	k.PreciseX += vmath.Mul(k.VelX, dt)
	k.PreciseY += vmath.Mul(k.VelY, dt)

	// Path traversal for wall and enemy collision
	impactX, impactY, hitType := s.traverseForImpact(prevX, prevY, k.PreciseX, k.PreciseY, true)
	if hitType != impactNone {
		k.PreciseX = vmath.FromInt(impactX) + vmath.CellCenter
		k.PreciseY = vmath.FromInt(impactY) + vmath.CellCenter
		return false, true
	}

	// Split distance check (unchanged logic)
	destX, destY := k.AccelX, k.AccelY
	distSq := vmath.MagnitudeSq(destX-k.PreciseX, destY-k.PreciseY)
	splitDistSq := vmath.Mul(parameter.MissileClusterMinDistance, parameter.MissileClusterMinDistance)

	return distSq < splitDistSq || m.Lifetime > parameter.MissileParentMaxLifetime, false
}

func (s *MissileSystem) updateSeeker(m *component.MissileComponent, k *component.KineticComponent, dt int64) (impacted bool) {
	// Lifetime timeout for orphaned seekers
	if m.Lifetime > parameter.MissileSeekerMaxLifetime {
		return true
	}

	prevX, prevY := k.PreciseX, k.PreciseY

	// Resolve target
	targetX, targetY, hasTarget := s.resolveTarget(m, k.PreciseX, k.PreciseY)

	if !hasTarget {
		// Ballistic drift
		k.PreciseX += vmath.Mul(k.VelX, dt)
		k.PreciseY += vmath.Mul(k.VelY, dt)
	} else {
		// Vector to target
		dx := targetX - k.PreciseX
		dy := targetY - k.PreciseY
		distSq := vmath.MagnitudeSq(dx, dy)

		// Impact check
		if distSq < parameter.MissileImpactRadiusSq {
			k.PreciseX = targetX
			k.PreciseY = targetY
			return true
		}

		// Homing: steer toward target
		dist := vmath.Sqrt(distSq)
		if dist > 0 {
			dirX := vmath.Div(dx, dist)
			dirY := vmath.Div(dy, dist)

			accel := parameter.MissileSeekerHomingAccel
			k.VelX += vmath.Mul(vmath.Mul(dirX, accel), dt)
			k.VelY += vmath.Mul(vmath.Mul(dirY, accel), dt)
		}

		// Drag
		drag := parameter.MissileSeekerDrag
		if dist < parameter.MissileSeekerArrivalRadius && parameter.MissileSeekerArrivalRadius > 0 {
			proximityFactor := vmath.Scale - vmath.Div(dist, parameter.MissileSeekerArrivalRadius)
			drag += vmath.Mul(drag*2, proximityFactor)
		}

		speed := vmath.Magnitude(k.VelX, k.VelY)
		if speed > 0 {
			dragScale := vmath.Mul(drag, dt)
			if dragScale > vmath.Scale {
				dragScale = vmath.Scale
			}
			retain := vmath.Scale - dragScale
			k.VelX = vmath.Mul(k.VelX, retain)
			k.VelY = vmath.Mul(k.VelY, retain)
		}

		// Speed cap
		speed = vmath.Magnitude(k.VelX, k.VelY)
		if speed > parameter.MissileSeekerMaxSpeed {
			scale := vmath.Div(parameter.MissileSeekerMaxSpeed, speed)
			k.VelX = vmath.Mul(k.VelX, scale)
			k.VelY = vmath.Mul(k.VelY, scale)
		}

		// Integrate
		k.PreciseX += vmath.Mul(k.VelX, dt)
		k.PreciseY += vmath.Mul(k.VelY, dt)
	}

	// Path traversal for wall collision only (seekers pass through enemies)
	impactX, impactY, hitType := s.traverseForImpact(prevX, prevY, k.PreciseX, k.PreciseY, false)
	if hitType == impactWall {
		k.PreciseX = vmath.FromInt(impactX) + vmath.CellCenter
		k.PreciseY = vmath.FromInt(impactY) + vmath.CellCenter
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
// checkEnemies: true for parent (explodes on enemy), false for seeker (passes through)
// Returns impact grid position and type
func (s *MissileSystem) traverseForImpact(
	fromX, fromY, toX, toY int64,
	checkEnemies bool,
) (x, y int, hit impactType) {
	fromGridX, fromGridY := vmath.ToInt(fromX), vmath.ToInt(fromY)
	toGridX, toGridY := vmath.ToInt(toX), vmath.ToInt(toY)

	// No movement or same cell
	if fromGridX == toGridX && fromGridY == toGridY {
		return 0, 0, impactNone
	}

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

		// Enemy collision (parent only)
		if checkEnemies && s.hasCombatEntityAt(currX, currY) {
			return currX, currY, impactEnemy
		}

		lastSafeX, lastSafeY = currX, currY
	}

	return 0, 0, impactNone
}

// hasCombatEntityAt checks for drain or composite combat member at position
func (s *MissileSystem) hasCombatEntityAt(x, y int) bool {
	entities := s.world.Positions.GetAllEntityAt(x, y)
	for _, e := range entities {
		if s.world.Components.Drain.HasEntity(e) {
			return true
		}
		if memberComp, ok := s.world.Components.Member.GetComponent(e); ok {
			if headerComp, ok := s.world.Components.Header.GetComponent(memberComp.HeaderEntity); ok {
				switch headerComp.Behavior {
				case component.BehaviorQuasar, component.BehaviorSwarm, component.BehaviorStorm:
					return true
				}
			}
		}
	}
	return false
}

func (s *MissileSystem) resolveTarget(m *component.MissileComponent, missileX, missileY int64) (int64, int64, bool) {
	// Primary: assigned hit entity
	if m.HitEntity != 0 {
		if pos, ok := s.world.Positions.GetPosition(m.HitEntity); ok {
			x, y := vmath.CenteredFromGrid(pos.X, pos.Y)
			return x, y, true
		}
		m.HitEntity = 0 // Clear stale reference
	}

	// Secondary: target entity (header for composites)
	if m.TargetEntity != 0 && m.TargetEntity != m.HitEntity {
		if pos, ok := s.world.Positions.GetPosition(m.TargetEntity); ok {
			x, y := vmath.CenteredFromGrid(pos.X, pos.Y)
			return x, y, true
		}
		m.TargetEntity = 0 // Clear stale reference
	}

	// Retarget: nearest enemy
	newTarget := s.findNearestEnemy(missileX, missileY)
	if newTarget != 0 {
		m.TargetEntity = newTarget
		m.HitEntity = newTarget
		if pos, ok := s.world.Positions.GetPosition(newTarget); ok {
			x, y := vmath.CenteredFromGrid(pos.X, pos.Y)
			return x, y, true
		}
	}

	return 0, 0, false
}

func (s *MissileSystem) findNearestEnemy(fromX, fromY int64) core.Entity {
	var best core.Entity
	var bestDistSq int64 = -1

	for _, combatEntity := range s.world.Components.Combat.GetAllEntities() {
		combatComp, ok := s.world.Components.Combat.GetComponent(combatEntity)
		if !ok || combatComp.CombatEntityType == component.CombatEntityCursor {
			continue
		}

		pos, ok := s.world.Positions.GetPosition(combatEntity)
		if !ok {
			continue
		}

		tx, ty := vmath.CenteredFromGrid(pos.X, pos.Y)
		distSq := vmath.MagnitudeSq(tx-fromX, ty-fromY)

		if bestDistSq < 0 || distSq < bestDistSq {
			bestDistSq = distSq
			best = combatEntity
		}
	}

	return best
}

// --- Spawning ---

func (s *MissileSystem) spawnClusterParent(p *event.MissileSpawnRequestPayload) {
	// Calculate centroid of targets
	sumX, sumY, count := 0, 0, 0
	for _, t := range p.Targets {
		if pos, ok := s.world.Positions.GetPosition(t); ok {
			sumX += pos.X
			sumY += pos.Y
			count++
		}
	}

	destX, destY := p.TargetX, p.TargetY
	if count > 0 {
		destX = sumX / count
		destY = sumY / count
	}

	startX := vmath.FromInt(p.OriginX) + vmath.CellCenter
	startY := vmath.FromInt(p.OriginY) + vmath.CellCenter
	targetX := vmath.FromInt(destX) + vmath.CellCenter
	targetY := vmath.FromInt(destY) + vmath.CellCenter

	dirX, dirY := vmath.Normalize2D(targetX-startX, targetY-startY)

	e := s.world.CreateEntity()

	s.world.Components.Missile.SetComponent(e, component.MissileComponent{
		Type:        component.MissileTypeClusterParent,
		Phase:       component.MissilePhaseFlying,
		Owner:       p.OwnerEntity,
		Origin:      p.OriginEntity,
		ChildCount:  p.ChildCount,
		Targets:     p.Targets,
		HitEntities: p.HitEntities,
	})

	s.world.Components.Kinetic.SetComponent(e, component.KineticComponent{
		Kinetic: core.Kinetic{
			PreciseX: startX,
			PreciseY: startY,
			VelX:     vmath.Mul(dirX, parameter.MissileClusterLaunchSpeed),
			VelY:     vmath.Mul(dirY, parameter.MissileClusterLaunchSpeed),
			AccelX:   targetX, // Destination stored here
			AccelY:   targetY,
		},
	})

	s.world.Positions.SetPosition(e, component.PositionComponent{X: p.OriginX, Y: p.OriginY})
}

func (s *MissileSystem) performSplit(m *component.MissileComponent, k *component.KineticComponent) {
	if m.ChildCount <= 0 {
		return
	}

	originX, originY := k.PreciseX, k.PreciseY

	// Explosion at split point
	s.world.PushEvent(event.EventExplosionRequest, &event.ExplosionRequestPayload{
		X:      vmath.ToInt(originX),
		Y:      vmath.ToInt(originY),
		Radius: parameter.MissileExplosionRadius,
	})

	// Calculate spread arc
	spread := parameter.MissileSeekerSpreadAngle
	step := int64(0)
	if m.ChildCount > 1 {
		step = spread / int64(m.ChildCount-1)
	}
	startAngle := -spread / 2

	baseDirX, baseDirY := vmath.Normalize2D(k.VelX, k.VelY)

	for i := 0; i < m.ChildCount; i++ {
		angle := startAngle + step*int64(i)
		dirX, dirY := vmath.RotateVector(baseDirX, baseDirY, angle)

		// Stagger initial speed slightly
		speedFactor := vmath.Scale - vmath.FromFloat(parameter.MissileSeekerStaggerFactor*float64(i))
		speed := vmath.Mul(parameter.MissileSeekerMaxSpeed, speedFactor)

		vx := vmath.Mul(dirX, speed)
		vy := vmath.Mul(dirY, speed)

		var target, hit core.Entity
		if len(m.Targets) > 0 {
			target = m.Targets[i%len(m.Targets)]
			hit = m.HitEntities[i%len(m.HitEntities)]
		}

		s.spawnChild(m.Owner, m.Origin, originX, originY, vx, vy, target, hit)
	}
}

func (s *MissileSystem) spawnChild(owner, origin core.Entity, x, y, vx, vy int64, target, hit core.Entity) {
	e := s.world.CreateEntity()

	s.world.Components.Missile.SetComponent(e, component.MissileComponent{
		Type:         component.MissileTypeClusterChild,
		Phase:        component.MissilePhaseSeeking,
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

func (s *MissileSystem) destroyMissile(e core.Entity) {
	s.world.Components.Missile.RemoveEntity(e)
	s.world.Components.Kinetic.RemoveEntity(e)
	s.world.Positions.RemoveEntity(e)
	s.world.DestroyEntity(e)
}

func (s *MissileSystem) destroyAll() {
	for _, e := range s.world.Components.Missile.GetAllEntities() {
		s.destroyMissile(e)
	}
}