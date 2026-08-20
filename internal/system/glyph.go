package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/status"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// GlyphKey is a spawnable glyph type/level pair
type GlyphKey struct {
	Type  component.GlyphType
	Level component.GlyphLevel
}

// Spawnable types and levels; census slots are indexed by position here,
// so the enum values themselves are free to change
var glyphSpawnTypes = [...]component.GlyphType{component.GlyphBlue, component.GlyphGreen}
var glyphSpawnLevels = [...]component.GlyphLevel{component.GlyphDark, component.GlyphNormal, component.GlyphBright}

const glyphCensusSlots = len(glyphSpawnTypes) * len(glyphSpawnLevels)

// glyphCensus counts live glyphs per type/level slot
type glyphCensus [glyphCensusSlots]int

// censusSlot maps a type/level pair to its slot, -1 when not spawnable
func censusSlot(t component.GlyphType, l component.GlyphLevel) int {
	ti := -1
	for i, st := range glyphSpawnTypes {
		if st == t {
			ti = i
			break
		}
	}
	if ti < 0 {
		return -1
	}
	for i, sl := range glyphSpawnLevels {
		if sl == l {
			return ti*len(glyphSpawnLevels) + i
		}
	}
	return -1
}

// glyphPlacement is a staged entity awaiting batch position commit
type glyphPlacement struct {
	entity core.Entity
	pos    component.PositionComponent
	char   rune
}

// GlyphSystem handles glyph sequence generation and spawning
type GlyphSystem struct {
	world *engine.World

	rng *vmath.FastRand

	// Glyph census
	census glyphCensus

	// Spawn timing and rate
	nextSpawnTimer time.Duration
	rateMultiplier float64 // 0.5x, 1.0x, 2.0x based on screen fill

	// Reused placement scratch
	placement []glyphPlacement

	// Cached metric pointers
	statEnabled     *atomic.Bool
	statDensity     *status.AtomicFloat
	statRateMult    *status.AtomicFloat
	statNextSpawnMS *atomic.Int64
	statOrphanGlyph *atomic.Int64

	enabled bool
}

// NewGlyphSystem creates a new glyph system
func NewGlyphSystem(world *engine.World) engine.System {
	s := &GlyphSystem{
		world: world,
	}

	// Cache metric pointers
	s.statEnabled = world.Resources.Status.Bools.Get("glyph.enabled")
	s.statDensity = world.Resources.Status.Floats.Get("glyph.density")
	s.statRateMult = world.Resources.Status.Floats.Get("glyph.rate_mult")
	s.statNextSpawnMS = world.Resources.Status.Ints.Get("glyph.next_spawn_ms")
	s.statOrphanGlyph = world.Resources.Status.Ints.Get("glyph.orphan_glyph")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *GlyphSystem) Init() {
	s.rng = s.world.Rand(s.Name())
	s.census = glyphCensus{}

	s.nextSpawnTimer = time.Duration(0)
	s.rateMultiplier = 1.0
	s.placement = s.placement[:0]
	s.statEnabled.Store(true)
	s.statDensity.Set(0)
	s.statRateMult.Set(0)
	s.statNextSpawnMS.Store(0)
	s.statOrphanGlyph.Store(0)
	s.enabled = true
}

// Name returns system's name
func (s *GlyphSystem) Name() string {
	return "glyph"
}

// Priority returns the system's priority
func (s *GlyphSystem) Priority() int {
	return parameter.PriorityGlyph
}

// EventTypes returns the event types SpawnSystem handles
func (s *GlyphSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

// HandleEvent processes spawn configuration events
func (s *GlyphSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
		s.Init()
		s.statRateMult.Set(1.0)
		s.statDensity.Set(0.0)
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
				s.statEnabled.Store(payload.Enabled)
			}
		}
	}

	if !s.enabled {
		return
	}
}

// Update runs the spawn system logic
func (s *GlyphSystem) Update() {
	if !s.enabled {
		return
	}

	config := s.world.Resources.Config

	// Calculate current density and update rate multiplier
	glyphCount := s.world.Components.Glyph.CountEntities()
	screenCapacity := config.MapWidth * config.MapHeight
	density := s.calculateDensity(glyphCount, screenCapacity)
	s.updateRateMultiplier(density)

	// Check if spawn is due
	dt := s.world.Resources.Time.DeltaTime
	s.nextSpawnTimer -= dt

	// Update metrics
	s.statDensity.Set(density)
	s.statRateMult.Set(s.rateMultiplier)
	s.statNextSpawnMS.Store(s.nextSpawnTimer.Milliseconds())

	maybeNewSpawnTimer := s.calculateNextSpawn()

	if s.nextSpawnTimer > 0 && maybeNewSpawnTimer > s.nextSpawnTimer {
		return
	}
	s.nextSpawnTimer = maybeNewSpawnTimer

	// Generate and spawn a new sequence of glyphs
	s.spawnGlyphs()
}

// calculateDensity returns entity density as fraction of screen capacity
func (s *GlyphSystem) calculateDensity(entityCount, screenCapacity int) float64 {
	if screenCapacity <= 0 {
		return 0
	}
	return float64(entityCount) / float64(screenCapacity)
}

// updateRateMultiplier adjusts spawn rate based on screen density
// <30% filled: 2x faster, 30-70%: normal, >70%: 0.5x slower
func (s *GlyphSystem) updateRateMultiplier(density float64) {
	if density < parameter.SpawnDensityLowThreshold {
		s.rateMultiplier = parameter.SpawnRateFast
	} else if density > parameter.SpawnDensityHighThreshold {
		s.rateMultiplier = parameter.SpawnRateSlow
	} else {
		s.rateMultiplier = parameter.SpawnRateNormal
	}
}

// calculateNextSpawn calculates and sets the next spawn time
func (s *GlyphSystem) calculateNextSpawn() time.Duration {
	baseDelay := time.Duration(parameter.SpawnIntervalMs) * time.Millisecond
	adjustedDelay := time.Duration(float64(baseDelay) / s.rateMultiplier)

	return adjustedDelay
}

// updateCensus counts live glyphs per spawn slot, called once per spawn, O(n)
func (s *GlyphSystem) updateCensus() {
	s.census = glyphCensus{}

	var orphanGlyph int64

	for _, glyphEntity := range s.world.Components.Glyph.Entities() {
		if !s.world.Positions.HasPosition(glyphEntity) {
			orphanGlyph++
			continue
		}

		glyphComp, ok := s.world.Components.Glyph.GetPtr(glyphEntity)
		if !ok {
			continue
		}

		if slot := censusSlot(glyphComp.Type, glyphComp.Level); slot >= 0 {
			s.census[slot]++
		}
	}

	s.statOrphanGlyph.Store(orphanGlyph)
}

// nextGlyphToSpawn returns the least represented type/level pair on the map.
// Ties break uniformly at random so equal counts don't lock onto one slot
func (s *GlyphSystem) nextGlyphToSpawn() GlyphKey {
	best, ties := 0, 1
	for slot := 1; slot < len(s.census); slot++ {
		switch {
		case s.census[slot] < s.census[best]:
			best, ties = slot, 1
		case s.census[slot] == s.census[best]:
			ties++
			if s.rng.Intn(ties) == 0 {
				best = slot
			}
		}
	}
	return GlyphKey{
		Type:  glyphSpawnTypes[best/len(glyphSpawnLevels)],
		Level: glyphSpawnLevels[best%len(glyphSpawnLevels)],
	}
}

// spawnGlyphs pulls the next content block and places its lines
func (s *GlyphSystem) spawnGlyphs() {
	res := s.world.Resources.Content
	if res == nil || res.Provider == nil {
		return
	}

	block, ok := res.Provider.NextBlock()
	if !ok {
		return
	}

	s.updateCensus()
	key := s.nextGlyphToSpawn()

	for _, line := range block.Lines {
		s.placeLine(line, key.Type, key.Level)
	}
}

// placeLine attempts to place a single line on the map
// Lines wider than the map are cropped; the map width is the only crop policy
func (s *GlyphSystem) placeLine(line string, glyphType component.GlyphType, glyphLevel component.GlyphLevel) bool {
	config := s.world.Resources.Config

	lineRunes := []rune(line)
	if len(lineRunes) == 0 {
		return false
	}
	if len(lineRunes) > config.MapWidth {
		lineRunes = lineRunes[:config.MapWidth]
	}
	lineLength := len(lineRunes)

	if s.world.Resources.Player.Count() == 0 {
		return false
	}

	for range parameter.MaxPlacementTries {
		row := s.rng.Intn(config.MapHeight)
		startCol := s.rng.Intn(config.MapWidth - lineLength + 1)

		// Cursor exclusion: interval overlap on X, distance on Y
		excluded := false
		for i := range parameter.MaxPlayers {
			cursor := s.world.Resources.Player.Slot(uint8(i))
			cursorPos, ok := s.world.Positions.GetPosition(cursor)
			if ok && vmath.IntAbs(row-cursorPos.Y) <= parameter.CursorExclusionY &&
				startCol <= cursorPos.X+parameter.CursorExclusionX &&
				startCol+lineLength > cursorPos.X-parameter.CursorExclusionX {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		blocked := false
		for i := range lineLength {
			if s.world.Positions.IsBlocked(startCol+i, row, component.WallBlockSpawn) {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}

		// 1. Stage entities, skipping spaces
		s.placement = s.placement[:0]
		for i := range lineLength {
			if lineRunes[i] == ' ' {
				continue
			}
			s.placement = append(s.placement, glyphPlacement{
				entity: s.world.CreateEntity(),
				pos:    component.PositionComponent{X: startCol + i, Y: row},
				char:   lineRunes[i],
			})
		}

		// 2. Batch position validation and commit
		batch := s.world.Positions.BeginBatch()
		for _, gp := range s.placement {
			batch.Add(gp.entity, gp.pos)
		}
		if err := batch.Commit(); err != nil {
			for _, gp := range s.placement {
				s.world.DestroyEntity(gp.entity)
			}
			continue
		}

		// 3. Attach glyph components
		for _, gp := range s.placement {
			s.world.Components.Glyph.SetComponent(gp.entity, component.GlyphComponent{
				Rune:  gp.char,
				Type:  glyphType,
				Level: glyphLevel,
			})
		}
		return true
	}

	return false
}
