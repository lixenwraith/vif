package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// DecaySystem handles character decay animation and logic
type DecaySystem struct {
	world *engine.World

	rng *vmath.FastRand

	// Per-frame tracking
	decayedThisFrame   map[core.Entity]bool
	processedGridCells map[int]bool // Key is flat index: (y * gameWidth) + x

	statCount            *atomic.Int64
	statApplied          *atomic.Int64
	statWallCollisions   *atomic.Int64
	statBoundaryHits     *atomic.Int64
	statGridSteps        *atomic.Int64
	statProtectedRejects *atomic.Int64
	buffers              bufferTelemetry

	enabled bool
}

// NewDecaySystem creates a new decay system
func NewDecaySystem(world *engine.World) engine.System {
	s := &DecaySystem{
		world: world,
	}

	s.decayedThisFrame = make(map[core.Entity]bool)
	s.processedGridCells = make(map[int]bool)

	s.statCount = s.world.Resources.Status.Ints.Get("decay.count")
	s.statApplied = s.world.Resources.Status.Ints.Get("decay.applied")
	s.statWallCollisions = s.world.Resources.Status.Ints.Get("decay.wall_collisions")
	s.statBoundaryHits = s.world.Resources.Status.Ints.Get("decay.boundary_hits")
	s.statGridSteps = s.world.Resources.Status.Ints.Get("decay.grid_steps")
	s.statProtectedRejects = s.world.Resources.Status.Ints.Get("decay.protected_rejects")
	s.buffers = newBufferTelemetry(s.world.Resources.Status, "decay", "hit_entities", "processed_cells")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *DecaySystem) Init() {
	s.rng = s.world.Rand(core.DomainPlayer, s.Name())
	clear(s.decayedThisFrame)
	clear(s.processedGridCells)
	s.statCount.Store(0)
	s.statApplied.Store(0)
	s.statWallCollisions.Store(0)
	s.statBoundaryHits.Store(0)
	s.statGridSteps.Store(0)
	s.statProtectedRejects.Store(0)
	s.buffers.Reset()
	s.enabled = true
}

// Name returns system's name
func (s *DecaySystem) Name() string {
	return "decay"
}

// Domain reports player: it draws the player stream and creates player entities.
func (s *DecaySystem) Domain() engine.SystemDomain { return engine.SystemPlayer }

// Decay follows glyphs and deaths and idles without them.
func (s *DecaySystem) Requires() engine.SystemDependencies {
	return engine.Optional("glyph", "death")
}

// Priority returns the system's priority
func (s *DecaySystem) Priority() int {
	return parameter.PriorityDecay
}

// EventTypes returns the event types DecaySystem handles
func (s *DecaySystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventDecayWave,
		event.EventDecaySpawnOne,
		event.EventDecaySpawnBatch,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

// HandleEvent processes decay-related events
func (s *DecaySystem) HandleEvent(ev event.GameEvent) {
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
		return
	}

	switch ev.Type {
	case event.EventDecayWave:
		s.spawnDecayWave()

	case event.EventDecaySpawnOne:
		if payload, ok := ev.Payload.(*event.DecaySpawnPayload); ok {
			s.spawnSingleDecay(payload.X, payload.Y, payload.Char, payload.SkipStartCell)
		}

	case event.EventDecaySpawnBatch:
		if batch, ok := ev.Payload.(*event.BatchPayload[event.DecaySpawnEntry]); ok {
			for i := range batch.Entries {
				e := &batch.Entries[i]
				s.spawnSingleDecay(e.X, e.Y, e.Char, e.SkipStartCell)
			}
			event.DecayBatchPool.Release(batch)
		}
	}
}

// Update runs the decay system logic
func (s *DecaySystem) Update() {
	if !s.enabled {
		return
	}

	count := s.world.Components.Decay.CountEntities()
	if count == 0 {
		s.statCount.Store(0)
		return
	}

	s.updateDecayEntities()
	s.statCount.Store(int64(s.world.Components.Decay.CountEntities()))
}

// spawnSingleDecay creates one decay entity at specified position
func (s *DecaySystem) spawnSingleDecay(x, y int, char rune, skipStartCell bool) {
	// Random speed between ParticleMinSpeed and ParticleMaxSpeed
	// Decay moves DOWN by default, so velocity is positive
	speed := parameter.ParticleMinSpeed + s.rng.Float64()*(parameter.ParticleMaxSpeed-parameter.ParticleMinSpeed)
	velY := speed
	accelY := parameter.ParticleAcceleration

	entity := s.world.CreateEntity(core.DomainPlayer)

	// 1. Grid Positions
	s.world.Positions.SetPosition(entity, component.PositionComponent{X: x, Y: y})

	// 2. Physics/Logic Components
	lastX, lastY := -1, -1
	if skipStartCell {
		lastX, lastY = x, y
	}
	s.world.Components.Decay.SetComponent(entity, component.DecayComponent{
		Rune:     char,
		LastIntX: lastX,
		LastIntY: lastY,
	})

	preciseX, preciseY := vmath.Point{X: x, Y: y}.CenterF()
	kinetic := physics.Kinetic{
		PreciseX: preciseX,
		PreciseY: preciseY,
		VelY:     velY,
		AccelY:   accelY,
	}
	kineticComp := component.KineticComponent{Kinetic: kinetic}
	s.world.Components.Kinetic.SetComponent(entity, kineticComp)

	// 3. Visual component
	s.world.Components.Sigil.SetComponent(entity, component.SigilComponent{
		Rune:  char,
		Color: visual.RgbDecay,
	})
}

// spawnDecayWave creates a screen-wide falling decay wave
func (s *DecaySystem) spawnDecayWave() {
	gameWidth := s.world.Resources.Config.MapWidth

	// Spawn one decay entity per column for full-width coverage
	for column := range gameWidth {
		char := parameter.AlphanumericRunes[s.rng.Intn(len(parameter.AlphanumericRunes))]
		s.spawnSingleDecay(column, 0, char, false)
	}
}

// updateDecayEntities updates entity positions and applies decay
func (s *DecaySystem) updateDecayEntities() {
	// Cap delta time to prevent tunneling on lag spikes
	dtSec := min(s.world.Resources.Time.DeltaTime.Seconds(), 0.1)

	gameWidth := s.world.Resources.Config.MapWidth

	decays := s.world.Components.Decay

	// Clear frame deduplication maps
	clear(s.processedGridCells)
	clear(s.decayedThisFrame)

	// Local buffers
	var deathCandidates []core.Entity
	var collisionBuf [parameter.MaxEntitiesPerCell]core.Entity

	// Wall-death exits intentionally skip component write-back while destruction
	// remains queued, so movement values stay detached even though iteration is live.
	for _, entity := range decays.Entities() {
		decayComp, ok := decays.GetComponent(entity)
		if !ok {
			continue
		}
		kineticComp, ok := s.world.Components.Kinetic.GetComponent(entity)
		if !ok {
			continue
		}

		oldX, oldY := kineticComp.PreciseX, kineticComp.PreciseY
		curX, curY := physics.Integrate(&kineticComp.Kinetic, dtSec)

		destroyEntity := false

		// Swept Traversal via Supercover DDA
		traverser := vmath.NewGridTraverserF(oldX, oldY, kineticComp.PreciseX, kineticComp.PreciseY)
		for traverser.Next() {
			s.statGridSteps.Add(1)
			x, y := traverser.Pos()

			// Wall or OOB - destroy particle
			if s.world.Positions.IsOutOfBounds(x, y) {
				s.statBoundaryHits.Add(1)
				destroyEntity = true
				break
			}
			if s.world.Positions.HasBlockingWallAt(x, y, component.WallBlockSpawn) {
				s.statWallCollisions.Add(1)
				destroyEntity = true
				break
			}

			// Skip cell from previous frame (already processed)
			if x == decayComp.LastIntX && y == decayComp.LastIntY {
				continue
			}

			flatIdx := (y * gameWidth) + x
			if s.processedGridCells[flatIdx] {
				continue
			}

			n := s.world.Positions.GetAllEntitiesAtInto(x, y, collisionBuf[:])
			for i := range n {
				target := collisionBuf[i]
				if target == 0 || target == entity {
					continue
				}

				alreadyHit := s.decayedThisFrame[target]
				if alreadyHit {
					continue
				}

				// Mutual destruction: decay + blossom annihilate
				if s.world.Components.Blossom.HasEntity(target) {
					event.EmitDeath(s.world.Resources.Event.Queue, 0, target)
					event.EmitDeath(s.world.Resources.Event.Queue, 0, entity)
					break
				}

				if s.world.Components.Nugget.HasEntity(target) {
					s.world.PushEvent(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{Entity: target})
					event.EmitDeath(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, target)
				} else if s.shouldDieByDecay(target) {
					deathCandidates = append(deathCandidates, target)
				} else {
					s.applyDecayToCharacter(target)
				}

				s.decayedThisFrame[target] = true
			}

			s.processedGridCells[flatIdx] = true
		}

		if destroyEntity {
			event.EmitDeath(s.world.Resources.Event.Queue, 0, entity)
			continue
		}

		// 2D Matrix Visual Effect: Update character on ANY cell entry
		if decayComp.LastIntX != curX || decayComp.LastIntY != curY {
			if s.rng.Float64() < parameter.ParticleChangeChance {
				decayComp.Rune = parameter.AlphanumericRunes[s.rng.Intn(len(parameter.AlphanumericRunes))]
				if sigil, ok := s.world.Components.Sigil.GetPtr(entity); ok {
					sigil.Rune = decayComp.Rune
				}
			}
			decayComp.LastIntX = curX
			decayComp.LastIntY = curY
		}

		// Grid Sync: Update Positions for spatial queries
		s.world.Positions.SetPosition(entity, component.PositionComponent{X: curX, Y: curY})
		decays.SetComponent(entity, decayComp)
		s.world.Components.Kinetic.SetComponent(entity, kineticComp)
	}

	// Emit single batch event instead of scalar events per hit
	if len(deathCandidates) > 0 {
		event.EmitDeath(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, deathCandidates...)
	}
	s.buffers.Observe(0, len(s.decayedThisFrame))
	s.buffers.Observe(1, len(s.processedGridCells))
}

// shouldDieByDecay checks if a character has reached the end of the decay chain
func (s *DecaySystem) shouldDieByDecay(entity core.Entity) bool {
	glyph, ok := s.world.Components.Glyph.GetComponent(entity)
	if !ok {
		return false
	}
	return glyph.Level == component.GlyphDark && glyph.Type == component.GlyphRed
}

// applyDecayToCharacter applies decay logic to a single character entity
func (s *DecaySystem) applyDecayToCharacter(entity core.Entity) {
	glyphComp, ok := s.world.Components.Glyph.GetPtr(entity)
	if !ok {
		return
	}

	// Check protection
	if protComp, ok := s.world.Components.Protection.GetComponent(entity); ok {
		if protComp.Mask&component.ProtectFromDecay != 0 {
			s.statProtectedRejects.Add(1)
			return
		}
	}

	// Apply decay logic
	if glyphComp.Level > component.GlyphDark {
		// Decrease level if not level dark
		glyphComp.Level--
	} else {
		// Dark level: type chain Blue→Green→Red→destroy
		switch glyphComp.Type {
		case component.GlyphBlue:
			glyphComp.Type = component.GlyphGreen
			glyphComp.Level = component.GlyphBright

		case component.GlyphGreen:
			glyphComp.Type = component.GlyphRed
			glyphComp.Level = component.GlyphBright

		default:
			// Fallback: Red or other: destroy
			event.EmitDeath(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, entity)
		}
	}

	s.statApplied.Add(1)
}
