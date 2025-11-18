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
type SpawnSystem struct {
	lastSpawn  time.Time
	nextSeqID  int
	gameWidth  int
	gameHeight int
	cursorX    int
	cursorY    int
	ctx        *engine.GameContext

	// Content management
	contentManager *content.ContentManager
	contentMutex   sync.RWMutex
	codeBlocks     []CodeBlock
	nextBlockIndex int
	totalBlocks    int
	blocksConsumed int
	nextContent    []CodeBlock // Pre-fetched content for seamless transition
	isRefreshing   atomic.Bool

	// Atomic color tracking counters (6 states: Blue×3 + Green×3)
	blueCountBright  atomic.Int64
	blueCountNormal  atomic.Int64
	blueCountDark    atomic.Int64
	greenCountBright atomic.Int64
	greenCountNormal atomic.Int64
	greenCountDark   atomic.Int64
}

// NewSpawnSystem creates a new spawn system
func NewSpawnSystem(gameWidth, gameHeight, cursorX, cursorY int, ctx *engine.GameContext) *SpawnSystem {
	s := &SpawnSystem{
		lastSpawn:      ctx.TimeProvider.Now(),
		nextSeqID:      1,
		gameWidth:      gameWidth,
		gameHeight:     gameHeight,
		cursorX:        cursorX,
		cursorY:        cursorY,
		ctx:            ctx,
		nextBlockIndex: 0,
		blocksConsumed: 0,
	}

	// Initialize ContentManager
	s.contentManager = content.NewContentManager()
	if err := s.contentManager.DiscoverContentFiles(); err != nil {
		// Log error but continue with empty content
		// System will handle gracefully
	}

	// Load initial content
	s.loadContentFromManager()

	// Initialize atomic counters to 0
	s.blueCountBright.Store(0)
	s.blueCountNormal.Store(0)
	s.blueCountDark.Store(0)
	s.greenCountBright.Store(0)
	s.greenCountNormal.Store(0)
	s.greenCountDark.Store(0)
	s.isRefreshing.Store(false)

	return s
}

// loadContentFromManager loads content using ContentManager
func (s *SpawnSystem) loadContentFromManager() {
	// Get random content block from ContentManager
	lines, _, err := s.contentManager.SelectRandomBlockWithValidation()
	if err != nil || len(lines) == 0 {
		// If no content available, use empty slice
		// System will gracefully handle this by not spawning file-based blocks
		s.codeBlocks = []CodeBlock{}
		s.totalBlocks = 0
		return
	}

	// Group the content lines into logical code blocks
	s.contentMutex.Lock()
	s.codeBlocks = s.groupIntoBlocks(lines)
	s.totalBlocks = len(s.codeBlocks)
	s.nextBlockIndex = 0
	s.blocksConsumed = 0
	s.contentMutex.Unlock()
}

// checkAndTriggerRefresh checks if content refresh is needed and triggers pre-fetch
func (s *SpawnSystem) checkAndTriggerRefresh() {
	// Check if we're at the refresh threshold
	if s.totalBlocks == 0 {
		return
	}

	consumptionRatio := float64(s.blocksConsumed) / float64(s.totalBlocks)

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
	defer s.contentMutex.Unlock()

	// Check if we have pre-fetched content
	if len(s.nextContent) > 0 {
		// Swap to pre-fetched content
		s.codeBlocks = s.nextContent
		s.totalBlocks = len(s.codeBlocks)
		s.nextBlockIndex = 0
		s.blocksConsumed = 0
		s.nextContent = nil
		s.isRefreshing.Store(false)
	} else {
		// No pre-fetched content, load new content synchronously
		s.contentMutex.Unlock() // Unlock before calling loadContentFromManager
		s.loadContentFromManager()
		s.contentMutex.Lock() // Re-lock before returning
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
	return 10 // Run early
}

// GetColorCount returns the character count for a specific color/level combination
func (s *SpawnSystem) GetColorCount(seqType components.SequenceType, level components.SequenceLevel) int64 {
	switch seqType {
	case components.SequenceBlue:
		switch level {
		case components.LevelBright:
			return s.blueCountBright.Load()
		case components.LevelNormal:
			return s.blueCountNormal.Load()
		case components.LevelDark:
			return s.blueCountDark.Load()
		}
	case components.SequenceGreen:
		switch level {
		case components.LevelBright:
			return s.greenCountBright.Load()
		case components.LevelNormal:
			return s.greenCountNormal.Load()
		case components.LevelDark:
			return s.greenCountDark.Load()
		}
	}
	return 0
}

// AddColorCount atomically increments the counter for a color/level
func (s *SpawnSystem) AddColorCount(seqType components.SequenceType, level components.SequenceLevel, delta int64) {
	switch seqType {
	case components.SequenceBlue:
		switch level {
		case components.LevelBright:
			s.blueCountBright.Add(delta)
		case components.LevelNormal:
			s.blueCountNormal.Add(delta)
		case components.LevelDark:
			s.blueCountDark.Add(delta)
		}
	case components.SequenceGreen:
		switch level {
		case components.LevelBright:
			s.greenCountBright.Add(delta)
		case components.LevelNormal:
			s.greenCountNormal.Add(delta)
		case components.LevelDark:
			s.greenCountDark.Add(delta)
		}
	}
}

// getAvailableColors returns colors that are not yet on screen
func (s *SpawnSystem) getAvailableColors() []ColorLevelKey {
	available := []ColorLevelKey{}

	// Check all 6 combinations
	colors := []struct {
		Type  components.SequenceType
		Level components.SequenceLevel
	}{
		{components.SequenceBlue, components.LevelBright},
		{components.SequenceBlue, components.LevelNormal},
		{components.SequenceBlue, components.LevelDark},
		{components.SequenceGreen, components.LevelBright},
		{components.SequenceGreen, components.LevelNormal},
		{components.SequenceGreen, components.LevelDark},
	}

	for _, c := range colors {
		if s.GetColorCount(c.Type, c.Level) == 0 {
			available = append(available, ColorLevelKey{Type: c.Type, Level: c.Level})
		}
	}

	return available
}

// Update runs the spawn system logic
func (s *SpawnSystem) Update(world *engine.World, dt time.Duration) {
	// Calculate fill percentage
	posType := reflect.TypeOf(components.PositionComponent{})
	entities := world.GetEntitiesWith(posType)
	totalCells := s.gameWidth * s.gameHeight
	filledCells := len(entities)
	if filledCells > maxCharacters {
		return // Already at max capacity
	}

	fillPercentage := float64(filledCells) / float64(totalCells)

	// Adjust spawn rate based on fill percentage
	var spawnDelay int64
	if fillPercentage < 0.30 {
		spawnDelay = characterSpawnMs / 2 // 2x faster
	} else if fillPercentage > 0.70 {
		spawnDelay = characterSpawnMs * 2 // 2x slower
	} else {
		spawnDelay = characterSpawnMs
	}

	// Check if it's time to spawn
	now := s.ctx.TimeProvider.Now()
	if now.Sub(s.lastSpawn).Milliseconds() <= spawnDelay {
		return
	}

	// Generate and spawn a new sequence
	s.spawnSequence(world)
	s.lastSpawn = now
}

// spawnSequence generates and spawns a new character block from file
func (s *SpawnSystem) spawnSequence(world *engine.World) {
	// Check if we have any available colors (less than 6 colors on screen)
	availableColors := s.getAvailableColors()
	if len(availableColors) == 0 {
		// All 6 color combinations are present, don't spawn
		return
	}

	// Check if we have code blocks
	if len(s.codeBlocks) == 0 {
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
	s.contentMutex.RLock()
	if len(s.codeBlocks) == 0 {
		s.contentMutex.RUnlock()
		return CodeBlock{Lines: []string{}}
	}

	block := s.codeBlocks[s.nextBlockIndex]
	currentIndex := s.nextBlockIndex
	s.contentMutex.RUnlock()

	// Update index and consumption counter (thread-safe write)
	s.contentMutex.Lock()
	s.nextBlockIndex = (currentIndex + 1) % len(s.codeBlocks)

	// Check if we've wrapped around (consumed all blocks)
	if s.nextBlockIndex == 0 {
		// We've consumed all blocks, swap to new content
		s.contentMutex.Unlock()
		s.swapToNextContent()
	} else {
		s.blocksConsumed++
		s.contentMutex.Unlock()

		// Check if we need to start pre-fetching
		s.checkAndTriggerRefresh()
	}

	return block
}

// placeLine attempts to place a single line on the screen
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

		// Check if too close to cursor
		for i := 0; i < lineLength; i++ {
			col := startCol + i
			if math.Abs(float64(col-s.cursorX)) <= 5 && math.Abs(float64(row-s.cursorY)) <= 3 {
				hasOverlap = true
				break
			}
		}

		if !hasOverlap {
			// Valid position found, create entities
			sequenceID := s.nextSeqID
			s.nextSeqID++

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
