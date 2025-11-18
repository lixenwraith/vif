package systems

import (
	"reflect"
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// CleanerSystem manages the cleaner animation triggered when gold sequences are completed at max heat.
// Cleaners are bright yellow blocks that sweep across rows containing Red characters, removing them
// on contact while leaving Blue/Green characters unaffected.
type CleanerSystem struct {
	ctx               *engine.GameContext
	mu                sync.RWMutex
	isActive          bool
	activationTime    time.Time
	lastUpdateTime    time.Time
	gameWidth         int
	gameHeight        int
	animationDuration time.Duration
}

// NewCleanerSystem creates a new cleaner system
func NewCleanerSystem(ctx *engine.GameContext, gameWidth, gameHeight int) *CleanerSystem {
	return &CleanerSystem{
		ctx:               ctx,
		isActive:          false,
		gameWidth:         gameWidth,
		gameHeight:        gameHeight,
		animationDuration: constants.CleanerAnimationDuration,
	}
}

// Priority returns the system's priority (runs after decay system)
func (cs *CleanerSystem) Priority() int {
	return 35
}

// Update runs the cleaner system logic
func (cs *CleanerSystem) Update(world *engine.World, dt time.Duration) {
	cs.mu.RLock()
	active := cs.isActive
	cs.mu.RUnlock()

	if !active {
		return
	}

	now := cs.ctx.TimeProvider.Now()

	// Initialize last update time on first frame
	cs.mu.Lock()
	if cs.lastUpdateTime.IsZero() {
		cs.lastUpdateTime = now
	}
	cs.mu.Unlock()

	// Calculate elapsed time since animation started
	cs.mu.RLock()
	elapsed := now.Sub(cs.activationTime)
	cs.mu.RUnlock()

	// Check if animation is complete
	if elapsed >= cs.animationDuration {
		cs.cleanupCleaners(world)
		cs.mu.Lock()
		cs.isActive = false
		cs.lastUpdateTime = time.Time{}
		cs.mu.Unlock()
		return
	}

	// Update cleaner positions and check for collisions
	cs.updateCleanerPositions(world, now)
	cs.detectAndDestroyRedCharacters(world)

	cs.mu.Lock()
	cs.lastUpdateTime = now
	cs.mu.Unlock()
}

// TriggerCleaners initiates the cleaner animation
func (cs *CleanerSystem) TriggerCleaners(world *engine.World) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Prevent duplicate triggers
	if cs.isActive {
		return
	}

	// Scan for rows with Red characters
	redRows := cs.scanRedCharacterRows(world)

	if len(redRows) == 0 {
		// No Red characters to clean
		return
	}

	// Spawn cleaner entities for each row
	now := cs.ctx.TimeProvider.Now()
	for _, row := range redRows {
		cs.spawnCleanerForRow(world, row, now)
	}

	cs.isActive = true
	cs.activationTime = now
	cs.lastUpdateTime = now
}

// IsActive returns whether the cleaner animation is currently running
func (cs *CleanerSystem) IsActive() bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.isActive
}

// scanRedCharacterRows scans the world for rows containing Red characters
func (cs *CleanerSystem) scanRedCharacterRows(world *engine.World) []int {
	redRows := make(map[int]bool)

	seqType := reflect.TypeOf(components.SequenceComponent{})
	posType := reflect.TypeOf(components.PositionComponent{})

	entities := world.GetEntitiesWith(seqType, posType)

	for _, entity := range entities {
		seqComp, ok := world.GetComponent(entity, seqType)
		if !ok {
			continue
		}
		seq := seqComp.(components.SequenceComponent)

		// Only care about Red sequences
		if seq.Type != components.SequenceRed {
			continue
		}

		posComp, ok := world.GetComponent(entity, posType)
		if !ok {
			continue
		}
		pos := posComp.(components.PositionComponent)

		redRows[pos.Y] = true
	}

	// Convert map to sorted slice
	rows := make([]int, 0, len(redRows))
	for row := range redRows {
		rows = append(rows, row)
	}

	return rows
}

// spawnCleanerForRow creates a cleaner entity for the given row
func (cs *CleanerSystem) spawnCleanerForRow(world *engine.World, row int, startTime time.Time) {
	// Determine direction based on row parity
	// Odd rows: L→R (direction = 1), Even rows: R→L (direction = -1)
	direction := 1
	startX := -1.0

	if row%2 == 0 {
		// Even row: R→L
		direction = -1
		startX = float64(cs.gameWidth)
	}

	// Calculate speed: distance / time = gameWidth / animationDuration
	speed := float64(cs.gameWidth) / cs.animationDuration.Seconds()

	// Create cleaner entity
	entity := world.CreateEntity()

	cleaner := components.CleanerComponent{
		Row:            row,
		XPosition:      startX,
		Speed:          speed,
		Direction:      direction,
		StartTime:      startTime,
		TrailPositions: make([]float64, 0, constants.CleanerTrailLength),
		TrailMaxAge:    constants.CleanerTrailFadeTime,
	}

	world.AddComponent(entity, cleaner)
}

// updateCleanerPositions updates the position of all cleaner entities
func (cs *CleanerSystem) updateCleanerPositions(world *engine.World, now time.Time) {
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	entities := world.GetEntitiesWith(cleanerType)

	cs.mu.RLock()
	lastUpdate := cs.lastUpdateTime
	cs.mu.RUnlock()

	deltaTime := now.Sub(lastUpdate).Seconds()

	for _, entity := range entities {
		cleanerComp, ok := world.GetComponent(entity, cleanerType)
		if !ok {
			continue
		}
		cleaner := cleanerComp.(components.CleanerComponent)

		// Update position based on speed and direction
		cleaner.XPosition += cleaner.Speed * float64(cleaner.Direction) * deltaTime

		// Update trail (add current position to front)
		cleaner.TrailPositions = append([]float64{cleaner.XPosition}, cleaner.TrailPositions...)
		if len(cleaner.TrailPositions) > constants.CleanerTrailLength {
			cleaner.TrailPositions = cleaner.TrailPositions[:constants.CleanerTrailLength]
		}

		// Update component
		world.AddComponent(entity, cleaner)
	}
}

// detectAndDestroyRedCharacters checks for Red characters under cleaners and destroys them
func (cs *CleanerSystem) detectAndDestroyRedCharacters(world *engine.World) {
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleanerEntities := world.GetEntitiesWith(cleanerType)

	seqType := reflect.TypeOf(components.SequenceComponent{})

	for _, cleanerEntity := range cleanerEntities {
		cleanerComp, ok := world.GetComponent(cleanerEntity, cleanerType)
		if !ok {
			continue
		}
		cleaner := cleanerComp.(components.CleanerComponent)

		// Get the integer X position (current cleaner location)
		cleanerX := int(cleaner.XPosition + 0.5) // Round to nearest integer

		// Skip if out of bounds
		if cleanerX < 0 || cleanerX >= cs.gameWidth {
			continue
		}

		// Check if there's an entity at this position
		targetEntity := world.GetEntityAtPosition(cleanerX, cleaner.Row)
		if targetEntity == 0 {
			continue
		}

		// Check if it's a Red character
		seqComp, ok := world.GetComponent(targetEntity, seqType)
		if !ok {
			continue
		}
		seq := seqComp.(components.SequenceComponent)

		if seq.Type == components.SequenceRed {
			// Destroy the Red character
			world.SafeDestroyEntity(targetEntity)
		}
	}
}

// cleanupCleaners removes all cleaner entities from the world
func (cs *CleanerSystem) cleanupCleaners(world *engine.World) {
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	entities := world.GetEntitiesWith(cleanerType)

	for _, entity := range entities {
		world.SafeDestroyEntity(entity)
	}
}

// UpdateDimensions updates the game area dimensions
func (cs *CleanerSystem) UpdateDimensions(gameWidth, gameHeight int) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.gameWidth = gameWidth
	cs.gameHeight = gameHeight
}

// GetCleanerEntities returns all active cleaner entities (for rendering)
func (cs *CleanerSystem) GetCleanerEntities(world *engine.World) []engine.Entity {
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	return world.GetEntitiesWith(cleanerType)
}
