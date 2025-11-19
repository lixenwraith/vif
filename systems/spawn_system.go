package systems

import (
	"math"
	"math/rand"
	"reflect"
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
	characterSpawnMs = 2000
	maxCharacters    = 200
	minBlockLines    = 3
	maxBlockLines    = 15
	maxPlacementTries = 3
	minIndentChange  = 2   // Minimum indent change to start new block
	contentRefreshThreshold = 0.8 // Refresh when 80% of content consumed
)

// ColorLevelKey represents a unique color+level combination
type ColorLevelKey struct {
	Type  components.SequenceType
	Level components.SequenceLevel
}

// CodeBlock represents a logical group of related lines
type CodeBlock struct {
	Lines       []string
	IndentLevel int
	HasBraces   bool
}

// SpawnSystem handles character sequence generation and spawning
// State management delegated to GameState (spawn timing, color counters, sequence IDs)
type SpawnSystem struct {
	gameWidth  int
	gameHeight int
	cursorX    int // Cached for exclusion zone, synced from ctx.State
	cursorY    int // Cached for exclusion zone, synced from ctx.State
	ctx        *engine.GameContext

	// Content management (implementation detail, not game state)
	contentManager *content.ContentManager
	contentMutex   sync.RWMutex
	indexMutex     sync.Mutex   // Mutex to protect index update and wraparound operations
	codeBlocks     []CodeBlock
	nextBlockIndex atomic.Int32 // Atomic index for thread-safe access
	totalBlocks    atomic.Int32 // Atomic counter for total blocks
	blocksConsumed atomic.Int32 // Atomic counter for consumed blocks
	nextContent    []CodeBlock  // Pre-fetched content for seamless transition
	isRefreshing   atomic.Bool
}

// NewSpawnSystem creates a new spawn system
// State is now managed in ctx.State (spawn timing, color counters, sequence IDs)
func NewSpawnSystem(gameWidth, gameHeight, cursorX, cursorY int, ctx *engine.GameContext) *SpawnSystem {
	s := &SpawnSystem{
		gameWidth:  gameWidth,
		gameHeight: gameHeight,
		cursorX:    cursorX,
		cursorY:    cursorY,
		ctx:        ctx,
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

// GetColorCount returns the character count for a specific color/level combination
// Delegated to GameState
func (s *SpawnSystem) GetColorCount(seqType components.SequenceType, level components.SequenceLevel) int64 {
	// Convert components.SequenceType to int for GameState
	var typeInt int
	if seqType == components.SequenceBlue {
		typeInt = 0
	} else if seqType == components.SequenceGreen {
		typeInt = 1
	} else {
		return 0
	}

	// Convert components.SequenceLevel to int for GameState
	levelInt := int(level) // 0=Dark, 1=Normal, 2=Bright

	// Read from GameState atomically
	switch typeInt {
	case 0: // Blue
		switch levelInt {
		case 2: // Bright
			return s.ctx.State.BlueCountBright.Load()
		case 1: // Normal
			return s.ctx.State.BlueCountNormal.Load()
		case 0: // Dark
			return s.ctx.State.BlueCountDark.Load()
		}
	case 1: // Green
		switch levelInt {
		case 2: // Bright
			return s.ctx.State.GreenCountBright.Load()
		case 1: // Normal
			return s.ctx.State.GreenCountNormal.Load()
		case 0: // Dark
			return s.ctx.State.GreenCountDark.Load()
		}
	}
	return 0
}

// AddColorCount atomically increments the counter for a color/level
// Delegated to GameState
func (s *SpawnSystem) AddColorCount(seqType components.SequenceType, level components.SequenceLevel, delta int64) {
	// Convert components.SequenceType to int for GameState
	var typeInt int
	if seqType == components.SequenceBlue {
		typeInt = 0
	} else if seqType == components.SequenceGreen {
		typeInt = 1
	} else {
		return
	}

	// Convert components.SequenceLevel to int for GameState
	levelInt := int(level) // 0=Dark, 1=Normal, 2=Bright

	// Update GameState atomically
	s.ctx.State.AddColorCount(typeInt, levelInt, int(delta))
}

// getAvailableColors returns colors that are not yet on screen
// Uses ReadColorCounts() snapshot for atomic 6-color limit check
func (s *SpawnSystem) getAvailableColors() []ColorLevelKey {
	available := []ColorLevelKey{}

	// Read color counts snapshot for consistent view of all 6 combinations
	counts := s.ctx.State.ReadColorCounts()

	// Check all 6 combinations using snapshot (ensures atomicity)
	if counts.BlueBright == 0 {
		available = append(available, ColorLevelKey{Type: components.SequenceBlue, Level: components.LevelBright})
	}
	if counts.BlueNormal == 0 {
		available = append(available, ColorLevelKey{Type: components.SequenceBlue, Level: components.LevelNormal})
	}
	if counts.BlueDark == 0 {
		available = append(available, ColorLevelKey{Type: components.SequenceBlue, Level: components.LevelDark})
	}
	if counts.GreenBright == 0 {
		available = append(available, ColorLevelKey{Type: components.SequenceGreen, Level: components.LevelBright})
	}
	if counts.GreenNormal == 0 {
		available = append(available, ColorLevelKey{Type: components.SequenceGreen, Level: components.LevelNormal})
	}
	if counts.GreenDark == 0 {
		available = append(available, ColorLevelKey{Type: components.SequenceGreen, Level: components.LevelDark})
	}

	return available
}

// Update runs the spawn system logic
// Now uses GameState for spawn timing and rate adaptation
func (s *SpawnSystem) Update(world *engine.World, dt time.Duration) {
	// Calculate fill percentage and update GameState
	posType := reflect.TypeOf(components.PositionComponent{})
	entities := world.GetEntitiesWith(posType)
	entityCount := len(entities)

	if entityCount > maxCharacters {
		return // Already at max capacity
	}

	// Update spawn rate in GameState based on entity count
	s.ctx.State.UpdateSpawnRate(entityCount, maxCharacters)

	// Check if it's time to spawn (reads from GameState)
	if !s.ctx.State.ShouldSpawn() {
		return
	}

	// Get current spawn state for rate calculation
	spawnState := s.ctx.State.ReadSpawnState()

	// Calculate next spawn time based on rate multiplier
	baseDelay := time.Duration(characterSpawnMs) * time.Millisecond
	adjustedDelay := time.Duration(float64(baseDelay) / spawnState.RateMultiplier)

	// Generate and spawn a new sequence
	s.spawnSequence(world)

	// Update spawn timing in GameState
	now := s.ctx.TimeProvider.Now()
	nextTime := now.Add(adjustedDelay)
	s.ctx.State.UpdateSpawnTiming(now, nextTime)
}

// spawnSequence generates and spawns a new character block from file
func (s *SpawnSystem) spawnSequence(world *engine.World) {
	// Check if we have any available colors (less than 6 colors on screen)
	availableColors := s.getAvailableColors()
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

// placeLine attempts to place a single line on the screen
// Thread-safety: This method creates multiple entities for a single line.
// While individual World operations are thread-safe (via mutex), the entire
// sequence of entity creation is not atomic. Callers should ensure this method
// is not called concurrently with rendering to avoid seeing partially-created entities.
func (s *SpawnSystem) placeLine(world *engine.World, line string, seqType components.SequenceType, seqLevel components.SequenceLevel, style tcell.Style) bool {
	lineRunes := []rune(line)
	lineLength := len(lineRunes)

	if lineLength == 0 {
		return false
	}

	// Try up to maxPlacementTries times to find a valid position
	for attempt := 0; attempt < maxPlacementTries; attempt++ {
		// Random row selection
		row := rand.Intn(s.gameHeight)

		// Check if line fits and find available columns
		if lineLength > s.gameWidth {
			// Line too long for screen, skip
			continue
		}

		// Random column selection (must have room for full line)
		maxStartCol := s.gameWidth - lineLength
		if maxStartCol < 0 {
			continue
		}

		startCol := rand.Intn(maxStartCol + 1)

		// Check for overlaps
		hasOverlap := false
		for i := 0; i < lineLength; i++ {
			if world.GetEntityAtPosition(startCol+i, row) != 0 {
				hasOverlap = true
				break
			}
		}

		// Check if too close to cursor (use snapshot for consistent position)
		cursor := s.ctx.State.ReadCursorPosition()
		for i := 0; i < lineLength; i++ {
			col := startCol + i
			if math.Abs(float64(col-cursor.X)) <= 5 && math.Abs(float64(row-cursor.Y)) <= 3 {
				hasOverlap = true
				break
			}
		}

		if !hasOverlap {
			// Valid position found, create entities atomically
			// Get sequence ID from GameState (atomic increment)
			sequenceID := s.ctx.State.IncrementSeqID()

			// Count non-space characters for color counter
			nonSpaceCount := 0

			for i := 0; i < lineLength; i++ {
				// Skip space characters - don't create entities for them
				if lineRunes[i] == ' ' {
					continue
				}

				entity := world.CreateEntity()

				// Add position component
				world.AddComponent(entity, components.PositionComponent{
					X: startCol + i,
					Y: row,
				})

				// Add character component
				world.AddComponent(entity, components.CharacterComponent{
					Rune:  lineRunes[i],
					Style: style,
				})

				// Add sequence component
				world.AddComponent(entity, components.SequenceComponent{
					ID:    sequenceID,
					Index: i,
					Type:  seqType,
					Level: seqLevel,
				})

				// Update spatial index
				world.UpdateSpatialIndex(entity, startCol+i, row)

				// Increment non-space character count
				nonSpaceCount++
			}

			// Atomically increment the color counter (only non-space characters)
			s.AddColorCount(seqType, seqLevel, int64(nonSpaceCount))

			return true
		}
	}

	// Failed to place after maxPlacementTries attempts
	return false
}

// UpdateDimensions updates the game area dimensions
func (s *SpawnSystem) UpdateDimensions(gameWidth, gameHeight, cursorX, cursorY int) {
	s.gameWidth = gameWidth
	s.gameHeight = gameHeight
	s.cursorX = cursorX
	s.cursorY = cursorY
}
