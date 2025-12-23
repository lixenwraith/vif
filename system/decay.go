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

// DecaySystem handles character decay animation and logic
type DecaySystem struct {
	mu    sync.RWMutex
	world *engine.World
	res   engine.Resources

	decayStore    *engine.Store[component.DecayComponent]
	protStore     *engine.Store[component.ProtectionComponent]
	deathStore    *engine.Store[component.DeathComponent]
	nuggetStore   *engine.Store[component.NuggetComponent]
	charStore     *engine.Store[component.CharacterComponent]
	typeableStore *engine.Store[component.TypeableComponent]

	// Per-frame tracking
	decayedThisFrame   map[core.Entity]bool
	processedGridCells map[int]bool // Key is flat index: (y * gameWidth) + x

	statCount   *atomic.Int64
	statApplied *atomic.Int64
}

// NewDecaySystem creates a new decay system
func NewDecaySystem(world *engine.World) engine.System {
	res := engine.GetResources(world)
	s := &DecaySystem{
		world: world,
		res:   res,

		decayStore:    engine.GetStore[component.DecayComponent](world),
		protStore:     engine.GetStore[component.ProtectionComponent](world),
		deathStore:    engine.GetStore[component.DeathComponent](world),
		nuggetStore:   engine.GetStore[component.NuggetComponent](world),
		charStore:     engine.GetStore[component.CharacterComponent](world),
		typeableStore: engine.GetStore[component.TypeableComponent](world),

		decayedThisFrame:   make(map[core.Entity]bool),
		processedGridCells: make(map[int]bool),

		statCount:   res.Status.Ints.Get("decay.count"),
		statApplied: res.Status.Ints.Get("decay.applied"),
	}
	s.initLocked()
	return s
}

// Init resets session state for new game
func (s *DecaySystem) Init() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()
}

// initLocked performs session state reset, caller must hold s.mu
func (s *DecaySystem) initLocked() {
	clear(s.decayedThisFrame)
	clear(s.processedGridCells)
	s.statCount.Store(0)
	s.statApplied.Store(0)
}

// Priority returns the system's priority
func (s *DecaySystem) Priority() int {
	return constant.PriorityDecay
}

// EventTypes returns the event types DecaySystem handles
func (s *DecaySystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventDecayWave,
		event.EventDecaySpawnOne,
		event.EventGameReset,
	}
}

// HandleEvent processes decay-related events
func (s *DecaySystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventDecayWave:
		s.spawnDecayWave()

	case event.EventDecaySpawnOne:
		if payload, ok := ev.Payload.(*event.DecaySpawnPayload); ok {
			s.spawnSingleDecay(payload.X, payload.Y, payload.Char)
		}

	case event.EventGameReset:
		s.Init()
	}
}

// Update runs the decay system logic
func (s *DecaySystem) Update() {
	count := s.decayStore.Count()
	if count == 0 {
		s.statCount.Store(0)
		return
	}

	s.updateDecayEntities()
	s.statCount.Store(int64(s.decayStore.Count()))
}

// spawnSingleDecay creates one decay entity at specified position
func (s *DecaySystem) spawnSingleDecay(x, y int, char rune) {
	speedFloat := constant.DecayMinSpeed + rand.Float64()*(constant.DecayMaxSpeed-constant.DecayMinSpeed)
	velY := vmath.FromFloat(speedFloat)
	accelY := vmath.FromFloat(constant.BlossomAcceleration) // Replicated acceleration from blossom
	// TODO: its own constant

	entity := s.world.CreateEntity()

	// 1. Grid Position
	s.world.Positions.Add(entity, component.PositionComponent{X: x, Y: y})

	// 2. Physics/Logic Component
	s.decayStore.Add(entity, component.DecayComponent{
		PreciseX: vmath.FromInt(x),
		PreciseY: vmath.FromInt(y),
		VelX:     0,
		VelY:     velY,
		AccelX:   0,
		AccelY:   accelY,
		Char:     char,
		// Latch current cell to prevent immediate char randomization
		LastIntX: x,
		LastIntY: y,
	})

	// 3. Render component
	s.charStore.Add(entity, component.CharacterComponent{
		Rune:  char,
		Color: component.ColorDecay,
		Style: component.StyleNormal,
		// Type and Level not needed for decay
	})
}

// spawnDecayWave creates a screen-wide falling decay wave
func (s *DecaySystem) spawnDecayWave() {
	gameWidth := s.res.Config.GameWidth

	// Spawn one decay entity per column for full-width coverage
	for column := 0; column < gameWidth; column++ {
		char := constant.AlphanumericRunes[rand.Intn(len(constant.AlphanumericRunes))]
		s.spawnSingleDecay(column, 0, char)
	}
}

// updateDecayEntities updates entity positions and applies decay
func (s *DecaySystem) updateDecayEntities() {
	dtFixed := vmath.FromFloat(s.res.Time.DeltaTime.Seconds())
	gameWidth := s.res.Config.GameWidth
	gameHeight := s.res.Config.GameHeight

	decayEntities := s.decayStore.All()

	// Clear frame deduplication maps
	clear(s.processedGridCells)
	clear(s.decayedThisFrame)

	var collisionBuf [constant.MaxEntitiesPerCell]core.Entity

	for _, entity := range decayEntities {
		d, ok := s.decayStore.Get(entity)
		if !ok {
			continue
		}

		oldX, oldY := d.PreciseX, d.PreciseY

		// Physics Integration (Fixed Point)
		d.VelX += vmath.Mul(d.AccelX, dtFixed)
		d.VelY += vmath.Mul(d.AccelY, dtFixed)
		d.PreciseX += vmath.Mul(d.VelX, dtFixed)
		d.PreciseY += vmath.Mul(d.VelY, dtFixed)

		curX, curY := vmath.ToInt(d.PreciseX), vmath.ToInt(d.PreciseY)

		// 2D Boundary Check
		if curX < 0 || curX >= gameWidth || curY < 0 || curY >= gameHeight {
			s.world.DestroyEntity(entity)
			continue
		}

		// Swept Traversal via Supercover DDA
		vmath.Traverse(oldX, oldY, d.PreciseX, d.PreciseY, func(x, y int) bool {
			if x < 0 || x >= gameWidth || y < 0 || y >= gameHeight {
				return true
			}

			if x == d.LastIntX && y == d.LastIntY {
				return true
			}

			flatIdx := (y * gameWidth) + x
			if s.processedGridCells[flatIdx] {
				return true
			}

			n := s.world.Positions.GetAllAtInto(x, y, collisionBuf[:])
			for i := 0; i < n; i++ {
				target := collisionBuf[i]
				if target == 0 || target == entity {
					continue
				}

				s.mu.RLock()
				alreadyHit := s.decayedThisFrame[target]
				s.mu.RUnlock()
				if alreadyHit {
					continue
				}

				if s.nuggetStore.Has(target) {
					s.world.PushEvent(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{Entity: target})
					event.EmitDeathOne(s.res.Events.Queue, target, event.EventFlashRequest, s.res.Time.FrameNumber)
				} else {
					s.applyDecayToCharacter(target)
				}

				s.mu.Lock()
				s.decayedThisFrame[target] = true
				s.mu.Unlock()
			}

			s.processedGridCells[flatIdx] = true
			return true
		})

		// 2D Matrix Visual Effect: Update character on ANY cell entry
		if d.LastIntX != curX || d.LastIntY != curY {
			if rand.Float64() < constant.DecayChangeChance {
				d.Char = constant.AlphanumericRunes[rand.Intn(len(constant.AlphanumericRunes))]
				// Must update the component used by the renderer
				if charComp, ok := s.charStore.Get(entity); ok {
					charComp.Rune = d.Char
					s.charStore.Add(entity, charComp)
				}
			}
			d.LastIntX = curX
			d.LastIntY = curY
		}

		// Grid Sync: Update PositionStore for spatial queries
		s.world.Positions.Add(entity, component.PositionComponent{X: curX, Y: curY})
		s.decayStore.Add(entity, d)
	}
}

// applyDecayToCharacter applies decay logic to a single character entity
func (s *DecaySystem) applyDecayToCharacter(entity core.Entity) {
	typeable, ok := s.typeableStore.Get(entity)
	if !ok {
		return
	}

	// Check protection
	if prot, ok := s.protStore.Get(entity); ok {
		now := s.res.Time.GameTime
		if !prot.IsExpired(now.UnixNano()) && prot.Mask.Has(component.ProtectFromDecay) {
			return
		}
	}

	// Get character component for renderer sync
	char, hasChar := s.charStore.Get(entity)

	// Apply decay logic
	if typeable.Level > component.LevelDark {
		// Reduce level by 1
		typeable.Level--
		s.typeableStore.Add(entity, typeable)

		// Sync renderer
		if hasChar {
			char.Level = typeable.Level
			s.charStore.Add(entity, char)
		}
	} else {
		// Dark level: type chain Blue→Green→Red→destroy
		switch typeable.Type {
		case component.TypeBlue:
			typeable.Type = component.TypeGreen
			typeable.Level = component.LevelBright
			s.typeableStore.Add(entity, typeable)
			if hasChar {
				char.Type = component.CharacterGreen
				char.Level = component.LevelBright
				s.charStore.Add(entity, char)
			}

		case component.TypeGreen:
			typeable.Type = component.TypeRed
			typeable.Level = component.LevelBright
			s.typeableStore.Add(entity, typeable)
			if hasChar {
				char.Type = component.CharacterRed
				char.Level = component.LevelBright
				s.charStore.Add(entity, char)
			}

		default:
			// Red or other: destroy
			event.EmitDeathOne(s.res.Events.Queue, entity, event.EventFlashRequest, s.res.Time.FrameNumber)
		}
	}

	s.statApplied.Add(1)
}