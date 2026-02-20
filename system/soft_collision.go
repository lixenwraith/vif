package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/physics"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// collisionEntry holds cached entity data for soft collision processing
type collisionEntry struct {
	entity core.Entity
	x, y   int
}

// SoftCollisionRule defines a single soft collision interaction
type SoftCollisionRule struct {
	Profile     *physics.CollisionProfile
	SourceInvRx int64 // Source collision radius (inverse squared X)
	SourceInvRy int64 // Source collision radius (inverse squared Y)
}

// SoftCollisionMatrix maps [Source][Target] → Rule
// Source pushes Target away; nil entry = no interaction
type SoftCollisionMatrix [component.SpeciesCount][component.SpeciesCount]*SoftCollisionRule

// FlockingRule defines a single flocking separation interaction
type FlockingRule struct {
	InvRxSq    int64 // Separation ellipse inverse X radius squared (Q32.32)
	InvRySq    int64 // Separation ellipse inverse Y radius squared (Q32.32)
	MaxDist    int64 // Q32.32 max distance for weight calculation
	Strength   int64 // Q32.32 base acceleration strength
	WeightMult int64 // Q32.32 multiplier (e.g. for lower quasar influence)
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
	matrix      SoftCollisionMatrix
	flockMatrix FlockingMatrix

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

	s.initMatrix()
	s.initFlockingMatrix()
	s.Init()
	return s
}

// initMatrix populates the collision rule matrix
func (s *SoftCollisionSystem) initMatrix() {
	// Quasar pushes Drain
	s.matrix[component.SpeciesQuasar][component.SpeciesDrain] = &SoftCollisionRule{
		Profile:     &physics.SoftCollisionQuasarToDrain,
		SourceInvRx: parameter.QuasarCollisionInvRxSq,
		SourceInvRy: parameter.QuasarCollisionInvRySq,
	}

	// Swarm pushes Swarm (bidirectional via separate entries)
	s.matrix[component.SpeciesSwarm][component.SpeciesSwarm] = &SoftCollisionRule{
		Profile:     &physics.SoftCollisionSwarmToSwarm,
		SourceInvRx: parameter.SwarmCollisionInvRxSq,
		SourceInvRy: parameter.SwarmCollisionInvRySq,
	}

	// Swarm pushes Quasar
	s.matrix[component.SpeciesSwarm][component.SpeciesQuasar] = &SoftCollisionRule{
		Profile:     &physics.SoftCollisionSwarmToQuasar,
		SourceInvRx: parameter.SwarmCollisionInvRxSq,
		SourceInvRy: parameter.SwarmCollisionInvRySq,
	}

	// Quasar pushes Swarm
	s.matrix[component.SpeciesQuasar][component.SpeciesSwarm] = &SoftCollisionRule{
		Profile:     &physics.SoftCollisionQuasarToSwarm,
		SourceInvRx: parameter.QuasarCollisionInvRxSq,
		SourceInvRy: parameter.QuasarCollisionInvRySq,
	}

	// Quasar pushes Quasar (bidirectional)
	s.matrix[component.SpeciesQuasar][component.SpeciesQuasar] = &SoftCollisionRule{
		Profile:     &physics.SoftCollisionQuasarToQuasar,
		SourceInvRx: parameter.QuasarCollisionInvRxSq,
		SourceInvRy: parameter.QuasarCollisionInvRySq,
	}

	// Storm pushes Swarm (reuse quasar profile per existing code)
	s.matrix[component.SpeciesStorm][component.SpeciesSwarm] = &SoftCollisionRule{
		Profile:     &physics.SoftCollisionQuasarToSwarm,
		SourceInvRx: parameter.StormCollisionInvRxSq,
		SourceInvRy: parameter.StormCollisionInvRySq,
	}

	// Storm pushes Quasar (reuse swarm-to-quasar profile per existing code)
	s.matrix[component.SpeciesStorm][component.SpeciesQuasar] = &SoftCollisionRule{
		Profile:     &physics.SoftCollisionSwarmToQuasar,
		SourceInvRx: parameter.StormCollisionInvRxSq,
		SourceInvRy: parameter.StormCollisionInvRySq,
	}

	// Pylon pushes Drain
	s.matrix[component.SpeciesPylon][component.SpeciesDrain] = &SoftCollisionRule{
		Profile:     &physics.SoftCollisionPylonToDrain,
		SourceInvRx: parameter.PylonCollisionInvRxSq,
		SourceInvRy: parameter.PylonCollisionInvRySq,
	}

	// Pylon pushes Swarm
	s.matrix[component.SpeciesPylon][component.SpeciesSwarm] = &SoftCollisionRule{
		Profile:     &physics.SoftCollisionPylonToSwarm,
		SourceInvRx: parameter.PylonCollisionInvRxSq,
		SourceInvRy: parameter.PylonCollisionInvRySq,
	}

	// Pylon pushes Quasar
	s.matrix[component.SpeciesPylon][component.SpeciesQuasar] = &SoftCollisionRule{
		Profile:     &physics.SoftCollisionPylonToQuasar,
		SourceInvRx: parameter.PylonCollisionInvRxSq,
		SourceInvRy: parameter.PylonCollisionInvRySq,
	}
}

// initFlockingMatrix populates the flocking separation rules
func (s *SoftCollisionSystem) initFlockingMatrix() {
	// Whitelist of entities participating in continuous flocking separation.
	// Pylons (stationary) and Storms (complex orbital physics) are intentionally excluded.
	flockingSpecies := []component.SpeciesType{
		component.SpeciesDrain,
		component.SpeciesSwarm,
		component.SpeciesQuasar,
	}

	defaultRule := FlockingRule{
		InvRxSq:    parameter.FlockingSeparationInvRxSq,
		InvRySq:    parameter.FlockingSeparationInvRySq,
		MaxDist:    parameter.FlockingSeparationRadiusX,
		Strength:   parameter.SwarmSeparationStrength,
		WeightMult: vmath.Scale,
	}

	for _, src := range flockingSpecies {
		for _, tgt := range flockingSpecies {
			// Allocate individual rule configs
			rule := defaultRule

			// Specific overrides based on behavioral design
			if src == component.SpeciesQuasar && tgt == component.SpeciesSwarm {
				rule.WeightMult = vmath.FromFloat(parameter.SwarmQuasarSeparationWeight)
			}

			s.flockMatrix[src][tgt] = &rule
		}
	}
}

func (s *SoftCollisionSystem) Init() {
	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTime.UnixNano()))
	s.clearCaches()
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
		event.EventGameReset,
		event.EventMetaSystemCommandRequest,
	}
}

func (s *SoftCollisionSystem) HandleEvent(ev event.GameEvent) {
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
	}
}

func (s *SoftCollisionSystem) Update() {
	if !s.enabled {
		return
	}

	dtFixed := vmath.FromFloat(s.world.Resources.Time.DeltaTime.Seconds())
	if dtCap := vmath.FromFloat(0.1); dtFixed > dtCap {
		dtFixed = dtCap
	}

	s.rebuildCaches()
	s.processAllCollisions()
	s.processAllFlocking(dtFixed)
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
	for _, entity := range s.world.Components.Drain.GetAllEntities() {
		if pos, ok := s.world.Positions.GetPosition(entity); ok {
			s.drains = append(s.drains, collisionEntry{entity: entity, x: pos.X, y: pos.Y})
		}
	}

	// Swarms (header positions)
	for _, entity := range s.world.Components.Swarm.GetAllEntities() {
		if pos, ok := s.world.Positions.GetPosition(entity); ok {
			s.swarms = append(s.swarms, collisionEntry{entity: entity, x: pos.X, y: pos.Y})
		}
	}

	// Quasars (header positions)
	for _, entity := range s.world.Components.Quasar.GetAllEntities() {
		if pos, ok := s.world.Positions.GetPosition(entity); ok {
			s.quasars = append(s.quasars, collisionEntry{entity: entity, x: pos.X, y: pos.Y})
		}
	}

	// Storms (circle positions, not root)
	for _, rootEntity := range s.world.Components.Storm.GetAllEntities() {
		stormComp, ok := s.world.Components.Storm.GetComponent(rootEntity)
		if !ok {
			continue
		}
		for i := 0; i < component.StormCircleCount; i++ {
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
	for _, entity := range s.world.Components.Pylon.GetAllEntities() {
		pylonComp, ok := s.world.Components.Pylon.GetComponent(entity)
		if !ok {
			continue
		}
		s.pylons = append(s.pylons, collisionEntry{entity: entity, x: pylonComp.SpawnX, y: pylonComp.SpawnY})
	}
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
	kineticComp, ok := s.world.Components.Kinetic.GetComponent(targetEntity)
	if !ok {
		return
	}

	// Get target combat component for immunity/enrage check
	combatComp, ok := s.world.Components.Combat.GetComponent(targetEntity)
	if !ok {
		return
	}

	// Skip if immune or enraged
	if combatComp.RemainingKineticImmunity > 0 || combatComp.IsEnraged {
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

	// Apply collision impulse
	physics.ApplyCollision(&kineticComp.Kinetic, radialX, radialY, rule.Profile, s.rng)

	// Set immunity
	combatComp.RemainingKineticImmunity = parameter.SoftCollisionImmunityDuration

	// Write back components
	s.world.Components.Kinetic.SetComponent(targetEntity, kineticComp)
	s.world.Components.Combat.SetComponent(targetEntity, combatComp)
}

// processAllFlocking calculates and integrates continuous separation acceleration
func (s *SoftCollisionSystem) processAllFlocking(dtFixed int64) {
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

			kineticComp, ok := s.world.Components.Kinetic.GetComponent(tgt.entity)
			if !ok {
				continue
			}

			var totalAccelX, totalAccelY int64
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
				kineticComp.VelX += vmath.Mul(totalAccelX, dtFixed)
				kineticComp.VelY += vmath.Mul(totalAccelY, dtFixed)
				s.world.Components.Kinetic.SetComponent(tgt.entity, kineticComp)
			}
		}
	}
}

// calculateFlockingAccel computes the separation vector pushed onto the target by the source
func (s *SoftCollisionSystem) calculateFlockingAccel(
	sourceX, sourceY int,
	targetX, targetY int,
	rule *FlockingRule,
) (accelX, accelY int64, applied bool) {
	// Source is the center. Does its ellipse overlap the target?
	if !vmath.EllipseContainsPoint(targetX, targetY, sourceX, sourceY, rule.InvRxSq, rule.InvRySq) {
		return 0, 0, false
	}

	// Vector points from Source to Target (pushing Target away)
	dx := vmath.FromInt(targetX - sourceX)
	dy := vmath.FromInt(targetY - sourceY)

	if dx == 0 && dy == 0 {
		dx = vmath.Scale // Fallback rightwards to prevent stacking lock
	}

	dist := vmath.Magnitude(dx, dy)
	if dist == 0 {
		dist = 1
	}

	dirX, dirY := vmath.Normalize2D(dx, dy)

	// Weight inversely proportional to distance: (MaxDist - dist) / MaxDist
	weight := vmath.Div(rule.MaxDist-dist, rule.MaxDist)
	if weight < 0 {
		weight = 0
	}

	// Apply species-specific interaction modifier and base strength
	weight = vmath.Mul(weight, rule.WeightMult)
	accelMag := vmath.Mul(rule.Strength, weight)

	return vmath.Mul(dirX, accelMag), vmath.Mul(dirY, accelMag), true
}