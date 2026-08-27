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

// BlossomSystem handles blossom entity movement and collision logic
type BlossomSystem struct {
	world *engine.World

	rng *vmath.FastRand

	// Per-frame tracking
	blossomedThisFrame map[core.Entity]bool
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

// NewBlossomSystem creates a new blossom system
func NewBlossomSystem(world *engine.World) engine.System {
	s := &BlossomSystem{
		world: world,
	}

	s.blossomedThisFrame = make(map[core.Entity]bool)
	s.processedGridCells = make(map[int]bool)

	s.statCount = s.world.Resources.Status.Ints.Get("blossom.count")
	s.statApplied = s.world.Resources.Status.Ints.Get("blossom.applied")
	s.statWallCollisions = s.world.Resources.Status.Ints.Get("blossom.wall_collisions")
	s.statBoundaryHits = s.world.Resources.Status.Ints.Get("blossom.boundary_hits")
	s.statGridSteps = s.world.Resources.Status.Ints.Get("blossom.grid_steps")
	s.statProtectedRejects = s.world.Resources.Status.Ints.Get("blossom.protected_rejects")
	s.buffers = newBufferTelemetry(s.world.Resources.Status, "blossom", "hit_entities", "processed_cells")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *BlossomSystem) Init() {
	s.rng = s.world.Rand(core.DomainPlayer, s.Name())
	clear(s.blossomedThisFrame)
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
func (s *BlossomSystem) Name() string {
	return "blossom"
}

// Domain reports player: it draws the player stream and creates player entities.
func (s *BlossomSystem) Domain() engine.SystemDomain { return engine.SystemPlayer }

// Blossoms are requested on death and idle without it.
func (s *BlossomSystem) Requires() engine.SystemDependencies {
	return engine.Optional("death")
}

// Priority returns the system's priority
func (s *BlossomSystem) Priority() int {
	return parameter.PriorityBlossom
}

// EventTypes returns the event types BlossomSystem handles
func (s *BlossomSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventBlossomWave,
		event.EventBlossomSpawnOne,
		event.EventBlossomSpawnBatch,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

// HandleEvent processes blossom-related events
func (s *BlossomSystem) HandleEvent(ev event.GameEvent) {
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
	case event.EventBlossomWave:
		s.spawnBlossomWave()

	case event.EventBlossomSpawnOne:
		if payload, ok := ev.Payload.(*event.BlossomSpawnPayload); ok {
			s.spawnSingleBlossom(payload.X, payload.Y, payload.Char, payload.SkipStartCell)
		}

	case event.EventBlossomSpawnBatch:
		if batch, ok := ev.Payload.(*event.BatchPayload[event.BlossomSpawnEntry]); ok {
			for i := range batch.Entries {
				e := &batch.Entries[i]
				s.spawnSingleBlossom(e.X, e.Y, e.Char, e.SkipStartCell)
			}
			event.BlossomBatchPool.Release(batch)
		}
	}
}

// Update runs the blossom system logic
func (s *BlossomSystem) Update() {
	if !s.enabled {
		return
	}

	count := s.world.Components.Blossom.CountEntities()
	if count == 0 {
		s.statCount.Store(0)
		return
	}

	s.updateBlossomEntities()
	s.statCount.Store(int64(s.world.Components.Blossom.CountEntities()))
}

// spawnSingleBlossom creates one blossom entity at specified position
func (s *BlossomSystem) spawnSingleBlossom(x, y int, char rune, skipStartCell bool) {
	// Random speed between ParticleMinSpeed and ParticleMaxSpeed
	// Blossom moves UP by default, so velocity is negative
	speed := parameter.ParticleMinSpeed + s.rng.Float64()*(parameter.ParticleMaxSpeed-parameter.ParticleMinSpeed)
	velY := -speed
	accelY := -parameter.ParticleAcceleration

	entity := s.world.CreateEntity(core.DomainPlayer)

	// 1. Grid Positions
	s.world.Positions.SetPosition(entity, component.PositionComponent{X: x, Y: y})

	// 2. Physics/Logic Components
	lastX, lastY := -1, -1
	if skipStartCell {
		lastX, lastY = x, y
	}
	s.world.Components.Blossom.SetComponent(entity, component.BlossomComponent{
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

	// 3. Render component
	s.world.Components.Sigil.SetComponent(entity, component.SigilComponent{
		Rune:  char,
		Color: visual.RgbBlossom,
	})
}

// spawnBlossomWave creates a screen-wide rising blossom wave
func (s *BlossomSystem) spawnBlossomWave() {
	gameWidth := s.world.Resources.Config.MapWidth
	gameHeight := s.world.Resources.Config.MapHeight

	// Spawn one blossom entity per column for full-width coverage
	for column := range gameWidth {
		char := parameter.AlphanumericRunes[s.rng.Intn(len(parameter.AlphanumericRunes))]
		s.spawnSingleBlossom(column, gameHeight-1, char, false)
	}
}

// updateBlossomEntities updates entity positions and applies blossom effects
func (s *BlossomSystem) updateBlossomEntities() {
	// Cap delta time to prevent tunneling on lag spikes
	dtSec := min(s.world.Resources.Time.DeltaTime.Seconds(), 0.1)

	gameWidth := s.world.Resources.Config.MapWidth

	// Blossoms and collided decay entities are destroyed during traversal. A
	// detached entity/component snapshot avoids invalidating either dense store.
	blossomEntities := s.world.Components.Blossom.GetAllEntities()

	// Clear frame deduplication maps
	clear(s.processedGridCells)
	clear(s.blossomedThisFrame)

	var collisionBuf [parameter.MaxEntitiesPerCell]core.Entity

	for _, entity := range blossomEntities {
		blossomComp, ok := s.world.Components.Blossom.GetComponent(entity)
		if !ok {
			continue
		}
		kineticComp, ok := s.world.Components.Kinetic.GetComponent(entity)
		if !ok {
			continue
		}

		oldX, oldY := kineticComp.PreciseX, kineticComp.PreciseY
		curX, curY := physics.Integrate(&kineticComp.Kinetic, dtSec)

		destroyBlossom := false
		// Swept Traversal: Check every grid cell intersected by the movement vector
		traverser := vmath.NewGridTraverserF(oldX, oldY, kineticComp.PreciseX, kineticComp.PreciseY)
		for traverser.Next() {
			s.statGridSteps.Add(1)
			x, y := traverser.Pos()

			// Wall or OOB - destroy particle
			if s.world.Positions.IsOutOfBounds(x, y) {
				s.statBoundaryHits.Add(1)
				destroyBlossom = true
				break
			}
			if s.world.Positions.HasBlockingWallAt(x, y, component.WallBlockParticle) {
				s.statWallCollisions.Add(1)
				destroyBlossom = true
				break
			}

			// Skip cell from previous frame (already processed)
			if x == blossomComp.LastIntX && y == blossomComp.LastIntY {
				continue
			}

			// Global frame deduplication: skip if this cell was already processed by ANY blossom this tick
			flatIdx := (y * gameWidth) + x
			if s.processedGridCells[flatIdx] {
				continue
			}

			// Query entities at position using zero-alloc buffer
			n := s.world.Positions.GetAllEntitiesAtInto(x, y, collisionBuf[:])

			for i := 0; i < n && !destroyBlossom; i++ {
				target := collisionBuf[i]
				if target == 0 || target == entity {
					continue
				}

				// Entity deduplication: ensure one blossom effect per target per tick
				alreadyHit := s.blossomedThisFrame[target]
				if alreadyHit {
					continue
				}

				// Logic: Blossom vs Decay collision
				if s.world.Components.Decay.HasEntity(target) {
					s.world.DestroyEntity(target)
					destroyBlossom = true
					continue
				}

				// TODO: change it so only checks if glyph of red/green/blue and continue on the rest instead of reverse filtering
				// Logic: Passthrough checks
				if s.world.Components.Nugget.HasEntity(target) {
					continue
				}
				if member, ok := s.world.Components.Member.GetComponent(target); ok {
					if header, ok := s.world.Components.Header.GetComponent(member.HeaderEntity); ok && header.Behavior == component.BehaviorGold {
						continue
					}
				}

				// Apply effect
				if s.applyBlossomToCharacter(target) {
					destroyBlossom = true
				}

				s.blossomedThisFrame[target] = true
			}

			s.processedGridCells[flatIdx] = true
			if destroyBlossom {
				break
			}
		}

		if destroyBlossom {
			s.world.DestroyEntity(entity)
			continue
		}

		// 2D Matrix Visual Effect: Randomize character when entering ANY new cell
		if blossomComp.LastIntX != curX || blossomComp.LastIntY != curY {
			if s.rng.Float64() < parameter.ParticleChangeChance {
				blossomComp.Rune = parameter.AlphanumericRunes[s.rng.Intn(len(parameter.AlphanumericRunes))]
				// Must update the component used by the renderer
				if sigil, ok := s.world.Components.Sigil.GetPtr(entity); ok {
					sigil.Rune = blossomComp.Rune
				}
			}
			blossomComp.LastIntX = curX
			blossomComp.LastIntY = curY
		}

		// Grid Sync: Update Positions for spatial queries
		s.world.Positions.SetPosition(entity, component.PositionComponent{X: curX, Y: curY})
		s.world.Components.Blossom.SetComponent(entity, blossomComp)
		s.world.Components.Kinetic.SetComponent(entity, kineticComp)
	}
	s.buffers.Observe(0, len(s.blossomedThisFrame))
	s.buffers.Observe(1, len(s.processedGridCells))
}

// TODO: check if this can be refactored
// applyBlossomToCharacter applies blossom effect to a glyph character, returns true if blossom should be destroyed (hit Red)
func (s *BlossomSystem) applyBlossomToCharacter(entity core.Entity) bool {
	glyphComp, ok := s.world.Components.Glyph.GetPtr(entity)
	if !ok {
		return false
	}

	// Check protection
	if protComp, ok := s.world.Components.Protection.GetComponent(entity); ok {
		if protComp.Mask&component.ProtectFromDecay != 0 {
			s.statProtectedRejects.Add(1)
			return false
		}
	}

	// Red characters destroy the blossom
	if glyphComp.Type == component.GlyphRed {
		return true
	}

	// Increase level (inverse of decay)
	if glyphComp.Level < component.GlyphBright {
		glyphComp.Level++
		s.statApplied.Add(1)
	}
	// At Bright: no effect, blossom continues

	return false
}
