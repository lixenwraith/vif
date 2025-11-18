package systems

import (
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// cleanerSpawnRequest represents a request to spawn cleaners
type cleanerSpawnRequest struct {
	world *engine.World
}

// cleanerData holds the runtime data for a cleaner entity
type cleanerData struct {
	entity         engine.Entity
	row            int
	xPosition      float64
	speed          float64
	direction      int
	trailPositions []float64
}

// CleanerSystem manages the cleaner animation triggered when gold sequences are completed at max heat.
// Cleaners are bright yellow blocks that sweep across rows containing Red characters, removing them
// on contact while leaving Blue/Green characters unaffected.
//
// The system uses:
// - sync.Pool for efficient cleaner object allocation
// - Channel for non-blocking spawn requests
// - Concurrent update loop running in a goroutine
// - Atomic operations for thread-safe state management
// - Mutex protection for screen buffer scanning
type CleanerSystem struct {
	ctx               *engine.GameContext
	config            constants.CleanerConfig // Configuration for cleaner behavior
	mu                sync.RWMutex         // Protects gameWidth, gameHeight, animationDuration
	stateMu           sync.RWMutex         // Protects cleanerDataMap
	isActive          atomic.Bool          // Atomic flag for cleaner active state
	activationTime    atomic.Int64         // Unix nano timestamp of activation
	lastUpdateTime    atomic.Int64         // Unix nano timestamp of last update
	lastScanTime      atomic.Int64         // Unix nano timestamp of last periodic scan
	gameWidth         int                  // Protected by mu
	gameHeight        int                  // Protected by mu
	animationDuration time.Duration        // Protected by mu
	spawnChan         chan cleanerSpawnRequest // Channel for spawn requests
	stopChan          chan struct{}        // Channel to stop the update loop
	cleanerPool       sync.Pool            // Pool for cleaner trail slice allocation
	cleanerDataMap    map[engine.Entity]*cleanerData // Maps entity to its runtime data
	wg                sync.WaitGroup       // Tracks goroutine lifecycle
}

// NewCleanerSystem creates a new cleaner system with the specified configuration
func NewCleanerSystem(ctx *engine.GameContext, gameWidth, gameHeight int, config constants.CleanerConfig) *CleanerSystem {
	cs := &CleanerSystem{
		ctx:               ctx,
		config:            config,
		gameWidth:         gameWidth,
		gameHeight:        gameHeight,
		animationDuration: config.AnimationDuration,
		spawnChan:         make(chan cleanerSpawnRequest, 10), // Buffered channel
		stopChan:          make(chan struct{}),
		cleanerDataMap:    make(map[engine.Entity]*cleanerData),
		cleanerPool: sync.Pool{
			New: func() interface{} {
				// Pre-allocate trail slice to avoid repeated allocations
				return make([]float64, 0, config.TrailLength)
			},
		},
	}

	// Set initial atomic values
	cs.isActive.Store(false)
	cs.activationTime.Store(0)
	cs.lastUpdateTime.Store(0)
	cs.lastScanTime.Store(0)

	// Start concurrent update loop
	cs.wg.Add(1)
	go cs.updateLoop()

	return cs
}

// Priority returns the system's priority (runs after decay system)
func (cs *CleanerSystem) Priority() int {
	return 35
}

// Update runs the cleaner system logic (called from main game loop)
func (cs *CleanerSystem) Update(world *engine.World, dt time.Duration) {
	// Main game loop just processes spawn requests
	// Actual cleaner updates happen in concurrent updateLoop
	select {
	case req := <-cs.spawnChan:
		cs.processSpawnRequest(req)
	default:
		// No spawn request, continue
	}

	// Clean up expired flash effects
	cs.cleanupExpiredFlashes(world)
}

// cleanupExpiredFlashes removes flash effect entities that have exceeded their duration
func (cs *CleanerSystem) cleanupExpiredFlashes(world *engine.World) {
	flashType := reflect.TypeOf(components.RemovalFlashComponent{})
	entities := world.GetEntitiesWith(flashType)

	now := cs.ctx.TimeProvider.Now()

	for _, entity := range entities {
		flashComp, ok := world.GetComponent(entity, flashType)
		if !ok {
			continue
		}
		flash := flashComp.(components.RemovalFlashComponent)

		elapsed := now.Sub(flash.StartTime).Milliseconds()
		if elapsed >= int64(flash.Duration) {
			// Flash has expired, destroy entity
			world.SafeDestroyEntity(entity)
		}
	}
}

// updateLoop runs concurrently and updates cleaner positions
func (cs *CleanerSystem) updateLoop() {
	defer cs.wg.Done()

	ticker := time.NewTicker(time.Second / time.Duration(cs.config.FPS))
	defer ticker.Stop()

	// Create periodic scan ticker if ScanInterval is set
	var scanTicker *time.Ticker
	if cs.config.ScanInterval > 0 {
		scanTicker = time.NewTicker(cs.config.ScanInterval)
		defer scanTicker.Stop()
	}

	for {
		select {
		case <-cs.stopChan:
			return
		case <-ticker.C:
			cs.updateCleaners()
		case <-func() <-chan time.Time {
			if scanTicker != nil {
				return scanTicker.C
			}
			// Return a channel that never receives if scanTicker is nil
			return make(<-chan time.Time)
		}():
			// Periodic scan triggered
			cs.triggerPeriodicScan()
		}
	}
}

// triggerPeriodicScan checks if a periodic scan should trigger cleaners
func (cs *CleanerSystem) triggerPeriodicScan() {
	// Don't scan if cleaners are already active
	if cs.isActive.Load() {
		return
	}

	now := cs.ctx.TimeProvider.Now()
	nowNano := now.UnixNano()

	// Update last scan time
	cs.lastScanTime.Store(nowNano)

	// Trigger cleaners (uses existing spawn request mechanism)
	cs.TriggerCleaners(cs.ctx.World)
}

// updateCleaners performs the actual cleaner update logic
func (cs *CleanerSystem) updateCleaners() {
	if !cs.isActive.Load() {
		return
	}

	now := cs.ctx.TimeProvider.Now()
	nowNano := now.UnixNano()

	// Get activation time
	activationNano := cs.activationTime.Load()
	if activationNano == 0 {
		return
	}

	// Calculate elapsed time since animation started
	elapsed := time.Duration(nowNano - activationNano)

	// Check if animation is complete
	cs.mu.RLock()
	duration := cs.animationDuration
	cs.mu.RUnlock()

	if elapsed >= duration {
		cs.cleanupCleaners(cs.ctx.World)
		cs.isActive.Store(false)
		cs.lastUpdateTime.Store(0)
		return
	}

	// Update cleaner positions
	lastUpdateNano := cs.lastUpdateTime.Load()
	if lastUpdateNano == 0 {
		// First update, just set time
		cs.lastUpdateTime.Store(nowNano)
		return
	}

	deltaTime := float64(nowNano-lastUpdateNano) / float64(time.Second)
	cs.updateCleanerPositions(cs.ctx.World, deltaTime)
	cs.detectAndDestroyRedCharacters(cs.ctx.World)

	// Update last update time
	cs.lastUpdateTime.Store(nowNano)
}

// TriggerCleaners initiates the cleaner animation (non-blocking)
func (cs *CleanerSystem) TriggerCleaners(world *engine.World) {
	// Send spawn request via channel (non-blocking with select)
	select {
	case cs.spawnChan <- cleanerSpawnRequest{world: world}:
		// Request queued successfully
	default:
		// Channel full, drop request (already have pending spawn)
	}
}

// processSpawnRequest handles a cleaner spawn request
func (cs *CleanerSystem) processSpawnRequest(req cleanerSpawnRequest) {
	// Prevent duplicate triggers
	if cs.isActive.Load() {
		return
	}

	// Scan for rows with Red characters (with mutex protection)
	redRows := cs.scanRedCharacterRows(req.world)

	if len(redRows) == 0 {
		// No Red characters to clean
		return
	}

	// Apply MaxConcurrentCleaners limit if configured
	if cs.config.MaxConcurrentCleaners > 0 && len(redRows) > cs.config.MaxConcurrentCleaners {
		// Limit to max concurrent cleaners
		redRows = redRows[:cs.config.MaxConcurrentCleaners]
	}

	// Spawn cleaner entities for each row
	now := cs.ctx.TimeProvider.Now()
	for _, row := range redRows {
		cs.spawnCleanerForRow(req.world, row)
	}

	// Activate the system
	cs.isActive.Store(true)
	cs.activationTime.Store(now.UnixNano())
	cs.lastUpdateTime.Store(now.UnixNano())
}

// IsActive returns whether the cleaner animation is currently running
func (cs *CleanerSystem) IsActive() bool {
	return cs.isActive.Load()
}

// scanRedCharacterRows scans the world for rows containing Red characters
// World methods already provide thread-safe access
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

	// Convert map to slice
	rows := make([]int, 0, len(redRows))
	for row := range redRows {
		rows = append(rows, row)
	}

	return rows
}

// spawnCleanerForRow creates a cleaner entity for the given row
func (cs *CleanerSystem) spawnCleanerForRow(world *engine.World, row int) {
	cs.mu.RLock()
	gameWidth := cs.gameWidth
	duration := cs.animationDuration
	cs.mu.RUnlock()

	// Determine direction based on row parity
	// Odd rows: L→R (direction = 1), Even rows: R→L (direction = -1)
	direction := 1
	startX := -1.0

	if row%2 == 0 {
		// Even row: R→L
		direction = -1
		startX = float64(gameWidth)
	}

	// Calculate speed: use configured speed if set, otherwise calculate from duration
	var speed float64
	if cs.config.Speed > 0 {
		speed = cs.config.Speed
	} else {
		// Calculate speed: distance / time = gameWidth / animationDuration
		speed = float64(gameWidth) / duration.Seconds()
	}

	// Get trail slice from pool
	trail := cs.cleanerPool.Get().([]float64)
	trail = trail[:0] // Reset length

	// Create cleaner entity
	entity := world.CreateEntity()

	cleaner := components.CleanerComponent{
		Row:            row,
		XPosition:      startX,
		Speed:          speed,
		Direction:      direction,
		TrailPositions: trail,
		TrailMaxAge:    cs.config.TrailFadeTime,
		Char:           cs.config.Char,
	}

	world.AddComponent(entity, cleaner)

	// Track cleaner data
	cs.stateMu.Lock()
	cs.cleanerDataMap[entity] = &cleanerData{
		entity:         entity,
		row:            row,
		xPosition:      startX,
		speed:          speed,
		direction:      direction,
		trailPositions: trail,
	}
	cs.stateMu.Unlock()
}

// updateCleanerPositions updates the position of all cleaner entities
func (cs *CleanerSystem) updateCleanerPositions(world *engine.World, deltaTime float64) {
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	entities := world.GetEntitiesWith(cleanerType)

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
		if len(cleaner.TrailPositions) > cs.config.TrailLength {
			cleaner.TrailPositions = cleaner.TrailPositions[:cs.config.TrailLength]
		}

		// Update component
		world.AddComponent(entity, cleaner)

		// Update tracked data
		cs.stateMu.Lock()
		if data, exists := cs.cleanerDataMap[entity]; exists {
			data.xPosition = cleaner.XPosition
			data.trailPositions = cleaner.TrailPositions
		}
		cs.stateMu.Unlock()
	}
}

// detectAndDestroyRedCharacters checks for Red characters under cleaners and destroys them
// with optional flash effect for visual feedback
func (cs *CleanerSystem) detectAndDestroyRedCharacters(world *engine.World) {
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleanerEntities := world.GetEntitiesWith(cleanerType)

	seqType := reflect.TypeOf(components.SequenceComponent{})
	posType := reflect.TypeOf(components.PositionComponent{})
	charType := reflect.TypeOf(components.CharacterComponent{})

	cs.mu.RLock()
	gameWidth := cs.gameWidth
	cs.mu.RUnlock()

	for _, cleanerEntity := range cleanerEntities {
		cleanerComp, ok := world.GetComponent(cleanerEntity, cleanerType)
		if !ok {
			continue
		}
		cleaner := cleanerComp.(components.CleanerComponent)

		// Get the integer X position (current cleaner location)
		cleanerX := int(cleaner.XPosition + 0.5) // Round to nearest integer

		// Skip if out of bounds
		if cleanerX < 0 || cleanerX >= gameWidth {
			continue
		}

		// Check if there's an entity at this position
		// GetEntityAtPosition already provides thread-safe access
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
			// Get character info for flash effect before destroying
			charComp, hasChar := world.GetComponent(targetEntity, charType)
			posComp, hasPos := world.GetComponent(targetEntity, posType)

			// Create flash effect at removal location
			if hasChar && hasPos {
				char := charComp.(components.CharacterComponent)
				pos := posComp.(components.PositionComponent)

				flashEntity := world.CreateEntity()
				flash := components.RemovalFlashComponent{
					X:         pos.X,
					Y:         pos.Y,
					Char:      char.Rune,
					StartTime: cs.ctx.TimeProvider.Now(),
					Duration:  cs.config.FlashDuration,
				}
				world.AddComponent(flashEntity, flash)
			}

			// Destroy the Red character
			world.SafeDestroyEntity(targetEntity)
		}
	}
}

// cleanupCleaners removes all cleaner entities from the world
func (cs *CleanerSystem) cleanupCleaners(world *engine.World) {
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	entities := world.GetEntitiesWith(cleanerType)

	cs.stateMu.Lock()
	defer cs.stateMu.Unlock()

	for _, entity := range entities {
		// Return trail slice to pool
		if data, exists := cs.cleanerDataMap[entity]; exists {
			if data.trailPositions != nil {
				cs.cleanerPool.Put(data.trailPositions)
			}
			delete(cs.cleanerDataMap, entity)
		}

		// Destroy entity
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

// Shutdown stops the concurrent update loop
func (cs *CleanerSystem) Shutdown() {
	close(cs.stopChan)
	cs.wg.Wait()
}
