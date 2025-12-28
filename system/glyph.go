package system

import (
	"math"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/content"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/status"
)

// GlyphKey represents a unique glyph type+level combination
type GlyphKey struct {
	Type  component.GlyphType
	Level component.GlyphLevel
}

// GlyphCensus holds entity counts for each type/level combination
// Used for 6-color spawn limit enforcement
type GlyphCensus struct {
	BlueBright  int
	BlueNormal  int
	BlueDark    int
	GreenBright int
	GreenNormal int
	GreenDark   int
}

// Total returns sum of all tracked colors
func (c GlyphCensus) Total() int {
	return c.BlueBright + c.BlueNormal + c.BlueDark +
		c.GreenBright + c.GreenNormal + c.GreenDark
}

// ActiveColors returns count of non-zero type/level combinations
func (c GlyphCensus) ActiveColors() int {
	count := 0
	if c.BlueBright > 0 {
		count++
	}
	if c.BlueNormal > 0 {
		count++
	}
	if c.BlueDark > 0 {
		count++
	}
	if c.GreenBright > 0 {
		count++
	}
	if c.GreenNormal > 0 {
		count++
	}
	if c.GreenDark > 0 {
		count++
	}
	return count
}

// GlyphSystem handles glyph sequence generation and spawning
type GlyphSystem struct {
	mu    sync.RWMutex
	world *engine.World
	res   engine.Resources

	glyphStore    *engine.Store[component.GlyphComponent]
	typeableStore *engine.Store[component.TypeableComponent]

	// Spawn timing and rate
	lastSpawnTime  time.Time // When last spawn occurred
	nextSpawnTime  time.Time // When next spawn should occur
	rateMultiplier float64   // 0.5x, 1.0x, 2.0x based on screen fill

	// Content consumption tracking (frame-local)
	localGeneration int64
	localIndex      int
	frameContent    *content.PreparedContent // Snapshot for current frame

	// Cached metric pointers
	statEnabled        *atomic.Bool
	statDensity        *status.AtomicFloat
	statRateMult       *status.AtomicFloat
	statOrphanGlyph    *atomic.Int64
	statOrphanTypeable *atomic.Int64

	enabled bool
}

// NewGlyphSystem creates a new glyph system
func NewGlyphSystem(world *engine.World) engine.System {
	res := engine.GetResources(world)

	s := &GlyphSystem{
		world: world,
		res:   res,

		glyphStore:    engine.GetStore[component.GlyphComponent](world),
		typeableStore: engine.GetStore[component.TypeableComponent](world),

		// Cache metric pointers
		statEnabled:        res.Status.Bools.Get("glyph.enabled"),
		statDensity:        res.Status.Floats.Get("glyph.density"),
		statRateMult:       res.Status.Floats.Get("glyph.rate_mult"),
		statOrphanGlyph:    res.Status.Ints.Get("glyph.orphan_char"),
		statOrphanTypeable: res.Status.Ints.Get("glyph.orphan_typeable"),
	}

	s.initLocked()
	return s
}

// Init resets session state for new game
func (s *GlyphSystem) Init() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()
}

// initLocked performs session state reset, caller must hold s.mu
func (s *GlyphSystem) initLocked() {
	s.lastSpawnTime = time.Time{}
	s.nextSpawnTime = time.Time{}
	s.rateMultiplier = 1.0
	s.localGeneration = 0
	s.localIndex = 0
	s.frameContent = nil
	s.statEnabled.Store(true)
	s.statDensity.Set(0)
	s.statRateMult.Set(0)
	s.statOrphanGlyph.Store(0)
	s.statOrphanTypeable.Store(0)
	s.enabled = true
}

// Priority returns the system's priority
func (s *GlyphSystem) Priority() int {
	return constant.PrioritySpawn
}

// EventTypes returns the event types SpawnSystem handles
func (s *GlyphSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventSpawnChange,
		event.EventGameReset,
	}
}

// HandleEvent processes spawn configuration events
func (s *GlyphSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		s.statRateMult.Set(1.0)
		s.statDensity.Set(0.0)
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventSpawnChange:
		if payload, ok := ev.Payload.(*event.SpawnChangePayload); ok {
			s.enabled = payload.Enabled
			s.statEnabled.Store(payload.Enabled)
		}
	}
}

// Update runs the spawn system logic
func (s *GlyphSystem) Update() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.enabled {
		return
	}

	now := s.res.Time.GameTime
	config := s.res.Config

	// Calculate current density and update rate multiplier
	entityCount := s.world.Positions.Count()
	screenCapacity := config.GameWidth * config.GameHeight
	density := s.calculateDensity(entityCount, screenCapacity)
	s.updateRateMultiplier(density)

	// Update metrics
	s.statDensity.Set(density)
	s.statRateMult.Set(s.rateMultiplier)

	// Check if spawn is due
	if now.Before(s.nextSpawnTime) {
		return
	}

	// Snapshot content at frame start to prevent mid-frame race
	s.frameContent = s.res.Content.Provider.CurrentContent()

	// Detect content swap and reset index
	if s.frameContent != nil && s.frameContent.Generation != s.localGeneration {
		s.localGeneration = s.frameContent.Generation
		s.localIndex = 0
	}

	// Generate and spawn a new sequence of glyphs
	s.spawnGlyphs()

	// Schedule next spawn
	s.scheduleNextSpawn()
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
	if density < constant.SpawnDensityLowThreshold {
		s.rateMultiplier = constant.SpawnRateFast
	} else if density > constant.SpawnDensityHighThreshold {
		s.rateMultiplier = constant.SpawnRateSlow
	} else {
		s.rateMultiplier = constant.SpawnRateNormal
	}
}

// scheduleNextSpawn calculates and sets the next spawn time
func (s *GlyphSystem) scheduleNextSpawn() {
	now := s.res.Time.GameTime
	baseDelay := time.Duration(constant.SpawnIntervalMs) * time.Millisecond
	adjustedDelay := time.Duration(float64(baseDelay) / s.rateMultiplier)

	s.lastSpawnTime = now
	s.nextSpawnTime = now.Add(adjustedDelay)
}

// getNextBlock retrieves the next logical code block
func (s *GlyphSystem) getNextBlock() content.CodeBlock {
	if s.frameContent == nil || len(s.frameContent.Blocks) == 0 {
		return content.CodeBlock{}
	}

	// Bounds check and wrap
	if s.localIndex >= len(s.frameContent.Blocks) {
		s.localIndex = 0
	}

	block := s.frameContent.Blocks[s.localIndex]
	s.localIndex++

	// Notify service of consumption
	s.res.Content.Provider.NotifyConsumed(1)

	return block
}

// hasBracesInBlock checks if a block contains braces
func (s *GlyphSystem) hasBracesInBlock(lines []string) bool {
	for _, line := range lines {
		if strings.Contains(line, "{") || strings.Contains(line, "}") {
			return true
		}
	}
	return false
}

// runCensus iterates all glyph entities and counts types/levels
// Called once per spawn check, O(n)
func (s *GlyphSystem) runCensus() GlyphCensus {
	var census GlyphCensus
	var orphanGlyph, orphanTypeable int64

	glyphEntities := s.glyphStore.All()
	for _, entity := range glyphEntities {
		if !s.world.Positions.Has(entity) {
			orphanGlyph++
			continue
		}

		glyph, ok := s.glyphStore.Get(entity)
		if !ok {
			continue
		}

		// Only count Blue and Green (Red excluded from 6-color limit)
		switch glyph.Type {
		case component.GlyphBlue:
			switch glyph.Level {
			case component.GlyphBright:
				census.BlueBright++
			case component.GlyphNormal:
				census.BlueNormal++
			case component.GlyphDark:
				census.BlueDark++
			}
		case component.GlyphGreen:
			switch glyph.Level {
			case component.GlyphBright:
				census.GreenBright++
			case component.GlyphNormal:
				census.GreenNormal++
			case component.GlyphDark:
				census.GreenDark++
			}
		}
	}

	typeableEntities := s.typeableStore.All()
	for _, entity := range typeableEntities {
		if !s.world.Positions.Has(entity) {
			orphanTypeable++
		}
	}

	s.statOrphanGlyph.Store(orphanGlyph)
	s.statOrphanTypeable.Store(orphanTypeable)

	return census
}

// getAvailableColorsFromCensus returns color/level combinations not present on screen
func (s *GlyphSystem) getAvailableGlyphsFromCensus(census GlyphCensus) []GlyphKey {
	available := make([]GlyphKey, 0, 6)

	if census.BlueBright == 0 {
		available = append(available, GlyphKey{
			Type: component.GlyphBlue, Level: component.GlyphBright,
		})
	}
	if census.BlueNormal == 0 {
		available = append(available, GlyphKey{
			Type: component.GlyphBlue, Level: component.GlyphNormal,
		})
	}
	if census.BlueDark == 0 {
		available = append(available, GlyphKey{
			Type: component.GlyphBlue, Level: component.GlyphDark,
		})
	}
	if census.GreenBright == 0 {
		available = append(available, GlyphKey{
			Type: component.GlyphGreen, Level: component.GlyphBright,
		})
	}
	if census.GreenNormal == 0 {
		available = append(available, GlyphKey{
			Type: component.GlyphGreen, Level: component.GlyphNormal,
		})
	}
	if census.GreenDark == 0 {
		available = append(available, GlyphKey{
			Type: component.GlyphGreen, Level: component.GlyphDark,
		})
	}

	return available
}

// typeableTypeFromGlyph converts GlyphType to TypeableType
func typeableTypeFromGlyph(gt component.GlyphType) component.TypeableType {
	switch gt {
	case component.GlyphBlue:
		return component.TypeBlue
	case component.GlyphRed:
		return component.TypeRed
	default:
		return component.TypeGreen
	}
}

// typeableLevelFromGlyph converts GlyphLevel to TypeableLevel
func typeableLevelFromGlyph(gl component.GlyphLevel) component.TypeableLevel {
	return component.TypeableLevel(gl)
}

// spawnGlyphs generates and spawns a new glyph block from file
func (s *GlyphSystem) spawnGlyphs() {
	// Census for glyph counters
	census := s.runCensus()
	availableGlyphs := s.getAvailableGlyphsFromCensus(census)

	if len(availableGlyphs) == 0 {
		// All 6 type/level combinations are present, don't spawn
		return
	}

	// Check if we have content (already snapshotted in Update)
	if s.frameContent == nil || len(s.frameContent.Blocks) == 0 {
		return
	}

	// Select random available glyph type/level
	glyphKey := availableGlyphs[rand.Intn(len(availableGlyphs))]

	// Get next logical code block
	block := s.getNextBlock()
	if len(block.Lines) == 0 {
		return
	}

	// Try to place each line from the block on the screen
	for _, line := range block.Lines {
		s.placeLine(line, glyphKey.Type, glyphKey.Level)
	}
}

// placeLine attempts to place a single line on the screen
// Lines exceeding GameWidth are cropped to fit available space
func (s *GlyphSystem) placeLine(line string, glyphType component.GlyphType, glyphLevel component.GlyphLevel) bool {
	config := s.res.Config
	cursorEntity := s.res.Cursor.Entity

	lineRunes := []rune(line)
	lineLength := len(lineRunes)

	if lineLength == 0 {
		return false
	}

	// Crop line if it exceeds game width
	if lineLength > config.GameWidth {
		lineRunes = lineRunes[:config.GameWidth]
		lineLength = config.GameWidth
	}

	// Try up to MaxPlacementTries times to find a valid position
	for attempt := 0; attempt < constant.MaxPlacementTries; attempt++ {
		// Random row selection
		row := rand.Intn(config.GameHeight)

		// Check if line fits and find available columns
		if lineLength > config.GameWidth {
			// Line too long for screen, skip
			continue
		}

		// Random column selection (must have room for full line)
		maxStartCol := config.GameWidth - lineLength
		if maxStartCol < 0 {
			// Line still too long after crop, skip
			return false
		}

		startCol := rand.Intn(maxStartCol + 1)

		// Check for overlaps using HasAny
		hasOverlap := false
		for i := 0; i < lineLength; i++ {
			if s.world.Positions.HasAny(startCol+i, row) {
				hasOverlap = true
				break
			}
		}

		// Check if too close to cursor
		cursorPos, ok := s.world.Positions.Get(cursorEntity)
		if !ok {
			panic(nil)
		}
		for i := 0; i < lineLength; i++ {
			col := startCol + i
			if math.Abs(float64(col-cursorPos.X)) <= constant.CursorExclusionX &&
				math.Abs(float64(row-cursorPos.Y)) <= constant.CursorExclusionY {
				hasOverlap = true
				break
			}
		}

		if hasOverlap {
			continue
		}

		// Valid position found, create entities

		// Phase 1: Create entities and prepare components
		type entityData struct {
			entity core.Entity
			pos    component.PositionComponent
			char   rune
		}

		entities := make([]entityData, 0, lineLength)

		for i := 0; i < lineLength; i++ {
			// Skip space characters - don't create entities for them
			if lineRunes[i] == ' ' {
				continue
			}

			entity := s.world.CreateEntity()
			entities = append(entities, entityData{
				entity: entity,
				pos: component.PositionComponent{
					X: startCol + i,
					Y: row,
				},
				char: lineRunes[i],
			})
		}

		// Phase 2: Batch position validation and commit
		batch := s.world.Positions.BeginBatch()
		for _, ed := range entities {
			batch.Add(ed.entity, ed.pos)
		}

		if err := batch.Commit(); err != nil {
			// Collision detected - cleanup entities and try next attempt
			for _, ed := range entities {
				s.world.DestroyEntity(ed.entity)
			}
			continue
		}

		// Phase 3: Set glyph and typeable components
		for _, ed := range entities {
			s.glyphStore.Set(ed.entity, component.GlyphComponent{
				Rune:  ed.char,
				Type:  glyphType,
				Level: glyphLevel,
				Style: component.StyleNormal,
			})
			s.typeableStore.Set(ed.entity, component.TypeableComponent{
				Char:  ed.char,
				Type:  typeableTypeFromGlyph(glyphType),
				Level: typeableLevelFromGlyph(glyphLevel),
			})
		}

		return true
	}

	// Failed to place after MaxPlacementTries attempts
	return false
}