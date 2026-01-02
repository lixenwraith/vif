package system
// @lixen: #dev{feature[dust(render,system)]}

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

	decayStore   *engine.Store[component.DecayComponent]
	protStore    *engine.Store[component.ProtectionComponent]
	deathStore   *engine.Store[component.DeathComponent]
	nuggetStore  *engine.Store[component.NuggetComponent]
	sigilStore   *engine.Store[component.SigilComponent]
	glyphStore   *engine.Store[component.GlyphComponent]
	blossomStore *engine.Store[component.BlossomComponent]

	// Per-frame tracking
	decayedThisFrame   map[core.Entity]bool
	processedGridCells map[int]bool // Key is flat index: (y * gameWidth) + x

	statCount   *atomic.Int64
	statApplied *atomic.Int64

	enabled bool
}

// NewDecaySystem creates a new decay system
func NewDecaySystem(world *engine.World) engine.System {
	res := engine.GetResources(world)
	s := &DecaySystem{
		world: world,
		res:   res,

		decayStore:   engine.GetStore[component.DecayComponent](world),
		protStore:    engine.GetStore[component.ProtectionComponent](world),
		deathStore:   engine.GetStore[component.DeathComponent](world),
		nuggetStore:  engine.GetStore[component.NuggetComponent](world),
		sigilStore:   engine.GetStore[component.SigilComponent](world),
		glyphStore:   engine.GetStore[component.GlyphComponent](world),
		blossomStore: engine.GetStore[component.BlossomComponent](world),

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
	s.enabled = true
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
	if ev.Type == event.EventGameReset {
		s.Init()
		return
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
	}
}

// Update runs the decay system logic
func (s *DecaySystem) Update() {
	if !s.enabled {
		return
	}

	count := s.decayStore.Count()
	if count == 0 {
		s.statCount.Store(0)
		return
	}

	s.updateDecayEntities()
	s.statCount.Store(int64(s.decayStore.Count()))
}

// spawnSingleDecay creates one decay entity at specified position
func (s *DecaySystem) spawnSingleDecay(x, y int, char rune, skipStartCell bool) {
	// Random speed between ParticleMinSpeed and ParticleMaxSpeed
	// Note: Speed is converted to Q16.16. Decay moves DOWN by default, so velocity is positive
	speedFloat := constant.ParticleMinSpeed + rand.Float64()*(constant.ParticleMaxSpeed-constant.ParticleMinSpeed)
	velY := vmath.FromFloat(speedFloat)
	accelY := vmath.FromFloat(constant.ParticleAcceleration)

	entity := s.world.CreateEntity()

	// 1. Grid Position
	s.world.Positions.Set(entity, component.PositionComponent{X: x, Y: y})

	// 2. Physics/Logic Component
	lastX, lastY := -1, -1
	if skipStartCell {
		lastX, lastY = x, y
	}
	s.decayStore.Set(entity, component.DecayComponent{
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

	// 3. Visual component
	s.sigilStore.Set(entity, component.SigilComponent{
		Rune:  char,
		Color: component.SigilDecay,
	})
}

// spawnDecayWave creates a screen-wide falling decay wave
func (s *DecaySystem) spawnDecayWave() {
	gameWidth := s.res.Config.GameWidth

	// Spawn one decay entity per column for full-width coverage
	for column := 0; column < gameWidth; column++ {
		char := constant.AlphanumericRunes[rand.Intn(len(constant.AlphanumericRunes))]
		s.spawnSingleDecay(column, 0, char, false)
	}
}

// updateDecayEntities updates entity positions and applies decay
func (s *DecaySystem) updateDecayEntities() {
	dtFixed := vmath.FromFloat(s.res.Time.DeltaTime.Seconds())
	// Cap delta time to prevent tunneling on lag spikes
	dtCap := vmath.FromFloat(0.1)
	if dtFixed > dtCap {
		dtFixed = dtCap
	}

	gameWidth := s.res.Config.GameWidth
	gameHeight := s.res.Config.GameHeight

	decayEntities := s.decayStore.All()

	// Clear frame deduplication maps
	clear(s.processedGridCells)
	clear(s.decayedThisFrame)

	// Local buffers
	var deathCandidates []core.Entity
	var collisionBuf [constant.MaxEntitiesPerCell]core.Entity

	for _, entity := range decayEntities {
		d, ok := s.decayStore.Get(entity)
		if !ok {
			continue
		}

		oldX, oldY := d.PreciseX, d.PreciseY
		// Physics Integration (Fixed Point)
		curX, curY := d.Integrate(dtFixed)

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

			// Skip cell from previous frame (already processed)
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

				// Mutual destruction: decay + blossom annihilate
				if s.blossomStore.Has(target) {
					s.world.DestroyEntity(target)
					s.world.DestroyEntity(entity)
					break
				}

				if s.nuggetStore.Has(target) {
					s.world.PushEvent(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{Entity: target})
					event.EmitDeathOne(s.res.Events.Queue, target, event.EventFlashRequest, s.res.Time.FrameNumber)
				} else if s.shouldDieByDecay(target) {
					deathCandidates = append(deathCandidates, target)
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
			if rand.Float64() < constant.ParticleChangeChance {
				d.Char = constant.AlphanumericRunes[rand.Intn(len(constant.AlphanumericRunes))]
				if sigil, ok := s.sigilStore.Get(entity); ok {
					sigil.Rune = d.Char
					s.sigilStore.Set(entity, sigil)
				}
			}
			d.LastIntX = curX
			d.LastIntY = curY
		}

		// Grid Sync: Update PositionStore for spatial queries
		s.world.Positions.Set(entity, component.PositionComponent{X: curX, Y: curY})
		s.decayStore.Set(entity, d)
	}

	// Emit single batch event instead of scalar events per hit
	if len(deathCandidates) > 0 {
		event.EmitDeathBatch(s.res.Events.Queue, event.EventFlashRequest, deathCandidates, s.res.Time.FrameNumber)
	}
}

// shouldDieByDecay checks if a character has reached the end of the decay chain
func (s *DecaySystem) shouldDieByDecay(entity core.Entity) bool {
	glyph, ok := s.glyphStore.Get(entity)
	if !ok {
		return false
	}
	return glyph.Level == component.GlyphDark && glyph.Type == component.GlyphRed
}

// applyDecayToCharacter applies decay logic to a single character entity
func (s *DecaySystem) applyDecayToCharacter(entity core.Entity) {
	glyph, ok := s.glyphStore.Get(entity)
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

	// Apply decay logic
	if glyph.Level > component.GlyphDark {
		// Decrease level if not level dark
		glyph.Level--
		s.glyphStore.Set(entity, glyph)
	} else {
		// Dark level: type chain Blue→Green→Red→destroy
		switch glyph.Type {
		case component.GlyphBlue:
			glyph.Type = component.GlyphGreen
			glyph.Level = component.GlyphBright
			s.glyphStore.Set(entity, glyph)

		case component.GlyphGreen:
			glyph.Type = component.GlyphRed
			glyph.Level = component.GlyphBright
			s.glyphStore.Set(entity, glyph)

		default:
			// Fallback: Red or other: destroy
			event.EmitDeathOne(s.res.Events.Queue, entity, event.EventFlashRequest, s.res.Time.FrameNumber)
		}
	}

	s.statApplied.Add(1)
}