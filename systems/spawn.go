package systems

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/content"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/engine/status"
	"github.com/lixenwraith/vi-fighter/events"
)

// ColorLevelKey represents a unique color+level combination
type ColorLevelKey struct {
	Type  components.SequenceType
	Level components.SequenceLevel
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
	res   engine.CoreResources

	seqStore  *engine.Store[components.SequenceComponent]
	charStore *engine.Store[components.CharacterComponent]

	// Spawn timing and rate (internal state, replaces GameState coupling)
	enabled        bool
	lastSpawnTime  time.Time // When last spawn occurred
	nextSpawnTime  time.Time // When next spawn should occur
	rateMultiplier float64   // 0.5x, 1.0x, 2.0x based on screen fill

	// Content management
	contentManager *content.ContentManager
	contentMutex   sync.RWMutex
	indexMutex     sync.Mutex // Protect index update and wraparound operations
	codeBlocks     []CodeBlock
	nextBlockIndex atomic.Int32 // Index for thread-safe access
	totalBlocks    atomic.Int32 // Counter for total blocks
	blocksConsumed atomic.Int32 // Counter for consumed blocks
	nextContent    []CodeBlock  // Pre-fetched content for seamless transition
	isRefreshing   atomic.Bool

	// Cached metric pointers (zero-lock writes in Update)
	statEnabled  *atomic.Bool
	statDensity  *status.AtomicFloat
	statRateMult *status.AtomicFloat
}

// NewSpawnSystem creates a new spawn system
func NewSpawnSystem(world *engine.World) engine.System {
	res := engine.GetCoreResources(world)

	s := &SpawnSystem{
		world: world,
		res:   res,

		seqStore:  engine.GetStore[components.SequenceComponent](world),
		charStore: engine.GetStore[components.CharacterComponent](world),

		// Cache metric pointers
		statEnabled:  res.Status.Bools.Get("spawn.enabled"),
		statDensity:  res.Status.Floats.Get("spawn.density"),
		statRateMult: res.Status.Floats.Get("spawn.rate_mult"),
	}

	// Initialize ContentManager
	s.contentManager = content.NewContentManager()
	if err := s.contentManager.DiscoverContentFiles(); err != nil {
		// Continues gracefully with empty content
		// TODO: add status bar error message
	}

	// Pre-validate all discovered content files once at startup and build cache
	if err := s.contentManager.PreValidateAllContent(); err != nil {
		// Continue gracefully
	}

	// Initialize metrics
	s.statEnabled.Store(true)

	// Load initial content
	s.loadContentFromManager()

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
	s.nextBlockIndex.Store(0)
	s.blocksConsumed.Store(0)
	s.isRefreshing.Store(false)
}

// Priority returns the system's priority
func (s *SpawnSystem) Priority() int {
	return constants.PrioritySpawn
}

// EventTypes returns the event types SpawnSystem handles
func (s *SpawnSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventSpawnChange,
		events.EventGameReset,
	}
}

// HandleEvent processes spawn configuration events
func (s *SpawnSystem) HandleEvent(event events.GameEvent) {
	switch event.Type {
	case events.EventSpawnChange:
		if payload, ok := event.Payload.(*events.SpawnChangePayload); ok {
			s.enabled = payload.Enabled
			s.statEnabled.Store(payload.Enabled)
		}

	case events.EventGameReset:
		s.Init()
		s.statRateMult.Set(1.0)
		s.statDensity.Set(0.0)
	}
}

// Update runs the spawn system logic
func (s *SpawnSystem) Update() {
	now := s.res.Time.GameTime
	config := s.res.Config

	// Check spawn enabled
	if !s.enabled {
		return
	}

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

	// Generate and spawn a new sequence
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
	if density < constants.SpawnDensityLowThreshold {
		s.rateMultiplier = constants.SpawnRateFast
	} else if density > constants.SpawnDensityHighThreshold {
		s.rateMultiplier = constants.SpawnRateSlow
	} else {
		s.rateMultiplier = constants.SpawnRateNormal
	}
}

// scheduleNextSpawn calculates and sets the next spawn time
func (s *SpawnSystem) scheduleNextSpawn() {
	now := s.res.Time.GameTime
	baseDelay := time.Duration(constants.SpawnIntervalMs) * time.Millisecond
	adjustedDelay := time.Duration(float64(baseDelay) / s.rateMultiplier)

	s.lastSpawnTime = now
	s.nextSpawnTime = now.Add(adjustedDelay)
}

// loadContentFromManager loads content using ContentManager
func (s *SpawnSystem) loadContentFromManager() {
	// Get random content block from ContentManager
	lines, _, err := s.contentManager.SelectRandomBlockWithValidation()
	if err != nil || len(lines) == 0 {
		// If no content available, use empty slice
		s.contentMutex.Lock()
		s.codeBlocks = []CodeBlock{}
		s.contentMutex.Unlock()

		s.totalBlocks.Store(0)
		s.nextBlockIndex.Store(0)
		s.blocksConsumed.Store(0)
		return
	}

	// Group the content lines into logical code blocks
	newBlocks := s.groupIntoBlocks(lines)

	// Atomically update content with write lock
	s.contentMutex.Lock()
	s.codeBlocks = newBlocks
	s.contentMutex.Unlock()

	// Update atomic counters
	s.totalBlocks.Store(int32(len(newBlocks)))
	s.nextBlockIndex.Store(0)
	s.blocksConsumed.Store(0)
}

// checkAndTriggerRefresh checks if content refresh is needed and triggers pre-fetch
func (s *SpawnSystem) checkAndTriggerRefresh() {
	// Read current state
	totalBlocks := s.totalBlocks.Load()
	blocksConsumed := s.blocksConsumed.Load()

	// Check if we're at the refresh threshold
	if totalBlocks == 0 {
		return
	}

	consumptionRatio := float64(blocksConsumed) / float64(totalBlocks)

	// If we've consumed 80% and not already refreshing, start pre-fetch
	if consumptionRatio >= constants.ContentRefreshThreshold && !s.isRefreshing.Load() {
		s.isRefreshing.Store(true)
		// Use core.Go for safe execution with centralized crash handling
		core.Go(s.preFetchNextContent)
	}
}

// preFetchNextContent loads next content batch in background
func (s *SpawnSystem) preFetchNextContent() {
	// Get new content from ContentManager
	lines, _, err := s.contentManager.SelectRandomBlockWithValidation()
	if err != nil || len(lines) == 0 {
		// Failed to get new content, will retry on next check
		s.isRefreshing.Store(false)
		return
	}

	// Group into blocks
	newBlocks := s.groupIntoBlocks(lines)

	// Store pre-fetched content
	s.contentMutex.Lock()
	s.nextContent = newBlocks
	s.contentMutex.Unlock()
}

// swapToNextContent performs content swap when current content is exhausted
func (s *SpawnSystem) swapToNextContent() {
	s.contentMutex.Lock()

	// Check if we have pre-fetched content
	if len(s.nextContent) > 0 {
		// Atomically swap to pre-fetched content
		s.codeBlocks = s.nextContent
		newTotalBlocks := int32(len(s.codeBlocks))
		s.nextContent = nil
		s.contentMutex.Unlock()

		// Update counters after releasing lock
		s.totalBlocks.Store(newTotalBlocks)
		s.nextBlockIndex.Store(0)
		s.blocksConsumed.Store(0)
		s.isRefreshing.Store(false)
	} else {
		// No pre-fetched content, need to load synchronously
		s.contentMutex.Unlock()
		s.loadContentFromManager()
	}
}

// groupIntoBlocks groups lines into logical code blocks
func (s *SpawnSystem) groupIntoBlocks(lines []string) []CodeBlock {
	if len(lines) == 0 {
		return []CodeBlock{}
	}

	var blocks []CodeBlock
	var currentBlock []string
	var currentIndent int
	var braceDepth int

	for _, line := range lines {
		indent := s.getIndentLevel(line)

		// Update brace depth
		braceDepth += strings.Count(line, "{")
		braceDepth -= strings.Count(line, "}")

		// Start new block if:
		// 1. Current block is empty (first line)
		// 2. Significant indent change (>= MinIndentChange) and brace depth is 0
		// 3. Current block reached max size
		shouldStartNewBlock := len(currentBlock) == 0 ||
			(len(currentBlock) >= constants.MaxBlockLines) ||
			(braceDepth == 0 && len(currentBlock) >= constants.MinBlockLines &&
				(indent < currentIndent-constants.MinIndentChange || indent > currentIndent+constants.MinIndentChange))

		if shouldStartNewBlock && len(currentBlock) > 0 {
			// Save current block if it meets minimum size
			if len(currentBlock) >= constants.MinBlockLines {
				blocks = append(blocks, CodeBlock{
					Lines:       currentBlock,
					IndentLevel: currentIndent,
					HasBraces:   s.hasBracesInBlock(currentBlock),
				})
			}
			currentBlock = []string{}
			currentIndent = indent
		}

		// Add line to current block
		currentBlock = append(currentBlock, line)
		if len(currentBlock) == 1 {
			currentIndent = indent
		}
	}

	// Add final block if it meets minimum size
	if len(currentBlock) >= constants.MinBlockLines {
		blocks = append(blocks, CodeBlock{
			Lines:       currentBlock,
			IndentLevel: currentIndent,
			HasBraces:   s.hasBracesInBlock(currentBlock),
		})
	}

	return blocks
}

// getIndentLevel counts leading spaces/tabs
func (s *SpawnSystem) getIndentLevel(line string) int {
	indent := 0
	for _, ch := range line {
		if ch == ' ' {
			indent++
		} else if ch == '\t' {
			indent += 4 // Count tabs as 4 spaces
		} else {
			break
		}
	}
	return indent
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

// runCensus iterates all sequence entities and counts colors
// Called once per spawn check, O(n) where n ≈ 200 max entities
func (s *SpawnSystem) runCensus() ColorCensus {
	var census ColorCensus

	entities := s.seqStore.All()
	for _, entity := range entities {
		seq, ok := s.seqStore.Get(entity)
		if !ok {
			continue
		}

		// Only count Blue and Green (Red and Gold excluded from 6-color limit)
		switch seq.Type {
		case components.SequenceBlue:
			switch seq.Level {
			case components.LevelBright:
				census.BlueBright++
			case components.LevelNormal:
				census.BlueNormal++
			case components.LevelDark:
				census.BlueDark++
			}
		case components.SequenceGreen:
			switch seq.Level {
			case components.LevelBright:
				census.GreenBright++
			case components.LevelNormal:
				census.GreenNormal++
			case components.LevelDark:
				census.GreenDark++
			}
			// SequenceRed, SequenceGold: intentionally not counted
		}
	}

	return census
}

// getAvailableColorsFromCensus returns color/level combinations not present on screen
func (s *SpawnSystem) getAvailableColorsFromCensus(census ColorCensus) []ColorLevelKey {
	available := make([]ColorLevelKey, 0, 6)

	if census.BlueBright == 0 {
		available = append(available, ColorLevelKey{
			Type: components.SequenceBlue, Level: components.LevelBright,
		})
	}
	if census.BlueNormal == 0 {
		available = append(available, ColorLevelKey{
			Type: components.SequenceBlue, Level: components.LevelNormal,
		})
	}
	if census.BlueDark == 0 {
		available = append(available, ColorLevelKey{
			Type: components.SequenceBlue, Level: components.LevelDark,
		})
	}
	if census.GreenBright == 0 {
		available = append(available, ColorLevelKey{
			Type: components.SequenceGreen, Level: components.LevelBright,
		})
	}
	if census.GreenNormal == 0 {
		available = append(available, ColorLevelKey{
			Type: components.SequenceGreen, Level: components.LevelNormal,
		})
	}
	if census.GreenDark == 0 {
		available = append(available, ColorLevelKey{
			Type: components.SequenceGreen, Level: components.LevelDark,
		})
	}

	return available
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

	// Check if we have code blocks (with read lock for thread safety)
	s.contentMutex.RLock()
	hasBlocks := len(s.codeBlocks) > 0
	s.contentMutex.RUnlock()

	if !hasBlocks {
		// No code blocks, can't spawn file-based blocks
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
	placedCount := 0
	for _, line := range block.Lines {
		if s.placeLine(line, seqType, seqLevel) {
			placedCount++
		}
	}
}

// getNextBlock retrieves the next logical code block with content refresh management
func (s *SpawnSystem) getNextBlock() CodeBlock {
	// Read block with lock to ensure consistency
	s.contentMutex.RLock()
	if len(s.codeBlocks) == 0 {
		s.contentMutex.RUnlock()
		return CodeBlock{Lines: []string{}}
	}

	// Get current index atomically
	currentIndex := int(s.nextBlockIndex.Load())
	totalBlocks := len(s.codeBlocks)

	// Bounds check
	if currentIndex >= totalBlocks {
		currentIndex = 0
		s.nextBlockIndex.Store(0)
	}

	block := s.codeBlocks[currentIndex]
	s.contentMutex.RUnlock()

	// Mutex protection of the section where index is updated and checked for wraparound
	// This prevents race conditions where blocksConsumed could exceed totalBlocks
	s.indexMutex.Lock()

	// Re-read totalBlocks inside critical section to ensure consistency
	currentTotal := int(s.totalBlocks.Load())
	currentConsumed := int(s.blocksConsumed.Load())

	// Check if we've already consumed all blocks (can happen during concurrent wraparound)
	if currentConsumed >= currentTotal && currentTotal > 0 {
		// Another thread is handling or has handled the swap, just return this block
		s.indexMutex.Unlock()
		return block
	}

	// Calculate next index
	currentIndex = int(s.nextBlockIndex.Load())
	nextIndex := (currentIndex + 1) % currentTotal

	// Atomically update index
	s.nextBlockIndex.Store(int32(nextIndex))

	// Atomically increment consumed counter
	newConsumed := s.blocksConsumed.Add(1)

	// Check if we've consumed all blocks
	needsSwap := int(newConsumed) >= currentTotal

	s.indexMutex.Unlock()

	// Perform operations outside the mutex to avoid holding lock during expensive operations
	if needsSwap {
		// We've consumed all blocks, swap to new content
		s.swapToNextContent()
	} else {
		// Check if we need to start pre-fetching
		s.checkAndTriggerRefresh()
	}

	return block
}

// placeLine attempts to place a single line on the screen using generic stores
// Lines exceeding GameWidth are cropped to fit available space
func (s *SpawnSystem) placeLine(line string, seqType components.SequenceType, seqLevel components.SequenceLevel) bool {
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
	for attempt := 0; attempt < constants.MaxPlacementTries; attempt++ {
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
			if math.Abs(float64(col-cursorPos.X)) <= constants.CursorExclusionX &&
				math.Abs(float64(row-cursorPos.Y)) <= constants.CursorExclusionY {
				hasOverlap = true
				break
			}
		}

		if !hasOverlap {
			// Valid position found, create entities
			// Get sequence ID from GameState
			// TODO: future migration to SystemStatus resource
			sequenceID := s.res.State.State.IncrementSeqID()

			// Phase 1: Create entities and prepare components
			type entityData struct {
				entity core.Entity
				pos    components.PositionComponent
				char   components.CharacterComponent
				seq    components.SequenceComponent
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
					pos: components.PositionComponent{
						X: startCol + i,
						Y: row,
					},
					char: components.CharacterComponent{
						Rune: lineRunes[i],
						// Color defaults to ColorNone (0), signaling renderer to use SeqType/SeqLevel
						Style:    components.StyleNormal,
						SeqType:  seqType,
						SeqLevel: seqLevel,
					},
					seq: components.SequenceComponent{
						ID:    sequenceID,
						Index: i,
						Type:  seqType,
						Level: seqLevel,
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
				s.seqStore.Add(ed.entity, ed.seq)
			}

			return true
		}
	}

	// Failed to place after MaxPlacementTries attempts
	return false
}