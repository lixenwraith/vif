package system

import (
	"math/rand"
	"sync"
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
	mu    sync.RWMutex
	world *engine.World
	res   engine.Resources

	blossomStore  *engine.Store[component.BlossomComponent]
	decayStore    *engine.Store[component.DecayComponent]
	protStore     *engine.Store[component.ProtectionComponent]
	deathStore    *engine.Store[component.DeathComponent]
	nuggetStore   *engine.Store[component.NuggetComponent]
	charStore     *engine.Store[component.CharacterComponent]
	typeableStore *engine.Store[component.TypeableComponent]
	memberStore   *engine.Store[component.MemberComponent]
	headerStore   *engine.Store[component.CompositeHeaderComponent]

	// Per-frame tracking
	blossomedThisFrame map[core.Entity]bool
	processedGridCells map[int]bool // Key is flat index: (y * gameWidth) + x

	statCount   *atomic.Int64
	statApplied *atomic.Int64

	enabled bool
}

// NewBlossomSystem creates a new blossom system
func NewBlossomSystem(world *engine.World) engine.System {
	res := engine.GetResources(world)
	s := &BlossomSystem{
		world: world,
		res:   res,

		blossomStore:  engine.GetStore[component.BlossomComponent](world),
		decayStore:    engine.GetStore[component.DecayComponent](world),
		protStore:     engine.GetStore[component.ProtectionComponent](world),
		deathStore:    engine.GetStore[component.DeathComponent](world),
		nuggetStore:   engine.GetStore[component.NuggetComponent](world),
		charStore:     engine.GetStore[component.CharacterComponent](world),
		typeableStore: engine.GetStore[component.TypeableComponent](world),
		memberStore:   engine.GetStore[component.MemberComponent](world),
		headerStore:   engine.GetStore[component.CompositeHeaderComponent](world),

		blossomedThisFrame: make(map[core.Entity]bool),
		processedGridCells: make(map[int]bool),

		statCount:   res.Status.Ints.Get("blossom.count"),
		statApplied: res.Status.Ints.Get("blossom.applied"),
	}
	s.initLocked()
	return s
}

// Init resets session state for new game
func (s *BlossomSystem) Init() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()
}

// initLocked performs session state reset, caller must hold s.mu
func (s *BlossomSystem) initLocked() {
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
	switch ev.Type {
	case event.EventBlossomWave:
		s.spawnBlossomWave()
	case event.EventBlossomSpawnOne:
		if payload, ok := ev.Payload.(*event.BlossomSpawnPayload); ok {
			s.spawnSingleBlossom(payload.X, payload.Y, payload.Char, payload.SkipStartCell)
		}

	case event.EventGameReset:
		s.Init()
	}
}

// Update runs the blossom system logic
func (s *BlossomSystem) Update() {
	count := s.blossomStore.Count()
	if count == 0 {
		s.statCount.Store(0)
		return
	}

	s.updateBlossomEntities()
	s.statCount.Store(int64(s.blossomStore.Count()))
}

// spawnSingleBlossom creates one blossom entity at specified position
func (s *BlossomSystem) spawnSingleBlossom(x, y int, char rune, skipStartCell bool) {
	// Random speed between ParticleMinSpeed and ParticleMaxSpeed
	// Note: Speed is converted to Q16.16. Blossom moves UP by default, so velocity is negative
	speedFloat := constant.ParticleMinSpeed + rand.Float64()*(constant.ParticleMaxSpeed-constant.ParticleMinSpeed)
	velY := -vmath.FromFloat(speedFloat)
	accelY := -vmath.FromFloat(constant.ParticleAcceleration)

	entity := s.world.CreateEntity()

	// 1. Grid Position
	s.world.Positions.Set(entity, component.PositionComponent{X: x, Y: y})

	// 2. Physics/Logic Component
	lastX, lastY := -1, -1
	if skipStartCell {
		lastX, lastY = x, y
	}
	s.blossomStore.Set(entity, component.BlossomComponent{
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
	s.charStore.Set(entity, component.CharacterComponent{
		Rune:  char,
		Color: component.ColorBlossom,
		Style: component.StyleNormal,
		// Type and Level not needed for blossom
	})
}

// spawnBlossomWave creates a screen-wide rising blossom wave
func (s *BlossomSystem) spawnBlossomWave() {
	gameWidth := s.res.Config.GameWidth
	gameHeight := s.res.Config.GameHeight

	// Spawn one blossom entity per column for full-width coverage
	for column := 0; column < gameWidth; column++ {
		char := constant.AlphanumericRunes[rand.Intn(len(constant.AlphanumericRunes))]
		s.spawnSingleBlossom(column, gameHeight-1, char, false)
	}
}

// updateBlossomEntities updates entity positions and applies blossom effects
func (s *BlossomSystem) updateBlossomEntities() {
	dtFixed := vmath.FromFloat(s.res.Time.DeltaTime.Seconds())
	// Cap delta time to prevent tunneling on lag spikes
	dtCap := vmath.FromFloat(0.1)
	if dtFixed > dtCap {
		dtFixed = dtCap
	}

	gameWidth := s.res.Config.GameWidth
	gameHeight := s.res.Config.GameHeight

	blossomEntities := s.blossomStore.All()

	// Clear frame deduplication maps
	clear(s.processedGridCells)
	clear(s.blossomedThisFrame)

	var collisionBuf [constant.MaxEntitiesPerCell]core.Entity

	for _, entity := range blossomEntities {
		b, ok := s.blossomStore.Get(entity)
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
			n := s.world.Positions.GetAllAtInto(x, y, collisionBuf[:])

			for i := 0; i < n && !destroyBlossom; i++ {
				target := collisionBuf[i]
				if target == 0 || target == entity {
					continue
				}

				// Entity deduplication: ensure one blossom effect per target per tick
				s.mu.RLock()
				alreadyHit := s.blossomedThisFrame[target]
				s.mu.RUnlock()
				if alreadyHit {
					continue
				}

				// Logic: Blossom vs Decay collision
				if s.decayStore.Has(target) {
					s.world.DestroyEntity(target)
					destroyBlossom = true
					continue
				}

				// Logic: Passthrough checks
				if s.nuggetStore.Has(target) {
					continue
				}
				if member, ok := s.memberStore.Get(target); ok {
					if header, ok := s.headerStore.Get(member.AnchorID); ok && header.BehaviorID == component.BehaviorGold {
						continue
					}
				}

				// Apply effect
				if s.applyBlossomToCharacter(target) {
					destroyBlossom = true
				}

				s.mu.Lock()
				s.blossomedThisFrame[target] = true
				s.mu.Unlock()
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
				if char, ok := s.charStore.Get(entity); ok {
					char.Rune = b.Char
					s.charStore.Set(entity, char)
				}
			}
			b.LastIntX = curX
			b.LastIntY = curY
		}

		// Grid Sync: Update PositionStore for spatial queries
		s.world.Positions.Set(entity, component.PositionComponent{X: curX, Y: curY})
		s.blossomStore.Set(entity, b)
	}
}

// applyBlossomToCharacter applies blossom effect to a typeable character
// Returns true if blossom should be destroyed (hit Red)
func (s *BlossomSystem) applyBlossomToCharacter(entity core.Entity) bool {
	typeable, ok := s.typeableStore.Get(entity)
	if !ok {
		return false
	}

	// Check protection
	if prot, ok := s.protStore.Get(entity); ok {
		now := s.res.Time.GameTime
		if !prot.IsExpired(now.UnixNano()) && prot.Mask.Has(component.ProtectFromDecay) {
			return false
		}
	}

	// Red characters destroy the blossom
	if typeable.Type == component.TypeRed {
		return true
	}

	// Get character component for renderer sync
	char, hasChar := s.charStore.Get(entity)

	// Increase level (inverse of decay)
	if typeable.Level < component.LevelBright {
		typeable.Level++
		s.typeableStore.Set(entity, typeable)

		// Sync renderer
		if hasChar {
			char.Level = typeable.Level
			s.charStore.Set(entity, char)
		}

		s.statApplied.Add(1)
	}
	// At Bright: no effect, blossom continues

	return false
}