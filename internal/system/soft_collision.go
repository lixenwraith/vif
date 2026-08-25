package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/profile"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// collisionEntry holds cached entity data for soft collision processing
type collisionEntry struct {
	entity core.Entity
	x, y   int
}

// SoftCollisionRule defines a single soft collision interaction
type SoftCollisionRule struct {
	Profile     *physics.CollisionProfile
	SourceInvRx float64 // Source collision radius (inverse squared X)
	SourceInvRy float64 // Source collision radius (inverse squared Y)
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

// SoftCollisionSystem centralizes inter-species soft collision and flocking separation
type SoftCollisionSystem struct {
	world *engine.World
	rng   *vmath.FastRand

	// Internal position caches (rebuilt each tick)
	drains  []collisionEntry
	swarms  []collisionEntry
	quasars []collisionEntry
	storms  []collisionEntry // Circle positions, not root
	pylons  []collisionEntry

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
	s := &SoftCollisionSystem{
		world:   world,
		drains:  make([]collisionEntry, 0, 16),
		swarms:  make([]collisionEntry, 0, 8),
		quasars: make([]collisionEntry, 0, 4),
		storms:  make([]collisionEntry, 0, 12), // 3 circles * potential multiple storms
		pylons:  make([]collisionEntry, 0, 4),
	}
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
	s.rng = s.world.Rand(core.DomainShared, s.Name())
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

	dtSec := min(s.world.Resources.Time.DeltaTime.Seconds(), 0.1)

	s.rebuildCaches()
	s.processAllCollisions()
	s.processAllFlocking(dtSec)
}

// clearCaches resets all cache slices
func (s *SoftCollisionSystem) clearCaches() {
	s.drains = s.drains[:0]
	s.swarms = s.swarms[:0]
	s.quasars = s.quasars[:0]
	s.storms = s.storms[:0]
	s.pylons = s.pylons[:0]
}

// rebuildCaches populates position caches from component stores
func (s *SoftCollisionSystem) rebuildCaches() {
	s.clearCaches()

	// Drains
	for _, entity := range s.world.Components.Drain.Entities() {
		if pos, ok := s.world.Positions.GetPosition(entity); ok {
			s.drains = append(s.drains, collisionEntry{entity: entity, x: pos.X, y: pos.Y})
		}
	}

	// Swarms (header positions)
	for _, entity := range s.world.Components.Swarm.Entities() {
		if pos, ok := s.world.Positions.GetPosition(entity); ok {
			s.swarms = append(s.swarms, collisionEntry{entity: entity, x: pos.X, y: pos.Y})
		}
	}

	// Quasars (header positions)
	for _, entity := range s.world.Components.Quasar.Entities() {
		if pos, ok := s.world.Positions.GetPosition(entity); ok {
			s.quasars = append(s.quasars, collisionEntry{entity: entity, x: pos.X, y: pos.Y})
		}
	}

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
				s.storms = append(s.storms, collisionEntry{entity: circleEntity, x: pos.X, y: pos.Y})
			}
		}
	}

	// Pylons (use spawn position - stationary)
	for _, entity := range s.world.Components.Pylon.Entities() {
		pylonComp, ok := s.world.Components.Pylon.GetPtr(entity)
		if !ok {
			continue
		}
		s.pylons = append(s.pylons, collisionEntry{entity: entity, x: pylonComp.SpawnX, y: pylonComp.SpawnY})
	}
	s.buffers.Observe(0, len(s.drains))
	s.buffers.Observe(1, len(s.swarms))
	s.buffers.Observe(2, len(s.quasars))
	s.buffers.Observe(3, len(s.storms))
	s.buffers.Observe(4, len(s.pylons))
}

// getCache returns the cache slice for a given species type
func (s *SoftCollisionSystem) getCache(species component.SpeciesType) []collisionEntry {
	switch species {
	case component.SpeciesDrain:
		return s.drains
	case component.SpeciesSwarm:
		return s.swarms
	case component.SpeciesQuasar:
		return s.quasars
	case component.SpeciesStorm:
		return s.storms
	case component.SpeciesPylon:
		return s.pylons
	default:
		return nil
	}
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
	sources := s.getCache(sourceType)
	targets := s.getCache(targetType)

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

			s.tryApplyCollision(src.x, src.y, tgt.entity, rule)
		}
	}
}

// tryApplyCollision checks and applies collision from source position to target entity
func (s *SoftCollisionSystem) tryApplyCollision(
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

	// Check collision
	radialX, radialY, hit := physics.CheckSoftCollision(
		targetPos.X, targetPos.Y,
		sourceX, sourceY,
		rule.SourceInvRx, rule.SourceInvRy,
	)
	if !hit {
		return
	}

	impulseX, impulseY := physics.ImpulseFromProfile(radialX, radialY, rule.Profile, s.rng)

	physics.ApplyImpulse(&kineticComp.Kinetic, impulseX, impulseY)
	s.statCollisions.Add(1)

	// Set immunity
	combatComp.RemainingKineticImmunity = parameter.SoftCollisionImmunityDuration

}

// processAllFlocking calculates and integrates continuous separation acceleration
func (s *SoftCollisionSystem) processAllFlocking(dtSec float64) {
	// Loop over targets first to accumulate acceleration and minimize ECS writes
	for targetType := component.SpeciesType(1); targetType < component.SpeciesCount; targetType++ {
		targets := s.getCache(targetType)
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

				sources := s.getCache(sourceType)
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
