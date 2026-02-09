package system

import (
	"math/rand"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/physics"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// DecaySystem handles character decay animation and logic
type DecaySystem struct {
	world *engine.World

	// Per-frame tracking
	decayedThisFrame   map[core.Entity]bool
	processedGridCells map[int]bool // Key is flat index: (y * gameWidth) + x

	statCount   *atomic.Int64
	statApplied *atomic.Int64

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

	s.Init()
	return s
}

// Init resets session state for new game
func (s *DecaySystem) Init() {
	clear(s.decayedThisFrame)
	clear(s.processedGridCells)
	s.statCount.Store(0)
	s.statApplied.Store(0)
	s.enabled = true
}

// Name returns system's name
func (s *DecaySystem) Name() string {
	return "decay"
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
		event.EventGameReset,
	}
}

// HandleEvent processes decay-related events
func (s *DecaySystem) HandleEvent(ev event.GameEvent) {
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
	// Note: Speed is converted to Q32.32. Decay moves DOWN by default, so velocity is positive
	speedFloat := parameter.ParticleMinSpeed + rand.Float64()*(parameter.ParticleMaxSpeed-parameter.ParticleMinSpeed)
	velY := vmath.FromFloat(speedFloat)
	accelY := vmath.FromFloat(parameter.ParticleAcceleration)

	entity := s.world.CreateEntity()

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

	kinetic := core.Kinetic{
		PreciseX: vmath.FromInt(x),
		PreciseY: vmath.FromInt(y),
		VelY:     velY,
		AccelY:   accelY,
	}
	kineticComp := component.KineticComponent{kinetic}
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
	for column := 0; column < gameWidth; column++ {
		char := parameter.AlphanumericRunes[rand.Intn(len(parameter.AlphanumericRunes))]
		s.spawnSingleDecay(column, 0, char, false)
	}
}

// updateDecayEntities updates entity positions and applies decay
func (s *DecaySystem) updateDecayEntities() {
	dtFixed := vmath.FromFloat(s.world.Resources.Time.DeltaTime.Seconds())
	// Cap delta time to prevent tunneling on lag spikes
	dtCap := vmath.FromFloat(0.1)
	if dtFixed > dtCap {
		dtFixed = dtCap
	}

	gameWidth := s.world.Resources.Config.MapWidth

	decayEntities := s.world.Components.Decay.GetAllEntities()

	// Clear frame deduplication maps
	clear(s.processedGridCells)
	clear(s.decayedThisFrame)

	// Local buffers
	var deathCandidates []core.Entity
	var collisionBuf [parameter.MaxEntitiesPerCell]core.Entity

	for _, entity := range decayEntities {
		decayComp, ok := s.world.Components.Decay.GetComponent(entity)
		if !ok {
			continue
		}
		kineticComp, ok := s.world.Components.Kinetic.GetComponent(entity)
		if !ok {
			continue
		}

		oldX, oldY := kineticComp.PreciseX, kineticComp.PreciseY
		// Physics Integration (Fixed Point)
		curX, curY := physics.Integrate(&kineticComp.Kinetic, dtFixed)

		destroyEntity := false

		// Swept Traversal via Supercover DDA
		vmath.Traverse(oldX, oldY, kineticComp.PreciseX, kineticComp.PreciseY, func(x, y int) bool {
			// Wall or OOB - destroy particle
			if s.world.Positions.IsBlocked(x, y, component.WallBlockSpawn) {
				destroyEntity = true
				return false
			}

			// Skip cell from previous frame (already processed)
			if x == decayComp.LastIntX && y == decayComp.LastIntY {
				return true
			}

			flatIdx := (y * gameWidth) + x
			if s.processedGridCells[flatIdx] {
				return true
			}

			n := s.world.Positions.GetAllEntitiesAtInto(x, y, collisionBuf[:])
			for i := 0; i < n; i++ {
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
					event.EmitDeathOne(s.world.Resources.Event.Queue, target, 0)
					event.EmitDeathOne(s.world.Resources.Event.Queue, entity, 0)
					break
				}

				if s.world.Components.Nugget.HasEntity(target) {
					s.world.PushEvent(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{Entity: target})
					event.EmitDeathOne(s.world.Resources.Event.Queue, target, event.EventFlashSpawnOneRequest)
				} else if s.shouldDieByDecay(target) {
					deathCandidates = append(deathCandidates, target)
				} else {
					s.applyDecayToCharacter(target)
				}

				s.decayedThisFrame[target] = true
			}

			s.processedGridCells[flatIdx] = true
			return true
		})

		if destroyEntity {
			event.EmitDeathOne(s.world.Resources.Event.Queue, entity, 0)
			continue
		}

		// 2D Matrix Visual Effect: Update character on ANY cell entry
		if decayComp.LastIntX != curX || decayComp.LastIntY != curY {
			if rand.Float64() < parameter.ParticleChangeChance {
				decayComp.Rune = parameter.AlphanumericRunes[rand.Intn(len(parameter.AlphanumericRunes))]
				if sigil, ok := s.world.Components.Sigil.GetComponent(entity); ok {
					sigil.Rune = decayComp.Rune
					s.world.Components.Sigil.SetComponent(entity, sigil)
				}
			}
			decayComp.LastIntX = curX
			decayComp.LastIntY = curY
		}

		// Grid Sync: Update Positions for spatial queries
		s.world.Positions.SetPosition(entity, component.PositionComponent{X: curX, Y: curY})
		s.world.Components.Decay.SetComponent(entity, decayComp)
		s.world.Components.Kinetic.SetComponent(entity, kineticComp)
	}

	// Emit single batch event instead of scalar events per hit
	if len(deathCandidates) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, deathCandidates)
	}
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
	glyphComp, ok := s.world.Components.Glyph.GetComponent(entity)
	if !ok {
		return
	}

	// Check protection
	if protComp, ok := s.world.Components.Protection.GetComponent(entity); ok {
		if protComp.Mask.Has(component.ProtectFromDecay) {
			return
		}
	}

	// Apply decay logic
	if glyphComp.Level > component.GlyphDark {
		// Decrease level if not level dark
		glyphComp.Level--
		s.world.Components.Glyph.SetComponent(entity, glyphComp)
	} else {
		// Dark level: type chain Blue→Green→Red→destroy
		switch glyphComp.Type {
		case component.GlyphBlue:
			glyphComp.Type = component.GlyphGreen
			glyphComp.Level = component.GlyphBright
			s.world.Components.Glyph.SetComponent(entity, glyphComp)

		case component.GlyphGreen:
			glyphComp.Type = component.GlyphRed
			glyphComp.Level = component.GlyphBright
			s.world.Components.Glyph.SetComponent(entity, glyphComp)

		default:
			// Fallback: Red or other: destroy
			event.EmitDeathOne(s.world.Resources.Event.Queue, entity, event.EventFlashSpawnOneRequest)
		}
	}

	s.statApplied.Add(1)
}