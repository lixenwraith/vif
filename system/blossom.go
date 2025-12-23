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
}

// Priority returns the system's priority
func (s *BlossomSystem) Priority() int {
	return constant.PriorityBlossom
}

// EventTypes returns the event types BlossomSystem handles
func (s *BlossomSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventBlossomSpawnOne,
		event.EventGameReset,
	}
}

// HandleEvent processes blossom-related events
func (s *BlossomSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventBlossomSpawnOne:
		if payload, ok := ev.Payload.(*event.BlossomSpawnPayload); ok {
			s.spawnSingleBlossom(payload.X, payload.Y, payload.Char)
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
func (s *BlossomSystem) spawnSingleBlossom(x, y int, char rune) {
	speed := constant.BlossomMinSpeed + rand.Float64()*(constant.BlossomMaxSpeed-constant.BlossomMinSpeed)

	entity := s.world.CreateEntity()

	s.world.Positions.Add(entity, component.PositionComponent{X: x, Y: y})
	s.blossomStore.Add(entity, component.BlossomComponent{
		PreciseX:      float64(x),
		PreciseY:      float64(y),
		Speed:         speed,
		Acceleration:  constant.BlossomAcceleration,
		Char:          char,
		LastChangeRow: -1,
		LastIntX:      -1,
		LastIntY:      -1,
		PrevPreciseX:  float64(x),
		PrevPreciseY:  float64(y),
	})
	s.charStore.Add(entity, component.CharacterComponent{
		Rune:  char,
		Color: component.ColorBlossom,
		Style: component.StyleNormal,
	})
}

// updateBlossomEntities updates entity positions and applies blossom effects
func (s *BlossomSystem) updateBlossomEntities() {
	dtSeconds := s.res.Time.DeltaTime.Seconds()
	gameWidth := s.res.Config.GameWidth

	if dtSeconds > 0.1 {
		dtSeconds = 0.1
	}

	blossomEntities := s.blossomStore.All()

	// Clear frame deduplication maps
	for k := range s.processedGridCells {
		delete(s.processedGridCells, k)
	}
	for k := range s.blossomedThisFrame {
		delete(s.blossomedThisFrame, k)
	}

	var collisionBuf [constant.MaxEntitiesPerCell]core.Entity

	for _, entity := range blossomEntities {
		b, ok := s.blossomStore.Get(entity)
		if !ok {
			continue
		}

		pos, hasPos := s.world.Positions.Get(entity)
		if !hasPos {
			continue
		}

		// Physics Integration: acceleration then position (upward movement)
		startY := b.PreciseY
		b.Speed += b.Acceleration * dtSeconds
		b.PreciseY -= b.Speed * dtSeconds
		b.PrevPreciseY = startY

		// Destroy if entity rises above game area
		if b.PreciseY < 0 {
			s.world.DestroyEntity(entity)
			continue
		}

		// Swept Traversal: Check all rows between previous and current position
		// Note: blossom moves upward, so currentY < startY
		y1 := int(b.PreciseY)
		y2 := int(startY)

		startRow, endRow := y1, y2
		if y1 > y2 {
			startRow, endRow = y2, y1
		}
		if startRow < 0 {
			startRow = 0
		}

		col := int(b.PreciseX)
		destroyBlossom := false

		// Check each traversed row for entity collisions
		for row := startRow; row <= endRow && !destroyBlossom; row++ {
			// Coordinate latch: skip if already processed this exact coordinate
			if col == b.LastIntX && row == b.LastIntY {
				continue
			}
			if col < 0 || col >= gameWidth {
				continue
			}

			// Frame deduplication: skip if this cell was already processed this frame
			flatIdx := (row * gameWidth) + col
			if s.processedGridCells[flatIdx] {
				continue
			}

			// Query entities at position using zero-alloc buffer
			n := s.world.Positions.GetAllAtInto(col, row, collisionBuf[:])

			// Process collisions with self-exclusion
			for i := 0; i < n && !destroyBlossom; i++ {
				targetEntity := collisionBuf[i]
				if targetEntity == 0 || targetEntity == entity {
					continue // Self-exclusion
				}

				// Entity deduplication: skip if already hit this frame
				s.mu.RLock()
				alreadyHit := s.blossomedThisFrame[targetEntity]
				s.mu.RUnlock()

				if alreadyHit {
					continue
				}

				// Decay collision: both destroyed
				if s.decayStore.Has(targetEntity) {
					s.world.DestroyEntity(targetEntity)
					destroyBlossom = true
					continue
				}

				// Skip nuggets (passthrough)
				if s.nuggetStore.Has(targetEntity) {
					continue
				}

				// Skip gold composite members (passthrough)
				if member, ok := s.memberStore.Get(targetEntity); ok {
					if header, ok := s.headerStore.Get(member.AnchorID); ok {
						if header.BehaviorID == component.BehaviorGold {
							continue
						}
					}
				}

				// Apply blossom effect to typeable characters
				killed := s.applyBlossomToCharacter(targetEntity)
				if killed {
					destroyBlossom = true
				}

				s.mu.Lock()
				s.blossomedThisFrame[targetEntity] = true
				s.mu.Unlock()
			}

			s.processedGridCells[flatIdx] = true
		}

		if destroyBlossom {
			s.world.DestroyEntity(entity)
			continue
		}

		// Coordinate Latch Update
		b.LastIntX = col
		b.LastIntY = int(b.PreciseY)

		// Visual character randomization (matrix effect)
		currentRow := int(b.PreciseY)
		if currentRow != b.LastChangeRow {
			b.LastChangeRow = currentRow
			if rand.Float64() < constant.DecayChangeChance {
				b.Char = constant.AlphanumericRunes[rand.Intn(len(constant.AlphanumericRunes))]
				// Sync CharacterComponent
				if char, ok := s.charStore.Get(entity); ok {
					char.Rune = b.Char
					s.charStore.Add(entity, char)
				}
			}
		}

		b.PrevPreciseX = b.PreciseX

		// Grid Sync Protocol: Update PositionStore if integer position changed
		newGridY := int(b.PreciseY)
		if newGridY != pos.Y {
			s.world.Positions.Add(entity, component.PositionComponent{X: pos.X, Y: newGridY})
		}

		s.blossomStore.Add(entity, b)
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
		s.typeableStore.Add(entity, typeable)

		// Sync renderer
		if hasChar {
			char.SeqLevel = typeable.Level
			s.charStore.Add(entity, char)
		}

		s.statApplied.Add(1)
	}
	// At Bright: no effect, blossom continues

	return false
}