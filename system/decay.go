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

	count := s.world.Components.Decay.CountEntity()
	if count == 0 {
		s.statCount.Store(0)
		return
	}

	s.updateDecayEntities()
	s.statCount.Store(int64(s.world.Components.Decay.CountEntity()))
}

// spawnSingleDecay creates one decay entity at specified position
func (s *DecaySystem) spawnSingleDecay(x, y int, char rune, skipStartCell bool) {
	// Random speed between ParticleMinSpeed and ParticleMaxSpeed
	// Note: Speed is converted to Q32.32. Decay moves DOWN by default, so velocity is positive
	speedFloat := constant.ParticleMinSpeed + rand.Float64()*(constant.ParticleMaxSpeed-constant.ParticleMinSpeed)
	velY := vmath.FromFloat(speedFloat)
	accelY := vmath.FromFloat(constant.ParticleAcceleration)

	entity := s.world.CreateEntity()

	// 1. Grid Positions
	s.world.Positions.SetPosition(entity, component.PositionComponent{X: x, Y: y})

	// 2. Physics/Logic Components
	lastX, lastY := -1, -1
	if skipStartCell {
		lastX, lastY = x, y
	}
	s.world.Components.Decay.SetComponent(entity, component.DecayComponent{
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
	s.world.Components.Sigil.SetComponent(entity, component.SigilComponent{
		Rune:  char,
		Color: component.SigilDecay,
	})
}

// spawnDecayWave creates a screen-wide falling decay wave
func (s *DecaySystem) spawnDecayWave() {
	gameWidth := s.world.Resources.Config.GameWidth

	// Spawn one decay entity per column for full-width coverage
	for column := 0; column < gameWidth; column++ {
		char := constant.AlphanumericRunes[rand.Intn(len(constant.AlphanumericRunes))]
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

	gameWidth := s.world.Resources.Config.GameWidth
	gameHeight := s.world.Resources.Config.GameHeight

	decayEntities := s.world.Components.Decay.AllEntity()

	// Clear frame deduplication maps
	clear(s.processedGridCells)
	clear(s.decayedThisFrame)

	// Local buffers
	var deathCandidates []core.Entity
	var collisionBuf [constant.MaxEntitiesPerCell]core.Entity

	for _, entity := range decayEntities {
		d, ok := s.world.Components.Decay.GetComponent(entity)
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

			n := s.world.Positions.GetAllEntityAtInto(x, y, collisionBuf[:])
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
					s.world.DestroyEntity(target)
					s.world.DestroyEntity(entity)
					break
				}

				if s.world.Components.Nugget.HasEntity(target) {
					s.world.PushEvent(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{Entity: target})
					event.EmitDeathOne(s.world.Resources.Event.Queue, target, event.EventFlashRequest, s.world.Resources.Time.FrameNumber)
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

		// 2D Matrix Visual Effect: Update character on ANY cell entry
		if d.LastIntX != curX || d.LastIntY != curY {
			if rand.Float64() < constant.ParticleChangeChance {
				d.Char = constant.AlphanumericRunes[rand.Intn(len(constant.AlphanumericRunes))]
				if sigil, ok := s.world.Components.Sigil.GetComponent(entity); ok {
					sigil.Rune = d.Char
					s.world.Components.Sigil.SetComponent(entity, sigil)
				}
			}
			d.LastIntX = curX
			d.LastIntY = curY
		}

		// Grid Sync: Update Positions for spatial queries
		s.world.Positions.SetPosition(entity, component.PositionComponent{X: curX, Y: curY})
		s.world.Components.Decay.SetComponent(entity, d)
	}

	// Emit single batch event instead of scalar events per hit
	if len(deathCandidates) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, event.EventFlashRequest, deathCandidates, s.world.Resources.Time.FrameNumber)
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
	glyph, ok := s.world.Components.Glyph.GetComponent(entity)
	if !ok {
		return
	}

	// Check protection
	if prot, ok := s.world.Components.Protection.GetComponent(entity); ok {
		now := s.world.Resources.Time.GameTime
		if !prot.IsExpired(now.UnixNano()) && prot.Mask.Has(component.ProtectFromDecay) {
			return
		}
	}

	// Apply decay logic
	if glyph.Level > component.GlyphDark {
		// Decrease level if not level dark
		glyph.Level--
		s.world.Components.Glyph.SetComponent(entity, glyph)
	} else {
		// Dark level: type chain Blue→Green→Red→destroy
		switch glyph.Type {
		case component.GlyphBlue:
			glyph.Type = component.GlyphGreen
			glyph.Level = component.GlyphBright
			s.world.Components.Glyph.SetComponent(entity, glyph)

		case component.GlyphGreen:
			glyph.Type = component.GlyphRed
			glyph.Level = component.GlyphBright
			s.world.Components.Glyph.SetComponent(entity, glyph)

		default:
			// Fallback: Red or other: destroy
			event.EmitDeathOne(s.world.Resources.Event.Queue, entity, event.EventFlashRequest, s.world.Resources.Time.FrameNumber)
		}
	}

	s.statApplied.Add(1)
}