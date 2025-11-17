package systems

import (
	"math"
	"math/rand"
	"reflect"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

const (
	characterSpawnMs = 2000
	maxCharacters    = 200
)

// SpawnSystem handles character sequence generation and spawning
type SpawnSystem struct {
	lastSpawn  time.Time
	nextSeqID  int
	characters string
	gameWidth  int
	gameHeight int
	cursorX    int
	cursorY    int
	ctx        *engine.GameContext
}

// NewSpawnSystem creates a new spawn system
func NewSpawnSystem(gameWidth, gameHeight, cursorX, cursorY int, ctx *engine.GameContext) *SpawnSystem {
	return &SpawnSystem{
		lastSpawn:  ctx.TimeProvider.Now(),
		nextSeqID:  1,
		characters: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()_+-=[]{}|;:,.<>?/",
		gameWidth:  gameWidth,
		gameHeight: gameHeight,
		cursorX:    cursorX,
		cursorY:    cursorY,
		ctx:        ctx,
	}
}

// Priority returns the system's priority (lower runs first)
func (s *SpawnSystem) Priority() int {
	return 10 // Run early
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

// spawnSequence generates and spawns a new character sequence
func (s *SpawnSystem) spawnSequence(world *engine.World) {
	// Generate sequence length (1-10 characters)
	seqLength := rand.Intn(10) + 1

	// Generate the sequence of runes
	sequence := make([]rune, seqLength)
	for i := 0; i < seqLength; i++ {
		sequence[i] = rune(s.characters[rand.Intn(len(s.characters))])
	}

	// Randomly assign sequence type (only Green or Blue, Red comes from decay)
	// rand.Intn(2) gives 0 or 1, map to Green (0) or Blue (2)
	typeChoice := rand.Intn(2)
	var seqType components.SequenceType
	if typeChoice == 0 {
		seqType = components.SequenceGreen
	} else {
		seqType = components.SequenceBlue
	}
	seqLevel := components.SequenceLevel(rand.Intn(3))

	// Get style for this sequence
	style := render.GetStyleForSequence(seqType, seqLevel)

	// Find valid position
	x, y := s.findValidPosition(world, seqLength)
	if x < 0 || y < 0 {
		return // No valid position found
	}

	// Create sequence ID
	sequenceID := s.nextSeqID
	s.nextSeqID++

	// Create entities for each character in sequence
	for i := 0; i < seqLength; i++ {
		entity := world.CreateEntity()

		// Add position component
		world.AddComponent(entity, components.PositionComponent{
			X: x + i,
			Y: y,
		})

		// Add character component
		world.AddComponent(entity, components.CharacterComponent{
			Rune:  sequence[i],
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
		world.UpdateSpatialIndex(entity, x+i, y)
	}
}

// findValidPosition finds a valid position for a sequence of given length
func (s *SpawnSystem) findValidPosition(world *engine.World, seqLength int) (int, int) {
	maxAttempts := 100
	for attempt := 0; attempt < maxAttempts; attempt++ {
		x := rand.Intn(s.gameWidth)
		y := rand.Intn(s.gameHeight)

		// Check if far enough from cursor
		if math.Abs(float64(x-s.cursorX)) <= 5 || math.Abs(float64(y-s.cursorY)) <= 3 {
			continue
		}

		// Check if sequence fits within game width
		if x+seqLength > s.gameWidth {
			continue
		}

		// Check for overlaps with existing characters
		overlaps := false
		for i := 0; i < seqLength; i++ {
			if world.GetEntityAtPosition(x+i, y) != 0 {
				overlaps = true
				break
			}
		}

		if !overlaps {
			return x, y
		}
	}

	return -1, -1 // No valid position found
}

// UpdateDimensions updates the game area dimensions
func (s *SpawnSystem) UpdateDimensions(gameWidth, gameHeight, cursorX, cursorY int) {
	s.gameWidth = gameWidth
	s.gameHeight = gameHeight
	s.cursorX = cursorX
	s.cursorY = cursorY
}
