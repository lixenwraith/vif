package system

import (
	"sync/atomic"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// particleBehaviorOrder preserves the former system order. Decay resolves
// before blossom even though both now share one component store and system.
var particleBehaviorOrder = [...]component.ParticleBehavior{
	component.ParticleDecay,
	component.ParticleBlossom,
}

// particleBehaviorProfile contains only the gameplay properties that genuinely
// differ between particle behaviors.
type particleBehaviorProfile struct {
	direction float64
	color     color.RGB
}

func particleProfileFor(behavior component.ParticleBehavior) (particleBehaviorProfile, bool) {
	switch behavior {
	case component.ParticleDecay:
		return particleBehaviorProfile{
			direction: 1,
			color:     visual.RgbDecay,
		}, true
	case component.ParticleBlossom:
		return particleBehaviorProfile{
			direction: -1,
			color:     visual.RgbBlossom,
		}, true
	default:
		return particleBehaviorProfile{}, false
	}
}

// particleBehaviorState keeps behavior-specific collision bookkeeping and
// diagnostics separate while all particles share their system RNG.
type particleBehaviorState struct {
	hitThisFrame       map[core.Entity]bool
	processedGridCells map[int]bool

	statCount            *atomic.Int64
	statApplied          *atomic.Int64
	statWallCollisions   *atomic.Int64
	statBoundaryHits     *atomic.Int64
	statGridSteps        *atomic.Int64
	statProtectedRejects *atomic.Int64
	buffers              bufferTelemetry
}

// ParticleSystem handles decay and blossom movement and collision rules.
type ParticleSystem struct {
	world *engine.World
	rng   *vmath.FastRand

	behavior [component.ParticleBehaviorCount]particleBehaviorState
	entities []core.Entity
	deathBuf []core.Entity

	enabled bool
}

// NewParticleSystem creates the unified particle system.
func NewParticleSystem(world *engine.World) engine.System {
	s := &ParticleSystem{
		world:    world,
		entities: make([]core.Entity, 0, 256),
		deathBuf: make([]core.Entity, 0, 128),
	}

	for _, behavior := range particleBehaviorOrder {
		state := &s.behavior[behavior]
		state.hitThisFrame = make(map[core.Entity]bool)
		state.processedGridCells = make(map[int]bool)

		name := behavior.String()
		state.statCount = world.Resources.Status.Ints.Get(name + ".count")
		state.statApplied = world.Resources.Status.Ints.Get(name + ".applied")
		state.statWallCollisions = world.Resources.Status.Ints.Get(name + ".wall_collisions")
		state.statBoundaryHits = world.Resources.Status.Ints.Get(name + ".boundary_hits")
		state.statGridSteps = world.Resources.Status.Ints.Get(name + ".grid_steps")
		state.statProtectedRejects = world.Resources.Status.Ints.Get(name + ".protected_rejects")
		state.buffers = newBufferTelemetry(world.Resources.Status, name, "hit_entities", "processed_cells")
	}

	s.Init()
	return s
}

// Init resets session state for a new game.
func (s *ParticleSystem) Init() {
	s.entities = s.entities[:0]
	s.deathBuf = s.deathBuf[:0]
	s.rng = s.world.Rand(core.DomainPlayer, s.Name())
	for _, behavior := range particleBehaviorOrder {
		state := &s.behavior[behavior]
		clear(state.hitThisFrame)
		clear(state.processedGridCells)
		state.statCount.Store(0)
		state.statApplied.Store(0)
		state.statWallCollisions.Store(0)
		state.statBoundaryHits.Store(0)
		state.statGridSteps.Store(0)
		state.statProtectedRejects.Store(0)
		state.buffers.Reset()
	}
	s.enabled = true
}

// Name returns the system's registry name.
func (s *ParticleSystem) Name() string {
	return "particle"
}

// Priority returns the system's update priority.
func (s *ParticleSystem) Priority() int {
	return parameter.PriorityParticle
}

// EventTypes returns the particle events handled by the system.
func (s *ParticleSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventParticleWave,
		event.EventParticleSpawnOne,
		event.EventParticleSpawnBatch,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

// HandleEvent resolves generic particle requests by their behavior field.
func (s *ParticleSystem) HandleEvent(ev event.GameEvent) {
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

	if !s.enabled {
		if ev.Type == event.EventParticleSpawnBatch {
			if batch, ok := ev.Payload.(*event.BatchPayload[event.ParticleSpawnEntry]); ok {
				event.ParticleBatchPool.Release(batch)
			}
		}
		return
	}

	switch ev.Type {
	case event.EventParticleWave:
		if payload, ok := ev.Payload.(*event.ParticleWavePayload); ok {
			s.spawnWave(payload.Behavior)
		}

	case event.EventParticleSpawnOne:
		if payload, ok := ev.Payload.(*event.ParticleSpawnPayload); ok {
			s.spawnOne(payload.Behavior, payload.X, payload.Y, payload.Char, payload.SkipStartCell)
		}

	case event.EventParticleSpawnBatch:
		if batch, ok := ev.Payload.(*event.BatchPayload[event.ParticleSpawnEntry]); ok {
			for i := range batch.Entries {
				entry := &batch.Entries[i]
				s.spawnOne(entry.Behavior, entry.X, entry.Y, entry.Char, entry.SkipStartCell)
			}
			event.ParticleBatchPool.Release(batch)
		}
	}
}

// Update advances decay first and blossom second, matching the update order of
// the systems they replace. A detached reusable entity list permits blossom's
// immediate removals from the now-shared component store.
func (s *ParticleSystem) Update() {
	if !s.enabled {
		return
	}

	particles := s.world.Components.Particle
	if particles.CountEntities() == 0 {
		for _, behavior := range particleBehaviorOrder {
			s.behavior[behavior].statCount.Store(0)
		}
		return
	}

	s.entities = append(s.entities[:0], particles.Entities()...)
	for _, behavior := range particleBehaviorOrder {
		s.updateBehavior(behavior)
	}

	var counts [component.ParticleBehaviorCount]int64
	for _, entity := range particles.Entities() {
		particle, ok := particles.GetComponent(entity)
		if ok && particle.Behavior.Valid() {
			counts[particle.Behavior]++
		}
	}
	for _, behavior := range particleBehaviorOrder {
		s.behavior[behavior].statCount.Store(counts[behavior])
	}
}

// spawnOne creates one decay or blossom particle at the requested position.
func (s *ParticleSystem) spawnOne(behavior component.ParticleBehavior, x, y int, char rune, skipStartCell bool) {
	profile, ok := particleProfileFor(behavior)
	if !ok {
		return
	}

	speed := parameter.ParticleMinSpeed + s.rng.Float64()*(parameter.ParticleMaxSpeed-parameter.ParticleMinSpeed)

	entity := s.world.CreateEntity(core.DomainPlayer)
	s.world.Positions.SetPosition(entity, component.PositionComponent{X: x, Y: y})

	lastX, lastY := -1, -1
	if skipStartCell {
		lastX, lastY = x, y
	}
	s.world.Components.Particle.SetComponent(entity, component.ParticleComponent{
		Behavior: behavior,
		Rune:     char,
		LastIntX: lastX,
		LastIntY: lastY,
	})

	preciseX, preciseY := vmath.Point{X: x, Y: y}.CenterF()
	s.world.Components.Kinetic.SetComponent(entity, component.KineticComponent{Kinetic: physics.Kinetic{
		PreciseX: preciseX,
		PreciseY: preciseY,
		VelY:     profile.direction * speed,
		AccelY:   profile.direction * parameter.ParticleAcceleration,
	}})

	s.world.Components.Sigil.SetComponent(entity, component.SigilComponent{
		Rune:  char,
		Color: profile.color,
	})
}

// spawnWave creates one particle per map column at the behavior's starting edge.
func (s *ParticleSystem) spawnWave(behavior component.ParticleBehavior) {
	profile, ok := particleProfileFor(behavior)
	if !ok {
		return
	}

	y := 0
	if profile.direction < 0 {
		y = s.world.Resources.Config.MapHeight - 1
	}
	for column := range s.world.Resources.Config.MapWidth {
		char := parameter.AlphanumericRunes[s.rng.Intn(len(parameter.AlphanumericRunes))]
		s.spawnOne(behavior, column, y, char, false)
	}
}

// updateBehavior integrates every particle of one behavior. Keeping one common
// traversal makes new particle behaviors cheap to add while the behavior switch
// isolates the collision rules that genuinely differ.
func (s *ParticleSystem) updateBehavior(behavior component.ParticleBehavior) {
	if _, ok := particleProfileFor(behavior); !ok {
		return
	}
	state := &s.behavior[behavior]
	clear(state.processedGridCells)
	clear(state.hitThisFrame)

	dtSec := min(s.world.Resources.Time.DeltaTime.Seconds(), parameter.MaxSimulationDeltaSeconds)
	gameWidth := s.world.Resources.Config.MapWidth
	particles := s.world.Components.Particle
	var collisionBuf [parameter.MaxEntitiesPerCell]core.Entity
	if behavior == component.ParticleDecay {
		s.deathBuf = s.deathBuf[:0]
	}

	for _, entity := range s.entities {
		particle, ok := particles.GetComponent(entity)
		if !ok || particle.Behavior != behavior {
			continue
		}
		kinetic, ok := s.world.Components.Kinetic.GetComponent(entity)
		if !ok {
			continue
		}

		oldX, oldY := kinetic.PreciseX, kinetic.PreciseY
		curX, curY := physics.Integrate(&kinetic.Kinetic, dtSec)
		destroyParticle := false

		traverser := vmath.NewGridTraverserF(oldX, oldY, kinetic.PreciseX, kinetic.PreciseY)
		for traverser.Next() {
			state.statGridSteps.Add(1)
			x, y := traverser.Pos()

			if s.world.Positions.IsOutOfBounds(x, y) {
				state.statBoundaryHits.Add(1)
				destroyParticle = true
				break
			}
			if s.world.Positions.HasBlockingWallAt(x, y, component.WallBlockParticle) {
				state.statWallCollisions.Add(1)
				destroyParticle = true
				break
			}

			if x == particle.LastIntX && y == particle.LastIntY {
				continue
			}

			flatIdx := (y * gameWidth) + x
			if state.processedGridCells[flatIdx] {
				continue
			}

			n := s.world.Positions.GetAllEntitiesAtInto(x, y, collisionBuf[:])
			targets := collisionBuf[:n]
			switch behavior {
			case component.ParticleDecay:
				destroyParticle = s.processDecayTargets(state, entity, targets)
			case component.ParticleBlossom:
				destroyParticle = s.processBlossomTargets(state, entity, targets)
			}

			state.processedGridCells[flatIdx] = true
			if destroyParticle {
				break
			}
		}

		if destroyParticle {
			// Particles have no death effect of their own, so collision, wall, and
			// boundary removal all use the same immediate path.
			s.world.DestroyEntity(entity)
			continue
		}

		if particle.LastIntX != curX || particle.LastIntY != curY {
			if s.rng.Float64() < parameter.ParticleChangeChance {
				particle.Rune = parameter.AlphanumericRunes[s.rng.Intn(len(parameter.AlphanumericRunes))]
				if sigil, ok := s.world.Components.Sigil.GetPtr(entity); ok {
					sigil.Rune = particle.Rune
				}
			}
			particle.LastIntX = curX
			particle.LastIntY = curY
		}

		s.world.Positions.SetPosition(entity, component.PositionComponent{X: curX, Y: curY})
		particles.SetComponent(entity, particle)
		s.world.Components.Kinetic.SetComponent(entity, kinetic)
	}

	if behavior == component.ParticleDecay && len(s.deathBuf) > 0 {
		event.EmitDeath(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, s.deathBuf...)
	}
	state.buffers.Observe(0, len(state.hitThisFrame))
	state.buffers.Observe(1, len(state.processedGridCells))
}

// annihilateOpposingParticle applies the one collision rule shared by decay
// and blossom: the pair consumes each other immediately and without a death
// effect. Other behavior pairings remain available for future rules.
func (s *ParticleSystem) annihilateOpposingParticle(behavior component.ParticleBehavior, target core.Entity) bool {
	other, ok := s.world.Components.Particle.GetComponent(target)
	if !ok || !((behavior == component.ParticleDecay && other.Behavior == component.ParticleBlossom) ||
		(behavior == component.ParticleBlossom && other.Behavior == component.ParticleDecay)) {
		return false
	}
	s.world.DestroyEntity(target)
	return true
}

func (s *ParticleSystem) processDecayTargets(state *particleBehaviorState, particleEntity core.Entity, targets []core.Entity) bool {
	for _, target := range targets {
		if target == 0 || target == particleEntity || state.hitThisFrame[target] {
			continue
		}
		if s.annihilateOpposingParticle(component.ParticleDecay, target) {
			return true
		}

		if s.world.Components.Nugget.HasEntity(target) {
			s.world.PushLocal(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{Entity: target})
			event.EmitDeath(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, target)
		} else {
			// Glyph mechanics are Player-domain; a Shared glyph is a gold member.
			if target.Domain() != core.DomainPlayer {
				continue
			}
			if s.shouldDieByDecay(target) {
				s.deathBuf = append(s.deathBuf, target)
			} else {
				s.applyDecayToCharacter(state, target)
			}
		}

		state.hitThisFrame[target] = true
	}
	return false
}

// processBlossomTargets returns true when the current blossom is consumed.
func (s *ParticleSystem) processBlossomTargets(state *particleBehaviorState, particleEntity core.Entity, targets []core.Entity) bool {
	for _, target := range targets {
		if target == 0 || target == particleEntity {
			continue
		}
		// Glyph mechanics are Player-domain; a Shared glyph is a gold member.
		if target.Domain() != core.DomainPlayer || state.hitThisFrame[target] {
			continue
		}
		if s.annihilateOpposingParticle(component.ParticleBlossom, target) {
			return true
		}

		destroy := s.applyBlossomToCharacter(state, target)
		state.hitThisFrame[target] = true
		if destroy {
			return true
		}
	}
	return false
}

func (s *ParticleSystem) shouldDieByDecay(entity core.Entity) bool {
	glyph, ok := s.world.Components.Glyph.GetComponent(entity)
	return ok && glyph.Level == component.GlyphDark && glyph.Type == component.GlyphRed
}

func (s *ParticleSystem) applyDecayToCharacter(state *particleBehaviorState, entity core.Entity) {
	glyph, ok := s.world.Components.Glyph.GetPtr(entity)
	if !ok {
		return
	}
	if protection, ok := s.world.Components.Protection.GetComponent(entity); ok {
		if protection.Mask&component.ProtectFromParticle != 0 {
			state.statProtectedRejects.Add(1)
			return
		}
	}

	if glyph.Level > component.GlyphDark {
		glyph.Level--
	} else {
		switch glyph.Type {
		case component.GlyphBlue:
			glyph.Type = component.GlyphGreen
			glyph.Level = component.GlyphBright
		case component.GlyphGreen:
			glyph.Type = component.GlyphRed
			glyph.Level = component.GlyphBright
		default:
			event.EmitDeath(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, entity)
		}
	}
	state.statApplied.Add(1)
}

func (s *ParticleSystem) applyBlossomToCharacter(state *particleBehaviorState, entity core.Entity) bool {
	glyph, ok := s.world.Components.Glyph.GetPtr(entity)
	if !ok {
		return false
	}
	if protection, ok := s.world.Components.Protection.GetComponent(entity); ok {
		if protection.Mask&component.ProtectFromParticle != 0 {
			state.statProtectedRejects.Add(1)
			return false
		}
	}

	if glyph.Type == component.GlyphRed {
		return true
	}
	if glyph.Level < component.GlyphBright {
		glyph.Level++
		state.statApplied.Add(1)
	}
	return false
}
