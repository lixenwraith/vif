package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/profile"
	"github.com/lixenwraith/vif/pkg/vmath"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
)

// collisionEntry holds cached entity data for soft collision processing
type collisionEntry struct {
	entity core.Entity
	x, y   int
}

// SoftCollisionRule defines a single soft collision interaction
type SoftCollisionRule struct {
	Profile         *physics.CollisionProfile
	SourceInvRx     float64 // Source collision radius (inverse squared X)
	SourceInvRy     float64 // Source collision radius (inverse squared Y)
	MemberFootprint bool    // Irregular composites collide through their occupied cells
}

// SoftCollisionMatrix maps [Source][Target] → Rule
// Source pushes Target away; nil entry = no interaction
type SoftCollisionMatrix [component.SpeciesCount][component.SpeciesCount]*SoftCollisionRule

// FlockingRule defines a single flocking separation interaction
type FlockingRule struct {
	InvRxSq    float64 // Separation ellipse inverse X radius squared
	InvRySq    float64 // Separation ellipse inverse Y radius squared
	MaxDist    float64 // Max distance (cells) for weight calculation
	Strength   float64 // Base acceleration strength (cells/sec²)
	WeightMult float64 // Multiplier (e.g. for lower quasar influence)
}

// FlockingMatrix maps → Rule
// Source repels Target. nil entry = no flocking interaction
type FlockingMatrix [component.SpeciesCount][component.SpeciesCount]*FlockingRule

// SoftCollisionSystem centralizes inter-species soft collision and flocking separation.
// Dual-domain (D-7): it observes both domains and may write kinetic in either, so its
// impulse stream is selected by the target's domain.
type SoftCollisionSystem struct {
	world *engine.World

	// sharedRoot seeds a shared impulse from the pair that produced it; rngPlayer
	// is an ordinary stream because a player impulse is this instance's alone.
	sharedRoot  uint64
	rngArtifact vmath.FastRand
	rngPlayer   *vmath.FastRand

	caches [component.SpeciesCount][]collisionEntry

	// Collision and flocking matrices
	matrix            SoftCollisionMatrix
	flockMatrix       FlockingMatrix
	statCollisions    *atomic.Int64
	statImmuneRejects *atomic.Int64
	buffers           bufferTelemetry

	enabled bool
}

// NewSoftCollisionSystem creates the centralized soft collision system
func NewSoftCollisionSystem(world *engine.World) engine.System {
	s := &SoftCollisionSystem{world: world}
	s.statCollisions = world.Resources.Status.Ints.Get("soft_collision.collisions")
	s.statImmuneRejects = world.Resources.Status.Ints.Get("soft_collision.immune_rejects")
	s.buffers = newBufferTelemetry(world.Resources.Status, "soft_collision", "drains", "swarms", "quasars", "storms", "pylons")

	s.initMatrix()
	s.initFlockingMatrix()
	s.Init()
	return s
}

// initMatrix populates the collision rule matrix
func (s *SoftCollisionSystem) initMatrix() {
	// Swarm pushes Swarm (bidirectional via separate entries)
	s.matrix[component.SpeciesSwarm][component.SpeciesSwarm] = &SoftCollisionRule{
		Profile:     &profile.SoftSwarmToSwarm,
		SourceInvRx: parameter.SwarmCollisionInvRxSq,
		SourceInvRy: parameter.SwarmCollisionInvRySq,
	}

	// Swarm pushes Quasar
	s.matrix[component.SpeciesSwarm][component.SpeciesQuasar] = &SoftCollisionRule{
		Profile:     &profile.SoftSwarmToQuasar,
		SourceInvRx: parameter.SwarmCollisionInvRxSq,
		SourceInvRy: parameter.SwarmCollisionInvRySq,
	}

	// Quasar pushes Swarm
	s.matrix[component.SpeciesQuasar][component.SpeciesSwarm] = &SoftCollisionRule{
		Profile:     &profile.SoftQuasarToSwarm,
		SourceInvRx: parameter.QuasarCollisionInvRxSq,
		SourceInvRy: parameter.QuasarCollisionInvRySq,
	}

	// Quasar pushes Quasar (bidirectional)
	s.matrix[component.SpeciesQuasar][component.SpeciesQuasar] = &SoftCollisionRule{
		Profile:     &profile.SoftQuasarToQuasar,
		SourceInvRx: parameter.QuasarCollisionInvRxSq,
		SourceInvRy: parameter.QuasarCollisionInvRySq,
	}

	// Storm pushes Swarm (reuse quasar profile per existing code)
	s.matrix[component.SpeciesStorm][component.SpeciesSwarm] = &SoftCollisionRule{
		Profile:     &profile.SoftQuasarToSwarm,
		SourceInvRx: parameter.StormCircleCollisionInvRxSq,
		SourceInvRy: parameter.StormCircleCollisionInvRySq,
	}

	// Storm pushes Quasar (reuse swarm-to-quasar profile per existing code)
	s.matrix[component.SpeciesStorm][component.SpeciesQuasar] = &SoftCollisionRule{
		Profile:     &profile.SoftSwarmToQuasar,
		SourceInvRx: parameter.StormCircleCollisionInvRxSq,
		SourceInvRy: parameter.StormCircleCollisionInvRySq,
	}

	// Pylon pushes Swarm
	s.matrix[component.SpeciesPylon][component.SpeciesSwarm] = &SoftCollisionRule{
		Profile:     &profile.SoftPylonToSwarm,
		SourceInvRx: parameter.PylonCollisionInvRxSq,
		SourceInvRy: parameter.PylonCollisionInvRySq,
	}

	// Pylon pushes Quasar
	s.matrix[component.SpeciesPylon][component.SpeciesQuasar] = &SoftCollisionRule{
		Profile:     &profile.SoftPylonToQuasar,
		SourceInvRx: parameter.PylonCollisionInvRxSq,
		SourceInvRy: parameter.PylonCollisionInvRySq,
	}
	for species := component.SpeciesType(1); species < component.SpeciesCount; species++ {
		if profile.SoftKraken[species].MassRatio == 0 {
			continue
		}
		s.matrix[component.SpeciesKraken][species] = &SoftCollisionRule{
			Profile: &profile.SoftKraken[species], MemberFootprint: true,
		}
	}
}

// initFlockingMatrix populates the flocking separation rules
func (s *SoftCollisionSystem) initFlockingMatrix() {
	// Shared species flock together. Drains are a separate, local flock: they
	// react to shared species, but never contribute acceleration back into the
	// shared flock. Pylons (stationary) and Storms (complex orbital physics) are
	// intentionally excluded from continuous flocking.
	sharedSpecies := []component.SpeciesType{
		component.SpeciesSwarm,
		component.SpeciesQuasar,
	}

	defaultRule := FlockingRule{
		InvRxSq:    parameter.FlockingSeparationInvRxSq,
		InvRySq:    parameter.FlockingSeparationInvRySq,
		MaxDist:    parameter.FlockingSeparationRadiusX,
		Strength:   parameter.SwarmSeparationStrength,
		WeightMult: 1.0,
	}

	for _, src := range sharedSpecies {
		for _, tgt := range sharedSpecies {
			// Allocate individual rule configs
			rule := defaultRule

			// Specific overrides based on behavioral design
			if src == component.SpeciesQuasar && tgt == component.SpeciesSwarm {
				rule.WeightMult = parameter.SwarmQuasarSeparationWeight
			}

			s.flockMatrix[src][tgt] = &rule
		}
	}

	// Local drain flock: drains separate from each other and observe the shared
	// flock. The reverse entries deliberately remain nil so shared motion never
	// reads or depends on drain positions.
	drainRule := defaultRule
	s.flockMatrix[component.SpeciesDrain][component.SpeciesDrain] = &drainRule
	for _, src := range sharedSpecies {
		rule := defaultRule
		s.flockMatrix[src][component.SpeciesDrain] = &rule
	}
}

func (s *SoftCollisionSystem) Init() {
	s.sharedRoot = vmath.DeriveSeed(s.world.Resources.Rand.DomainRoot(core.DomainShared), s.Name())
	s.rngPlayer = s.world.Rand(core.DomainPlayer, s.Name())
	s.clearCaches()
	s.statCollisions.Store(0)
	s.statImmuneRejects.Store(0)
	s.buffers.Reset()
	s.enabled = true
}

func (s *SoftCollisionSystem) Name() string {
	return "soft_collision"
}

func (s *SoftCollisionSystem) Priority() int {
	return parameter.PrioritySoftCollision
}

func (s *SoftCollisionSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGameResetRequest,
		event.EventMetaSystemCommandRequest,
	}
}

func (s *SoftCollisionSystem) HandleEvent(ev event.GameEvent) {
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
	}
}

func (s *SoftCollisionSystem) Update() {
	if !s.enabled {
		return
	}

	dtSec := min(s.world.Resources.Time.DeltaTime.Seconds(), parameter.MaxSimulationDeltaSeconds)

	s.rebuildCaches()
	s.processAllCollisions()
	s.processAllFlocking(dtSec)
}

// clearCaches resets all cache slices
func (s *SoftCollisionSystem) clearCaches() {
	for i := range s.caches {
		s.caches[i] = s.caches[i][:0]
	}
}

// rebuildCaches populates position caches from component stores
func (s *SoftCollisionSystem) rebuildCaches() {
	s.clearCaches()

	// Storms (circle positions, not root)
	for _, rootEntity := range s.world.Components.Storm.Entities() {
		stormComp, ok := s.world.Components.Storm.GetPtr(rootEntity)
		if !ok {
			continue
		}
		for i := range component.StormCircleCount {
			if !stormComp.CirclesAlive[i] {
				continue
			}
			circleEntity := stormComp.Circles[i]
			if pos, ok := s.world.Positions.GetPosition(circleEntity); ok {
				s.caches[component.SpeciesStorm] = append(s.caches[component.SpeciesStorm], collisionEntry{entity: circleEntity, x: pos.X, y: pos.Y})
			}
		}
	}

	// Pylons (use spawn position - stationary)
	for _, entity := range s.world.Components.Pylon.Entities() {
		pylonComp, ok := s.world.Components.Pylon.GetPtr(entity)
		if !ok {
			continue
		}
		s.caches[component.SpeciesPylon] = append(s.caches[component.SpeciesPylon], collisionEntry{entity: entity, x: pylonComp.SpawnX, y: pylonComp.SpawnY})
	}
	for _, group := range []struct {
		species  component.SpeciesType
		entities []core.Entity
	}{
		{component.SpeciesDrain, s.world.Components.Drain.Entities()},
		{component.SpeciesSwarm, s.world.Components.Swarm.Entities()},
		{component.SpeciesQuasar, s.world.Components.Quasar.Entities()},
		{component.SpeciesKraken, s.world.Components.Kraken.Entities()},
		{component.SpeciesEye, s.world.Components.Eye.Entities()},
		{component.SpeciesSnake, s.world.Components.SnakeHead.Entities()},
		{component.SpeciesSnake, s.world.Components.SnakeBody.Entities()},
	} {
		for _, entity := range group.entities {
			if pos, ok := s.world.Positions.GetPosition(entity); ok {
				s.caches[group.species] = append(s.caches[group.species], collisionEntry{entity: entity, x: pos.X, y: pos.Y})
			}
		}
	}
	s.buffers.Observe(0, len(s.caches[component.SpeciesDrain]))
	s.buffers.Observe(1, len(s.caches[component.SpeciesSwarm]))
	s.buffers.Observe(2, len(s.caches[component.SpeciesQuasar]))
	s.buffers.Observe(3, len(s.caches[component.SpeciesStorm]))
	s.buffers.Observe(4, len(s.caches[component.SpeciesPylon]))
}

// processAllCollisions iterates the matrix and applies collisions
func (s *SoftCollisionSystem) processAllCollisions() {
	for sourceType := component.SpeciesType(1); sourceType < component.SpeciesCount; sourceType++ {
		for targetType := component.SpeciesType(1); targetType < component.SpeciesCount; targetType++ {
			rule := s.matrix[sourceType][targetType]
			if rule == nil {
				continue
			}
			s.processCollisionPair(sourceType, targetType, rule)
		}
	}
}

// processCollisionPair handles collisions between source and target species
func (s *SoftCollisionSystem) processCollisionPair(
	sourceType, targetType component.SpeciesType,
	rule *SoftCollisionRule,
) {
	sources := s.caches[sourceType]
	targets := s.caches[targetType]

	if len(sources) == 0 || len(targets) == 0 {
		return
	}

	for i := range sources {
		src := &sources[i]

		for j := range targets {
			tgt := &targets[j]

			// Skip self-collision for same-species interactions
			if src.entity == tgt.entity {
				continue
			}

			s.tryApplyCollision(src.entity, src.x, src.y, tgt.entity, rule)
		}
	}
}

// Shared impulses use the tick and pair so predicted deaths cannot shift later
// draws (D-8). Player targets keep their local stream; entity domain bits are mixed.
func (s *SoftCollisionSystem) impulseStream(source, target core.Entity) *vmath.FastRand {
	if target.Domain() == core.DomainPlayer {
		return s.rngPlayer
	}
	tick := s.world.Resources.Game.State.GetGameTicks()
	s.rngArtifact.Reseed(vmath.Mix64(s.sharedRoot ^ tick*0x9E3779B97F4A7C15 ^
		uint64(source)*0xD6E8FEB86659FD93 ^ uint64(target)))
	return &s.rngArtifact
}

// tryApplyCollision checks and applies collision from source position to target entity
func (s *SoftCollisionSystem) tryApplyCollision(
	sourceEntity core.Entity,
	sourceX, sourceY int,
	targetEntity core.Entity,
	rule *SoftCollisionRule,
) {
	// Get target kinetic component
	kineticComp, ok := s.world.Components.Kinetic.GetPtr(targetEntity)
	if !ok {
		return
	}

	// Get target combat component for immunity/enrage check
	combatComp, ok := s.world.Components.Combat.GetPtr(targetEntity)
	if !ok {
		return
	}

	// Skip if immune or enraged
	if combatComp.RemainingKineticImmunity > 0 || combatComp.IsEnraged {
		s.statImmuneRejects.Add(1)
		return
	}

	// Get target position
	targetPos, ok := s.world.Positions.GetPosition(targetEntity)
	if !ok {
		return
	}

	var radialX, radialY float64
	if rule.MemberFootprint {
		if !s.footprintsOverlap(sourceEntity, targetEntity, targetPos) {
			return
		}
		radialX, radialY = float64(targetPos.X-sourceX), float64(targetPos.Y-sourceY)
		if radialX == 0 && radialY == 0 {
			radialX = 1
		}
	} else {
		var hit bool
		radialX, radialY, hit = physics.CheckSoftCollision(
			targetPos.X, targetPos.Y, sourceX, sourceY, rule.SourceInvRx, rule.SourceInvRy,
		)
		if !hit {
			return
		}
	}

	impulseX, impulseY := physics.ImpulseFromProfile(radialX, radialY, rule.Profile,
		s.impulseStream(sourceEntity, targetEntity))

	physics.ApplyImpulse(&kineticComp.Kinetic, impulseX, impulseY)
	s.statCollisions.Add(1)

	// Set immunity
	combatComp.SealKineticImmunity(parameter.SoftCollisionImmunityDuration)

}

func (s *SoftCollisionSystem) footprintsOverlap(source, target core.Entity, pos component.PositionComponent) bool {
	contains := func(x, y int) bool {
		var occupants [parameter.MaxEntitiesPerCell]core.Entity
		n := s.world.Positions.GetEntitiesAtInto(x, y, engine.ScopeShared, occupants[:])
		for _, occupant := range occupants[:n] {
			if member, ok := s.world.Components.Member.GetPtr(occupant); ok && member.HeaderEntity == source {
				return true
			}
		}
		return false
	}
	if contains(pos.X, pos.Y) {
		return true
	}
	if header, ok := s.world.Components.Header.GetPtr(target); ok {
		for _, member := range header.MemberEntries {
			if p, ok := s.world.Positions.GetPosition(member.Entity); ok && contains(p.X, p.Y) {
				return true
			}
		}
	}
	return false
}

// processAllFlocking calculates and integrates continuous separation acceleration
func (s *SoftCollisionSystem) processAllFlocking(dtSec float64) {
	// Loop over targets first to accumulate acceleration and minimize ECS writes
	for targetType := component.SpeciesType(1); targetType < component.SpeciesCount; targetType++ {
		targets := s.caches[targetType]
		if len(targets) == 0 {
			continue
		}

		for i := range targets {
			tgt := &targets[i]

			combatComp, ok := s.world.Components.Combat.GetComponent(tgt.entity)
			// Flocking does not apply if dead, immune to kinetic shifts (recently hit), enraged (attacking), or stunned
			if !ok || combatComp.HitPoints <= 0 || combatComp.RemainingKineticImmunity > 0 || combatComp.IsEnraged || combatComp.StunnedRemaining > 0 {
				continue
			}

			kineticComp, ok := s.world.Components.Kinetic.GetPtr(tgt.entity)
			if !ok {
				continue
			}

			var totalAccelX, totalAccelY float64
			hasFlocking := false

			// Accumulate repulsion from all active sources
			for sourceType := component.SpeciesType(1); sourceType < component.SpeciesCount; sourceType++ {
				rule := s.flockMatrix[sourceType][targetType]
				if rule == nil {
					continue
				}

				sources := s.caches[sourceType]
				for j := range sources {
					src := &sources[j]
					if src.entity == tgt.entity { // Prevent self-repulsion
						continue
					}

					accX, accY, applied := s.calculateFlockingAccel(src.x, src.y, tgt.x, tgt.y, rule)
					if applied {
						totalAccelX += accX
						totalAccelY += accY
						hasFlocking = true
					}
				}
			}

			// Integrate and apply accumulated acceleration
			if hasFlocking {
				kineticComp.VelX += totalAccelX * dtSec
				kineticComp.VelY += totalAccelY * dtSec
			}
		}
	}
}

// calculateFlockingAccel computes the separation vector pushed onto the target by the source
func (s *SoftCollisionSystem) calculateFlockingAccel(
	sourceX, sourceY int,
	targetX, targetY int,
	rule *FlockingRule,
) (accelX, accelY float64, applied bool) {
	// Source is the center. Does its ellipse overlap the target?
	if !vmath.EllipseContainsPointF(targetX, targetY, sourceX, sourceY, rule.InvRxSq, rule.InvRySq) {
		return 0, 0, false
	}

	// Vector points from Source to Target (pushing Target away)
	dx := float64(targetX - sourceX)
	dy := float64(targetY - sourceY)

	if dx == 0 && dy == 0 {
		dx = 1.0 // Fallback rightwards to prevent stacking lock
	}

	dist := vmath.MagnitudeF(dx, dy)
	dirX, dirY := dx/dist, dy/dist

	// Weight inversely proportional to distance: (MaxDist - dist) / MaxDist
	weight := max((rule.MaxDist-dist)/rule.MaxDist, 0)

	// Apply species-specific interaction modifier and base strength
	weight *= rule.WeightMult
	accelMag := rule.Strength * weight

	return dirX * accelMag, dirY * accelMag, true
}
