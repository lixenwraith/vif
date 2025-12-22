package system

import (
	"fmt"
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
	"github.com/lixenwraith/vi-fighter/engine/status"
	"github.com/lixenwraith/vi-fighter/event"
)

// ColorLevelKey represents a unique color+level combination
type ColorLevelKey struct {
	Type  component.CharacterType
	Level component.CharacterLevel
}

// ColorCensus holds entity counts for each color/level combination
// Used for 6-color spawn limit enforcement
type ColorCensus struct {
	BlueBright  int
	BlueNormal  int
	BlueDark    int
	GreenBright int
	GreenNormal int
	GreenDark   int
}

// Total returns sum of all tracked colors
func (c ColorCensus) Total() int {
	return c.BlueBright + c.BlueNormal + c.BlueDark +
		c.GreenBright + c.GreenNormal + c.GreenDark
}

// ActiveColors returns count of non-zero color/level combinations
func (c ColorCensus) ActiveColors() int {
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

// CodeBlock represents a logical group of related lines
type CodeBlock struct {
	Lines       []string
	IndentLevel int
	HasBraces   bool
}

// SpawnSystem handles character sequence generation and spawning
type SpawnSystem struct {
	mu    sync.RWMutex
	world *engine.World
	res   engine.Resources

	charStore     *engine.Store[component.CharacterComponent]
	typeableStore *engine.Store[component.TypeableComponent]

	// Spawn timing and rate (internal state, replaces GameState coupling)
	enabled        bool
	lastSpawnTime  time.Time // When last spawn occurred
	nextSpawnTime  time.Time // When next spawn should occur
	rateMultiplier float64   // 0.5x, 1.0x, 2.0x based on screen fill

	// Content consumption tracking (frame-local)
	localGeneration int64
	localIndex      int
	frameContent    *content.PreparedContent // Snapshot for current frame

	// Cached metric pointers
	statEnabled  *atomic.Bool
	statDensity  *status.AtomicFloat
	statRateMult *status.AtomicFloat
}

// NewSpawnSystem creates a new spawn system
func NewSpawnSystem(world *engine.World) engine.System {
	res := engine.GetResources(world)

	s := &SpawnSystem{
		world: world,
		res:   res,

		charStore:     engine.GetStore[component.CharacterComponent](world),
		typeableStore: engine.GetStore[component.TypeableComponent](world),

		// Cache metric pointers
		statEnabled:  res.Status.Bools.Get("spawn.enabled"),
		statDensity:  res.Status.Floats.Get("spawn.density"),
		statRateMult: res.Status.Floats.Get("spawn.rate_mult"),
	}

	// Initialize metrics
	s.statEnabled.Store(true)

	s.initLocked()
	return s
}

// Init resets session state for new game
func (s *SpawnSystem) Init() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()
}

// initLocked performs session state reset, caller must hold s.mu
func (s *SpawnSystem) initLocked() {
	s.enabled = true
	s.lastSpawnTime = time.Time{}
	s.nextSpawnTime = time.Time{}
	s.rateMultiplier = 1.0
	s.localGeneration = 0
	s.localIndex = 0
	s.frameContent = nil
}

// Priority returns the system's priority
func (s *SpawnSystem) Priority() int {
	return constant.PrioritySpawn
}

// EventTypes returns the event types SpawnSystem handles
func (s *SpawnSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventSpawnChange,
		event.EventGameReset,
	}
}

// HandleEvent processes spawn configuration events
func (s *SpawnSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventSpawnChange:
		if payload, ok := ev.Payload.(*event.SpawnChangePayload); ok {
			s.enabled = payload.Enabled
			s.statEnabled.Store(payload.Enabled)
		}

	case event.EventGameReset:
		s.Init()
		s.statRateMult.Set(1.0)
		s.statDensity.Set(0.0)
	}
}

// Update runs the spawn system logic
func (s *SpawnSystem) Update() {
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

	// Generate and spawn a new sequence
	// spawnSequence handles nil/empty content internally
	s.spawnSequence()

	// Schedule next spawn
	s.scheduleNextSpawn()
}

// calculateDensity returns entity density as fraction of screen capacity
func (s *SpawnSystem) calculateDensity(entityCount, screenCapacity int) float64 {
	if screenCapacity <= 0 {
		return 0
	}
	return float64(entityCount) / float64(screenCapacity)
}

// updateRateMultiplier adjusts spawn rate based on screen density
// <30% filled: 2x faster, 30-70%: normal, >70%: 0.5x slower
func (s *SpawnSystem) updateRateMultiplier(density float64) {
	if density < constant.SpawnDensityLowThreshold {
		s.rateMultiplier = constant.SpawnRateFast
	} else if density > constant.SpawnDensityHighThreshold {
		s.rateMultiplier = constant.SpawnRateSlow
	} else {
		s.rateMultiplier = constant.SpawnRateNormal
	}
}

// scheduleNextSpawn calculates and sets the next spawn time
func (s *SpawnSystem) scheduleNextSpawn() {
	now := s.res.Time.GameTime
	baseDelay := time.Duration(constant.SpawnIntervalMs) * time.Millisecond
	adjustedDelay := time.Duration(float64(baseDelay) / s.rateMultiplier)

	s.lastSpawnTime = now
	s.nextSpawnTime = now.Add(adjustedDelay)
}

// getNextBlock retrieves the next logical code block
func (s *SpawnSystem) getNextBlock() content.CodeBlock {
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
func (s *SpawnSystem) hasBracesInBlock(lines []string) bool {
	for _, line := range lines {
		if strings.Contains(line, "{") || strings.Contains(line, "}") {
			return true
		}
	}
	return false
}

// runCensus iterates all typeable entities and counts colors
// Called once per spawn check, O(n) where n ≈ 200 max entities
func (s *SpawnSystem) runCensus() ColorCensus {
	var census ColorCensus

	entities := s.typeableStore.All()
	for _, entity := range entities {
		typeable, ok := s.typeableStore.Get(entity)
		if !ok {
			continue
		}

		// Only count Blue and Green (Red, Gold, Nugget excluded from 6-color limit)
		switch typeable.Type {
		case component.TypeBlue:
			switch typeable.Level {
			case component.LevelBright:
				census.BlueBright++
			case component.LevelNormal:
				census.BlueNormal++
			case component.LevelDark:
				census.BlueDark++
			}
		case component.TypeGreen:
			switch typeable.Level {
			case component.LevelBright:
				census.GreenBright++
			case component.LevelNormal:
				census.GreenNormal++
			case component.LevelDark:
				census.GreenDark++
			}
		}
	}

	return census
}

// getAvailableColorsFromCensus returns color/level combinations not present on screen
func (s *SpawnSystem) getAvailableColorsFromCensus(census ColorCensus) []ColorLevelKey {
	available := make([]ColorLevelKey, 0, 6)

	if census.BlueBright == 0 {
		available = append(available, ColorLevelKey{
			Type: component.CharacterBlue, Level: component.LevelBright,
		})
	}
	if census.BlueNormal == 0 {
		available = append(available, ColorLevelKey{
			Type: component.CharacterBlue, Level: component.LevelNormal,
		})
	}
	if census.BlueDark == 0 {
		available = append(available, ColorLevelKey{
			Type: component.CharacterBlue, Level: component.LevelDark,
		})
	}
	if census.GreenBright == 0 {
		available = append(available, ColorLevelKey{
			Type: component.CharacterGreen, Level: component.LevelBright,
		})
	}
	if census.GreenNormal == 0 {
		available = append(available, ColorLevelKey{
			Type: component.CharacterGreen, Level: component.LevelNormal,
		})
	}
	if census.GreenDark == 0 {
		available = append(available, ColorLevelKey{
			Type: component.CharacterGreen, Level: component.LevelDark,
		})
	}

	return available
}

// typeableTypeFromSeq converts CharacterType to TypeableType
func typeableTypeFromSeq(st component.CharacterType) component.TypeableType {
	switch st {
	case component.CharacterBlue:
		return component.TypeBlue
	case component.CharacterGreen:
		return component.TypeGreen
	case component.CharacterRed:
		return component.TypeRed
	case component.CharacterGold:
		return component.TypeGold
	default:
		return component.TypeGreen
	}
}

// spawnSequence generates and spawns a new character block from file
func (s *SpawnSystem) spawnSequence() {
	// Census for color counters
	census := s.runCensus()
	availableColors := s.getAvailableColorsFromCensus(census)

	if len(availableColors) == 0 {
		// All 6 color combinations are present, don't spawn
		return
	}

	// Check if we have content (already snapshotted in Update)
	if s.frameContent == nil || len(s.frameContent.Blocks) == 0 {
		// No content available
		return
	}

	// Select random available color
	colorKey := availableColors[rand.Intn(len(availableColors))]
	seqType := colorKey.Type
	seqLevel := colorKey.Level

	// Get next logical code block
	block := s.getNextBlock()
	if len(block.Lines) == 0 {
		return
	}

	// Try to place each line from the block on the screen
	for _, line := range block.Lines {
		s.placeLine(line, seqType, seqLevel)
	}
}

// placeLine attempts to place a single line on the screen using generic stores
// Lines exceeding GameWidth are cropped to fit available space
func (s *SpawnSystem) placeLine(line string, seqType component.CharacterType, seqLevel component.CharacterLevel) bool {
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
			panic(fmt.Errorf("cursor destroyed"))
		}
		for i := 0; i < lineLength; i++ {
			col := startCol + i
			if math.Abs(float64(col-cursorPos.X)) <= constant.CursorExclusionX &&
				math.Abs(float64(row-cursorPos.Y)) <= constant.CursorExclusionY {
				hasOverlap = true
				break
			}
		}

		if !hasOverlap {
			// Valid position found, create entities

			// Phase 1: Create entities and prepare components
			type entityData struct {
				entity core.Entity
				pos    component.PositionComponent
				char   component.CharacterComponent
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
					char: component.CharacterComponent{
						Rune: lineRunes[i],
						// Color defaults to ColorNone (0), signaling renderer to use SeqType/SeqLevel
						Style:    component.StyleNormal,
						SeqType:  seqType,
						SeqLevel: seqLevel,
					},
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

			// Phase 3: Add other components (positions already committed)
			for _, ed := range entities {
				s.charStore.Add(ed.entity, ed.char)
				s.typeableStore.Add(ed.entity, component.TypeableComponent{
					Char:  ed.char.Rune,
					Type:  typeableTypeFromSeq(seqType),
					Level: seqLevel,
				})
			}

			return true
		}
	}

	// Failed to place after MaxPlacementTries attempts
	return false
}