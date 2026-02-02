package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// MissileSystem manages cluster missile lifecycle
type MissileSystem struct {
	world *engine.World

	enabled bool
}

func NewMissileSystem(world *engine.World) engine.System {
	s := &MissileSystem{
		world: world,
	}
	s.Init()
	return s
}

func (s *MissileSystem) Init() {
	s.destroyAll()
	s.enabled = true
}

func (s *MissileSystem) Name() string {
	return "missile"
}

func (s *MissileSystem) Priority() int {
	return parameter.PriorityMissile
}

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

	dt := vmath.FromFloat(s.world.Resources.Time.DeltaTime.Seconds())
	config := s.world.Resources.Config

	entities := s.world.Components.Missile.GetAllEntities()
	var toDestroy []core.Entity

	for _, e := range entities {
		missile, ok := s.world.Components.Missile.GetComponent(e)
		if !ok {
			continue
		}

		kinetic, ok := s.world.Components.Kinetic.GetComponent(e)
		if !ok {
			continue
		}

		missile.Age++

		switch missile.Phase {
		case component.MissilePhaseFlying:
			s.updateFlying(&missile, &kinetic, dt)

			// Check split condition: velocity turned downward and minimum age
			if kinetic.VelY > 0 && missile.Age > parameter.MissileClusterMinAgeFrames {
				s.splitCluster(e, &missile, &kinetic)
				toDestroy = append(toDestroy, e)
				continue
			}

		case component.MissilePhaseSeeking:
			impacted := s.updateSeeking(&missile, &kinetic, dt)
			if impacted {
				s.emitImpact(&missile, &kinetic)
				toDestroy = append(toDestroy, e)
				continue
			}
		}

		// Bounds check - parents get more leeway for arc trajectory
		x, y := vmath.ToInt(kinetic.PreciseX), vmath.ToInt(kinetic.PreciseY)
		oobMargin := 10
		if missile.Type == component.MissileTypeClusterParent {
			oobMargin = 50 // Allow higher arc before OOB
		}
		if x < -oobMargin || x >= config.GameWidth+oobMargin ||
			y < -oobMargin || y >= config.GameHeight+oobMargin {
			toDestroy = append(toDestroy, e)
			continue
		}

		// Trail emission
		if missile.Age%parameter.MissileTrailInterval == 0 {
			s.pushTrail(&missile, kinetic.PreciseX, kinetic.PreciseY)
		}

		// Age trail points
		s.ageTrail(&missile)

		// Update position component for spatial queries
		s.world.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})

		s.world.Components.Missile.SetComponent(e, missile)
		s.world.Components.Kinetic.SetComponent(e, kinetic)
	}

	for _, e := range toDestroy {
		s.destroyMissile(e)
	}
}

// --- Spawn ---

func (s *MissileSystem) spawnClusterParent(p *event.MissileSpawnRequestPayload) {
	e := s.world.CreateEntity()

	// Calculate launch direction toward far quadrant with upward bias
	dx := vmath.FromInt(p.TargetX - p.OriginX)
	dy := vmath.FromInt(p.TargetY - p.OriginY)
	dirX, dirY := vmath.Normalize2D(dx, dy)

	// Apply upward bias rotation
	dirX, dirY = vmath.RotateVector(dirX, dirY, -parameter.MissileClusterLaunchAngle)

	// Initial velocity
	velX := vmath.Mul(dirX, parameter.MissileClusterLaunchSpeed)
	velY := vmath.Mul(dirY, parameter.MissileClusterLaunchSpeed)

	missile := component.MissileComponent{
		Type:        component.MissileTypeClusterParent,
		Phase:       component.MissilePhaseFlying,
		Owner:       p.OwnerEntity,
		Origin:      p.OriginEntity,
		ChildCount:  p.ChildCount,
		Targets:     p.Targets,
		HitEntities: p.HitEntities,
	}

	kinetic := component.KineticComponent{
		Kinetic: core.Kinetic{
			PreciseX: vmath.FromInt(p.OriginX) + vmath.CellCenter,
			PreciseY: vmath.FromInt(p.OriginY) + vmath.CellCenter,
			VelX:     velX,
			VelY:     velY,
		},
	}

	pos := component.PositionComponent{X: p.OriginX, Y: p.OriginY}

	s.world.Components.Missile.SetComponent(e, missile)
	s.world.Components.Kinetic.SetComponent(e, kinetic)
	s.world.Positions.SetPosition(e, pos)
}

func (s *MissileSystem) spawnChild(
	owner, origin core.Entity,
	x, y int64,
	velX, velY int64,
	target, hit core.Entity,
) {
	e := s.world.CreateEntity()

	missile := component.MissileComponent{
		Type:         component.MissileTypeClusterChild,
		Phase:        component.MissilePhaseSeeking,
		Owner:        owner,
		Origin:       origin,
		TargetEntity: target,
		HitEntity:    hit,
	}

	kinetic := component.KineticComponent{
		Kinetic: core.Kinetic{
			PreciseX: x,
			PreciseY: y,
			VelX:     velX,
			VelY:     velY,
		},
	}

	pos := component.PositionComponent{
		X: vmath.ToInt(x),
		Y: vmath.ToInt(y),
	}

	s.world.Components.Missile.SetComponent(e, missile)
	s.world.Components.Kinetic.SetComponent(e, kinetic)
	s.world.Positions.SetPosition(e, pos)
}

// --- Physics Updates ---

func (s *MissileSystem) updateFlying(m *component.MissileComponent, k *component.KineticComponent, dt int64) {
	// Apply gravity
	k.VelY += vmath.Mul(parameter.MissileClusterGravity, dt)

	// Integrate position
	k.PreciseX += vmath.Mul(k.VelX, dt)
	k.PreciseY += vmath.Mul(k.VelY, dt)
}

func (s *MissileSystem) updateSeeking(m *component.MissileComponent, k *component.KineticComponent, dt int64) bool {
	// Get target position
	targetX, targetY := s.resolveTargetPosition(m.HitEntity)
	// Target lost - try to retarget or continue trajectory
	if targetX == 0 && targetY == 0 {
		// Check if target entity still exists
		if m.HitEntity != 0 && !s.world.Positions.HasPosition(m.HitEntity) {
			// Target destroyed, clear assignment and coast
			m.HitEntity = 0
			m.TargetEntity = 0
		}
		// Continue on current trajectory
		k.PreciseX += vmath.Mul(k.VelX, dt)
		k.PreciseY += vmath.Mul(k.VelY, dt)
		return false
	}

	// Steering toward target
	dx := targetX - k.PreciseX
	dy := targetY - k.PreciseY
	dist := vmath.Magnitude(dx, dy)

	// Impact check
	if dist < parameter.MissileImpactRadius {
		return true
	}

	// Desired velocity toward target at max speed
	desiredX, desiredY := vmath.Normalize2D(dx, dy)
	desiredX = vmath.Mul(desiredX, parameter.MissileSeekerMaxSpeed)
	desiredY = vmath.Mul(desiredY, parameter.MissileSeekerMaxSpeed)

	// Steering force
	steerX := desiredX - k.VelX
	steerY := desiredY - k.VelY
	steerX, steerY = vmath.ClampMagnitude(steerX, steerY, parameter.MissileSeekerSteerForce)

	// Apply steering
	k.VelX += vmath.Mul(steerX, dt)
	k.VelY += vmath.Mul(steerY, dt)

	// Clamp to max speed
	k.VelX, k.VelY = vmath.ClampMagnitude(k.VelX, k.VelY, parameter.MissileSeekerMaxSpeed)

	// Integrate position
	k.PreciseX += vmath.Mul(k.VelX, dt)
	k.PreciseY += vmath.Mul(k.VelY, dt)

	return false
}

func (s *MissileSystem) resolveTargetPosition(hitEntity core.Entity) (int64, int64) {
	if hitEntity == 0 {
		return 0, 0
	}

	pos, ok := s.world.Positions.GetPosition(hitEntity)
	if !ok {
		return 0, 0
	}

	return vmath.FromInt(pos.X) + vmath.CellCenter, vmath.FromInt(pos.Y) + vmath.CellCenter
}

// --- Split Logic ---

func (s *MissileSystem) splitCluster(parentEntity core.Entity, m *component.MissileComponent, k *component.KineticComponent) {
	childCount := m.ChildCount
	if childCount <= 0 {
		return
	}

	targetCount := len(m.Targets)
	if targetCount == 0 {
		return
	}

	// Get parent velocity direction for spread center
	dirX, dirY := vmath.Normalize2D(k.VelX, k.VelY)
	if dirX == 0 && dirY == 0 {
		dirX = vmath.Scale // Fallback: rightward
	}

	// Spread children evenly across arc centered on velocity direction
	angleStep := int64(0)
	startAngle := int64(0)
	if childCount > 1 {
		angleStep = parameter.MissileSeekerSpreadAngle / int64(childCount-1)
		startAngle = -parameter.MissileSeekerSpreadAngle / 2
	}

	// Initial child speed (fraction of parent speed for spread effect)
	parentSpeed := vmath.Magnitude(k.VelX, k.VelY)
	childSpeed := parentSpeed / 2
	if childSpeed < vmath.FromFloat(20.0) {
		childSpeed = vmath.FromFloat(20.0)
	}

	for i := 0; i < childCount; i++ {
		// Angle offset for this child from parent direction
		angleOffset := startAngle + angleStep*int64(i)

		// Rotate parent direction by offset
		childDirX, childDirY := vmath.RotateVector(dirX, dirY, angleOffset)

		// Initial velocity
		velX := vmath.Mul(childDirX, childSpeed)
		velY := vmath.Mul(childDirY, childSpeed)

		// Target assignment (cycle through available targets)
		targetIdx := i % targetCount
		target := m.Targets[targetIdx]
		hit := m.HitEntities[targetIdx]

		s.spawnChild(
			m.Owner,
			m.Origin,
			k.PreciseX, k.PreciseY,
			velX, velY,
			target, hit,
		)
	}

	// Visual burst effect at split point
	s.world.PushEvent(event.EventExplosionRequest, &event.ExplosionRequestPayload{
		X:      vmath.ToInt(k.PreciseX),
		Y:      vmath.ToInt(k.PreciseY),
		Radius: vmath.FromFloat(4.0), // Small burst
	})
}

// --- Impact ---

func (s *MissileSystem) emitImpact(m *component.MissileComponent, k *component.KineticComponent) {
	impactX := vmath.ToInt(k.PreciseX)
	impactY := vmath.ToInt(k.PreciseY)

	// Emit impact event for combat system
	s.world.PushEvent(event.EventMissileImpact, &event.MissileImpactPayload{
		OwnerEntity:  m.Owner,
		TargetEntity: m.TargetEntity,
		HitEntity:    m.HitEntity,
		ImpactX:      impactX,
		ImpactY:      impactY,
	})

	// Explosion visual effect (smaller radius than main explosion)
	s.world.PushEvent(event.EventExplosionRequest, &event.ExplosionRequestPayload{
		X:      impactX,
		Y:      impactY,
		Radius: parameter.MissileExplosionRadius,
	})
}

// --- Trail Management ---

func (s *MissileSystem) pushTrail(m *component.MissileComponent, x, y int64) {
	m.Trail[m.TrailHead] = component.MissileTrailPoint{X: x, Y: y, Age: 0}
	m.TrailHead = (m.TrailHead + 1) % component.TrailCapacity
	if m.TrailLen < component.TrailCapacity {
		m.TrailLen++
	}
}

func (s *MissileSystem) ageTrail(m *component.MissileComponent) {
	for i := 0; i < m.TrailLen; i++ {
		idx := (m.TrailHead - m.TrailLen + i + component.TrailCapacity) % component.TrailCapacity
		m.Trail[idx].Age++
	}
}

// --- Cleanup ---

func (s *MissileSystem) destroyMissile(e core.Entity) {
	s.world.Components.Missile.RemoveEntity(e)
	s.world.Components.Kinetic.RemoveEntity(e)
	s.world.Positions.RemoveEntity(e)
	s.world.DestroyEntity(e)
}

func (s *MissileSystem) destroyAll() {
	entities := s.world.Components.Missile.GetAllEntities()
	for _, e := range entities {
		s.destroyMissile(e)
	}
}