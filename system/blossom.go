package system

import (
	"math/rand"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// BlossomSystem handles blossom entity movement and collision logic
type BlossomSystem struct {
	world *engine.World

	// Per-frame tracking
	blossomedThisFrame map[core.Entity]bool
	processedGridCells map[int]bool // Key is flat index: (y * gameWidth) + x

	statCount   *atomic.Int64
	statApplied *atomic.Int64

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

	s.Init()
	return s
}

// Init resets session state for new game
func (s *BlossomSystem) Init() {
	clear(s.blossomedThisFrame)
	clear(s.processedGridCells)
	s.statCount.Store(0)
	s.statApplied.Store(0)
	s.enabled = true
}

// Priority returns the system's priority
func (s *BlossomSystem) Priority() int {
	return constant.PriorityBlossom
}

// EventTypes returns the event types BlossomSystem handles
func (s *BlossomSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventBlossomWave,
		event.EventBlossomSpawnOne,
		event.EventGameReset,
	}
}

// HandleEvent processes blossom-related events
func (s *BlossomSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
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
	}
}

// Update runs the blossom system logic
func (s *BlossomSystem) Update() {
	if !s.enabled {
		return
	}

	count := s.world.Components.Blossom.CountEntity()
	if count == 0 {
		s.statCount.Store(0)
		return
	}

	s.updateBlossomEntities()
	s.statCount.Store(int64(s.world.Components.Blossom.CountEntity()))
}

// spawnSingleBlossom creates one blossom entity at specified position
func (s *BlossomSystem) spawnSingleBlossom(x, y int, char rune, skipStartCell bool) {
	// Random speed between ParticleMinSpeed and ParticleMaxSpeed
	// Note: Speed is converted to Q32.32. Blossom moves UP by default, so velocity is negative
	speedFloat := constant.ParticleMinSpeed + rand.Float64()*(constant.ParticleMaxSpeed-constant.ParticleMinSpeed)
	velY := -vmath.FromFloat(speedFloat)
	accelY := -vmath.FromFloat(constant.ParticleAcceleration)

	entity := s.world.CreateEntity()

	// 1. Grid Positions
	s.world.Positions.SetPosition(entity, component.PositionComponent{X: x, Y: y})

	// 2. Physics/Logic Components
	lastX, lastY := -1, -1
	if skipStartCell {
		lastX, lastY = x, y
	}
	s.world.Components.Blossom.SetComponent(entity, component.BlossomComponent{
		KineticState: component.KineticState{
			PreciseX: vmath.FromInt(x),
			PreciseY: vmath.FromInt(y),
			VelY:     velY,
			AccelY:   accelY,
		},
		Char:     char,
		LastIntX: lastX,
		LastIntY: lastY,
	})

	// 3. Render component
	s.world.Components.Sigil.SetComponent(entity, component.SigilComponent{
		Rune:  char,
		Color: component.SigilBlossom,
	})
}

// spawnBlossomWave creates a screen-wide rising blossom wave
func (s *BlossomSystem) spawnBlossomWave() {
	gameWidth := s.world.Resources.Config.GameWidth
	gameHeight := s.world.Resources.Config.GameHeight

	// Spawn one blossom entity per column for full-width coverage
	for column := 0; column < gameWidth; column++ {
		char := constant.AlphanumericRunes[rand.Intn(len(constant.AlphanumericRunes))]
		s.spawnSingleBlossom(column, gameHeight-1, char, false)
	}
}

// updateBlossomEntities updates entity positions and applies blossom effects
func (s *BlossomSystem) updateBlossomEntities() {
	dtFixed := vmath.FromFloat(s.world.Resources.Time.DeltaTime.Seconds())
	// Cap delta time to prevent tunneling on lag spikes
	dtCap := vmath.FromFloat(0.1)
	if dtFixed > dtCap {
		dtFixed = dtCap
	}

	gameWidth := s.world.Resources.Config.GameWidth
	gameHeight := s.world.Resources.Config.GameHeight

	blossomEntities := s.world.Components.Blossom.AllEntity()

	// ClearAllComponent frame deduplication maps
	clear(s.processedGridCells)
	clear(s.blossomedThisFrame)

	var collisionBuf [constant.MaxEntitiesPerCell]core.Entity

	for _, entity := range blossomEntities {
		b, ok := s.world.Components.Blossom.GetComponent(entity)
		if !ok {
			continue
		}

		oldX, oldY := b.PreciseX, b.PreciseY
		// Physics Integration (Fixed Point)
		curX, curY := b.Integrate(dtFixed)

		// 2D Boundary Check: Destroy if entity leaves the game area in any direction
		if curX < 0 || curX >= gameWidth || curY < 0 || curY >= gameHeight {
			s.world.DestroyEntity(entity)
			continue
		}

		destroyBlossom := false
		// Swept Traversal: Check every grid cell intersected by the movement vector
		vmath.Traverse(oldX, oldY, b.PreciseX, b.PreciseY, func(x, y int) bool {
			// Bounds safety for the DDA callback
			if x < 0 || x >= gameWidth || y < 0 || y >= gameHeight {
				return true
			}

			// Skip cell from previous frame (already processed)
			if x == b.LastIntX && y == b.LastIntY {
				return true
			}

			// Global frame deduplication: skip if this cell was already processed by ANY blossom this tick
			flatIdx := (y * gameWidth) + x
			if s.processedGridCells[flatIdx] {
				return true
			}

			// Query entities at position using zero-alloc buffer
			n := s.world.Positions.GetAllEntityAtInto(x, y, collisionBuf[:])

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

				// Logic: Passthrough checks
				if s.world.Components.Nugget.HasEntity(target) {
					continue
				}
				if member, ok := s.world.Components.Member.GetComponent(target); ok {
					if header, ok := s.world.Components.Header.GetComponent(member.HeaderEntity); ok && header.BehaviorID == component.BehaviorGold {
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
			return !destroyBlossom // Stop traversal if blossom destroyed
		})

		if destroyBlossom {
			s.world.DestroyEntity(entity)
			continue
		}

		// 2D Matrix Visual Effect: Randomize character when entering ANY new cell
		if b.LastIntX != curX || b.LastIntY != curY {
			if rand.Float64() < constant.ParticleChangeChance {
				b.Char = constant.AlphanumericRunes[rand.Intn(len(constant.AlphanumericRunes))]
				// Must update the component used by the renderer
				if sigil, ok := s.world.Components.Sigil.GetComponent(entity); ok {
					sigil.Rune = b.Char
					s.world.Components.Sigil.SetComponent(entity, sigil)
				}
			}
			b.LastIntX = curX
			b.LastIntY = curY
		}

		// Grid Sync: Update Positions for spatial queries
		s.world.Positions.SetPosition(entity, component.PositionComponent{X: curX, Y: curY})
		s.world.Components.Blossom.SetComponent(entity, b)
	}
}

// applyBlossomToCharacter applies blossom effect to a glyph character
// Returns true if blossom should be destroyed (hit Red)
func (s *BlossomSystem) applyBlossomToCharacter(entity core.Entity) bool {
	glyph, ok := s.world.Components.Glyph.GetComponent(entity)
	if !ok {
		return false
	}

	// Check protection
	if prot, ok := s.world.Components.Protection.GetComponent(entity); ok {
		now := s.world.Resources.Time.GameTime
		if !prot.IsExpired(now.UnixNano()) && prot.Mask.Has(component.ProtectFromDecay) {
			return false
		}
	}

	// Red characters destroy the blossom
	if glyph.Type == component.GlyphRed {
		return true
	}

	// Increase level (inverse of decay)
	if glyph.Level < component.GlyphBright {
		glyph.Level++
		s.world.Components.Glyph.SetComponent(entity, glyph)
		s.statApplied.Add(1)
	}
	// At Bright: no effect, blossom continues

	return false
}