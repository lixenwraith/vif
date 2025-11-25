package systems

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/content"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

const (
	spawnIntervalMs         = 2000
	maxEntities             = 200
	minBlockLines           = 3
	maxBlockLines           = 15
	maxPlacementTries       = 3
	minIndentChange         = 2   // Minimum indent change to start new block
	contentRefreshThreshold = 0.8 // Refresh when 80% of content consumed
)

// ColorLevelKey represents a unique color+level combination
type ColorLevelKey struct {
	Type  components.SequenceType
	Level components.SequenceLevel
}

// ColorCensus holds entity counts for each color/level combination.
// Used for 6-color spawn limit enforcement.
type ColorCensus struct {
	BlueBright  int
	BlueNormal  int
	BlueDark    int
	GreenBright int
	GreenNormal int
	GreenDark   int
}

// Total returns sum of all tracked colors.
func (c ColorCensus) Total() int {
	return c.BlueBright + c.BlueNormal + c.BlueDark +
		c.GreenBright + c.GreenNormal + c.GreenDark
}

// ActiveColors returns count of non-zero color/level combinations.
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
	ctx *engine.GameContext

	// Content management (implementation detail, not game state)
	contentManager *content.ContentManager
	contentMutex   sync.RWMutex
	indexMutex     sync.Mutex // Mutex to protect index update and wraparound operations
	codeBlocks     []CodeBlock
	nextBlockIndex atomic.Int32 // Atomic index for thread-safe access
	totalBlocks    atomic.Int32 // Atomic counter for total blocks
	blocksConsumed atomic.Int32 // Atomic counter for consumed blocks
	nextContent    []CodeBlock  // Pre-fetched content for seamless transition
	isRefreshing   atomic.Bool
}

// NewSpawnSystem creates a new spawn system
func NewSpawnSystem(ctx *engine.GameContext) *SpawnSystem {
	s := &SpawnSystem{
		ctx: ctx,
	}

	// Initialize content management atomics (not game state)
	s.isRefreshing.Store(false)
	s.nextBlockIndex.Store(0)
	s.totalBlocks.Store(0)
	s.blocksConsumed.Store(0)

	// Initialize ContentManager
	s.contentManager = content.NewContentManager()
	if err := s.contentManager.DiscoverContentFiles(); err != nil {
		// Log error but continue with empty content
		// System will handle gracefully
	}

	// Pre-validate all discovered content files and build cache
	// This ensures content is validated once at startup for better performance
	// and prevents corruption from malformed files
	if err := s.contentManager.PreValidateAllContent(); err != nil {
		// Log error but continue - will fall back to default content
		// System will handle gracefully
	}

	// Load initial content
	s.loadContentFromManager()

	return s
}

// loadContentFromManager loads content using ContentManager
func (s *SpawnSystem) loadContentFromManager() {
	// Get random content block from ContentManager
	lines, _, err := s.contentManager.SelectRandomBlockWithValidation()
	if err != nil || len(lines) == 0 {
		// If no content available, use empty slice
		// System will gracefully handle this by not spawning file-based blocks
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
	// Read current state using atomic operations
	totalBlocks := s.totalBlocks.Load()
	blocksConsumed := s.blocksConsumed.Load()

	// Check if we're at the refresh threshold
	if totalBlocks == 0 {
		return
	}

	consumptionRatio := float64(blocksConsumed) / float64(totalBlocks)

	// If we've consumed 80% and not already refreshing, start pre-fetch
	if consumptionRatio >= contentRefreshThreshold && !s.isRefreshing.Load() {
		s.isRefreshing.Store(true)
		go s.preFetchNextContent()
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

// swapToNextContent performs thread-safe content swap when current content is exhausted
func (s *SpawnSystem) swapToNextContent() {
	s.contentMutex.Lock()

	// Check if we have pre-fetched content
	if len(s.nextContent) > 0 {
		// Atomically swap to pre-fetched content
		s.codeBlocks = s.nextContent
		newTotalBlocks := int32(len(s.codeBlocks))
		s.nextContent = nil
		s.contentMutex.Unlock()

		// Update atomic counters after releasing lock
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
		// 2. Significant indent change (>= minIndentChange) and brace depth is 0
		// 3. Current block reached max size
		shouldStartNewBlock := len(currentBlock) == 0 ||
			(len(currentBlock) >= maxBlockLines) ||
			(braceDepth == 0 && len(currentBlock) >= minBlockLines &&
				(indent < currentIndent-minIndentChange || indent > currentIndent+minIndentChange))

		if shouldStartNewBlock && len(currentBlock) > 0 {
			// Save current block if it meets minimum size
			if len(currentBlock) >= minBlockLines {
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
	if len(currentBlock) >= minBlockLines {
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

// Priority returns the system's priority (lower runs first)
func (s *SpawnSystem) Priority() int {
	return 15 // Run early
}

// runCensus iterates all sequence entities and counts colors.
// O(n) where n ≈ 200 max entities. Called once per spawn check.
func (s *SpawnSystem) runCensus(world *engine.World) ColorCensus {
	var census ColorCensus

	entities := world.Sequences.All()
	for _, entity := range entities {
		seq, ok := world.Sequences.Get(entity)
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

// getAvailableColorsFromCensus returns color/level combinations not present on screen.
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

// Update runs the spawn system logic
func (s *SpawnSystem) Update(world *engine.World, dt time.Duration) {
	// Fetch resources
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)

	// Calculate fill percentage and update GameState using generic stores

	entityCount := world.Positions.Count()

	if entityCount > maxEntities {
		return // Already at max capacity
	}

	// Update spawn rate in GameState based on entity count
	s.ctx.State.UpdateSpawnRate(entityCount, maxEntities)

	// Check if it's time to spawn (reads from GameState)
	if !s.ctx.State.ShouldSpawn() {
		return
	}

	// Get current spawn state for rate calculation
	spawnState := s.ctx.State.ReadSpawnState()

	// Calculate next spawn time based on rate multiplier
	baseDelay := time.Duration(spawnIntervalMs) * time.Millisecond
	adjustedDelay := time.Duration(float64(baseDelay) / spawnState.RateMultiplier)

	// Generate and spawn a new sequence
	s.spawnSequence(world)

	// Update spawn timing in GameState
	now := timeRes.GameTime
	nextTime := now.Add(adjustedDelay)
	s.ctx.State.UpdateSpawnTiming(now, nextTime)
}

// spawnSequence generates and spawns a new character block from file
func (s *SpawnSystem) spawnSequence(world *engine.World) {
	// CHANGED: Use census instead of atomic counters
	census := s.runCensus(world)
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

	// Get style for this sequence
	style := render.GetStyleForSequence(seqType, seqLevel)

	// Get next logical code block
	block := s.getNextBlock()
	if len(block.Lines) == 0 {
		return
	}

	// Try to place each line from the block on the screen
	placedCount := 0
	for _, line := range block.Lines {
		if s.placeLine(world, line, seqType, seqLevel, style) {
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

	// Use mutex to protect the critical section where we update index and check for wraparound
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
func (s *SpawnSystem) placeLine(world *engine.World, line string, seqType components.SequenceType, seqLevel components.SequenceLevel, style tcell.Style) bool {
	lineRunes := []rune(line)
	lineLength := len(lineRunes)

	if lineLength == 0 {
		return false
	}

	// Fetch dimensions from resources
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)

	// Try up to maxPlacementTries times to find a valid position
	for attempt := 0; attempt < maxPlacementTries; attempt++ {
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
			continue
		}

		startCol := rand.Intn(maxStartCol + 1)

		// Check for overlaps using generic position store
		hasOverlap := false
		for i := 0; i < lineLength; i++ {
			if world.Positions.GetEntityAt(startCol+i, row) != 0 {
				hasOverlap = true
				break
			}
		}

		// Check if too close to cursor (read from ECS)
		cursorPos, ok := s.ctx.World.Positions.Get(s.ctx.CursorEntity)
		if !ok {
			panic(fmt.Errorf("cursor destroyed"))
		}
		for i := 0; i < lineLength; i++ {
			col := startCol + i
			if math.Abs(float64(col-cursorPos.X)) <= 5 && math.Abs(float64(row-cursorPos.Y)) <= 3 {
				hasOverlap = true
				break
			}
		}

		if !hasOverlap {
			// Valid position found, create entities using generic stores
			// Get sequence ID from GameState (atomic increment)
			sequenceID := s.ctx.State.IncrementSeqID()

			// Phase 1: Create entities and prepare components
			type entityData struct {
				entity engine.Entity
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

				entity := world.CreateEntity()
				entities = append(entities, entityData{
					entity: entity,
					pos: components.PositionComponent{
						X: startCol + i,
						Y: row,
					},
					char: components.CharacterComponent{
						Rune:  lineRunes[i],
						Style: style,
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
			batch := world.Positions.BeginBatch()
			for _, ed := range entities {
				batch.Add(ed.entity, ed.pos)
			}

			if err := batch.Commit(); err != nil {
				// Collision detected - cleanup entities and try next attempt
				for _, ed := range entities {
					world.DestroyEntity(ed.entity)
				}
				continue
			}

			// Phase 3: Add other components (positions already committed)
			for _, ed := range entities {
				world.Characters.Add(ed.entity, ed.char)
				world.Sequences.Add(ed.entity, ed.seq)
			}

			return true
		}
	}

	// Failed to place after maxPlacementTries attempts
	return false
}