package system

import (
	"math"
	"math/rand"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/content"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/status"
)

// GlyphKey represents a unique glyph type+level combination
type GlyphKey struct {
	Type  component.GlyphType
	Level component.GlyphLevel
}

// GlyphCensus holds entity counts for each type/level combination
type GlyphCensus map[GlyphKey]int

// Allowed to spawn types and levels used as census keys
var glyphSpawnTypes = []component.GlyphType{component.GlyphBlue, component.GlyphGreen}
var glyphSpawnLevels = []component.GlyphLevel{component.GlyphDark, component.GlyphNormal, component.GlyphBright}

// GlyphSystem handles glyph sequence generation and spawning
type GlyphSystem struct {
	world *engine.World

	// Glyph census
	census map[GlyphKey]int

	// Spawn timing and rate
	nextSpawnTimer time.Duration
	rateMultiplier float64 // 0.5x, 1.0x, 2.0x based on screen fill

	// Content consumption tracking (frame-local)
	localGeneration int64
	localIndex      int
	frameContent    *content.PreparedContent // Snapshot for current frame

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
	s.census = make(map[GlyphKey]int)
	s.initCensus()

	s.nextSpawnTimer = time.Duration(0)
	s.rateMultiplier = 1.0
	s.localGeneration = 0
	s.localIndex = 0
	s.frameContent = nil
	s.statEnabled.Store(true)
	s.statDensity.Set(0)
	s.statRateMult.Set(0)
	s.statNextSpawnMS.Store(0)
	s.statOrphanGlyph.Store(0)
	s.enabled = true
}

// initCensus prepares an empty census with spawn keys
func (s *GlyphSystem) initCensus() {
	for _, spawnType := range glyphSpawnTypes {
		for _, spawnLevel := range glyphSpawnLevels {
			s.census[GlyphKey{Type: spawnType, Level: spawnLevel}] = 0
		}
	}
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

	// switch ev.Type {
	//
	// }
}

// Update runs the spawn system logic
func (s *GlyphSystem) Update() {
	if !s.enabled {
		return
	}

	config := s.world.Resources.Config

	// Calculate current density and update rate multiplier
	glyphCount := s.world.Components.Glyph.CountEntities()
	screenCapacity := config.GameWidth * config.GameHeight
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

	// Snapshot content at frame start to prevent mid-frame race
	s.frameContent = s.world.Resources.Content.Provider.CurrentContent()

	// Detect content swap and reset index
	if s.frameContent != nil && s.frameContent.Generation != s.localGeneration {
		s.localGeneration = s.frameContent.Generation
		s.localIndex = 0
	}

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
	s.world.Resources.Content.Provider.NotifyConsumed(1)

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

// updateCensus iterates all glyph entities and counts types/levels
// Called once per spawn check, O(n)
func (s *GlyphSystem) updateCensus() {
	s.initCensus()

	var orphanGlyph int64

	glyphEntities := s.world.Components.Glyph.GetAllEntities()
	for _, glyphEntity := range glyphEntities {
		if !s.world.Positions.HasPosition(glyphEntity) {
			orphanGlyph++
			continue
		}

		glyphComp, ok := s.world.Components.Glyph.GetComponent(glyphEntity)
		if !ok {
			continue
		}

		if glyphComp.Type != component.GlyphBlue && glyphComp.Type != component.GlyphGreen {
			continue
		}
		key := GlyphKey{Type: glyphComp.Type, Level: glyphComp.Level}
		s.census[key]++

	}

	s.statOrphanGlyph.Store(orphanGlyph)

}

// nextGlyphToSpawn returns color/level combinations not present on screen
func (s *GlyphSystem) nextGlyphToSpawn() GlyphKey {
	minGlyphCount := -1
	var minGlyphKey GlyphKey
	for key, count := range s.census {
		if minGlyphCount == -1 {
			minGlyphCount = count
			minGlyphKey = key
		} else if count < minGlyphCount {
			minGlyphCount = count
		}
	}
	return minGlyphKey
}

// spawnGlyphs generates and spawns a new glyph block from file
func (s *GlyphSystem) spawnGlyphs() {
	// Census for glyph counters
	s.updateCensus()
	glyphKey := s.nextGlyphToSpawn()

	// Check if we have content (already snapshotted in Update)
	if s.frameContent == nil || len(s.frameContent.Blocks) == 0 {
		return
	}

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
	config := s.world.Resources.Config
	cursorEntity := s.world.Resources.Player.Entity

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
	for attempt := 0; attempt < parameter.MaxPlacementTries; attempt++ {
		// Random row selection
		// TODO: convert to fast rand
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

		// Check for overlaps
		hasOverlap := false
		for i := 0; i < lineLength; i++ {
			if s.world.Positions.IsBlocked(startCol+i, row, component.WallBlockSpawn) {
				hasOverlap = true
				break
			}
		}

		// Check if too close to cursor
		cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
		if !ok {
			return false
		}
		for i := 0; i < lineLength; i++ {
			col := startCol + i
			if math.Abs(float64(col-cursorPos.X)) <= parameter.CursorExclusionX &&
				math.Abs(float64(row-cursorPos.Y)) <= parameter.CursorExclusionY {
				hasOverlap = true
				break
			}
		}

		if hasOverlap {
			continue
		}

		// Valid position found, create entities

		// 1. Create entities and prepare components
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

		// 2. Batch position validation and commit
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

		// 3. Set glyph components
		for _, ed := range entities {
			s.world.Components.Glyph.SetComponent(ed.entity, component.GlyphComponent{
				Rune:  ed.char,
				Type:  glyphType,
				Level: glyphLevel,
			})
		}

		return true
	}

	// Failed to place after MaxPlacementTries attempts
	return false
}