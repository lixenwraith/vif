package systems

import (
	"bufio"
	"math"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

const (
	characterSpawnMs = 2000
	maxCharacters    = 200
	dataFilePath     = "./assets/data.txt"
	minBlockLines    = 5
	maxBlockLines    = 10
	maxPlacementTries = 3
)

// ColorLevelKey represents a unique color+level combination
type ColorLevelKey struct {
	Type  components.SequenceType
	Level components.SequenceLevel
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

	// File-based content
	fileLines []string
	nextLineIndex int

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
		lastSpawn:  ctx.TimeProvider.Now(),
		nextSeqID:  1,
		gameWidth:  gameWidth,
		gameHeight: gameHeight,
		cursorX:    cursorX,
		cursorY:    cursorY,
		ctx:        ctx,
		nextLineIndex: 0,
	}

	// Load file content
	s.loadFileContent()

	// Initialize atomic counters to 0
	s.blueCountBright.Store(0)
	s.blueCountNormal.Store(0)
	s.blueCountDark.Store(0)
	s.greenCountBright.Store(0)
	s.greenCountNormal.Store(0)
	s.greenCountDark.Store(0)

	return s
}

// loadFileContent loads and parses the data file
func (s *SpawnSystem) loadFileContent() {
	file, err := os.Open(dataFilePath)
	if err != nil {
		// If file doesn't exist or can't be read, use empty slice
		// System will gracefully handle this by not spawning file-based blocks
		s.fileLines = []string{}
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	s.fileLines = []string{}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Include non-empty lines
		if len(line) > 0 {
			s.fileLines = append(s.fileLines, line)
		}
	}
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

	// Check if we have file content
	if len(s.fileLines) == 0 {
		// No file content, can't spawn file-based blocks
		return
	}

	// Select random available color
	colorKey := availableColors[rand.Intn(len(availableColors))]
	seqType := colorKey.Type
	seqLevel := colorKey.Level

	// Get style for this sequence
	style := render.GetStyleForSequence(seqType, seqLevel)

	// Select random block size (5-10 lines)
	blockSize := rand.Intn(maxBlockLines-minBlockLines+1) + minBlockLines

	// Get block of lines from file (wrap around if needed)
	blockLines := s.getNextBlock(blockSize)
	if len(blockLines) == 0 {
		return
	}

	// Try to place each line on the screen
	placedCount := 0
	for _, line := range blockLines {
		if s.placeLine(world, line, seqType, seqLevel, style) {
			placedCount++
		}
	}
}

// getNextBlock retrieves the next block of lines from the file
func (s *SpawnSystem) getNextBlock(blockSize int) []string {
	if len(s.fileLines) == 0 {
		return []string{}
	}

	block := []string{}
	for i := 0; i < blockSize; i++ {
		block = append(block, s.fileLines[s.nextLineIndex])
		s.nextLineIndex = (s.nextLineIndex + 1) % len(s.fileLines)
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

			for i := 0; i < lineLength; i++ {
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
			}

			// Atomically increment the color counter
			s.AddColorCount(seqType, seqLevel, int64(lineLength))

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
