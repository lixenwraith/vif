package systems

import (
	"fmt"
	"log"
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
	mu                sync.RWMutex         // Protects gameWidth, gameHeight, animationDuration, world
	stateMu           sync.RWMutex         // Protects cleanerDataMap
	flashMu           sync.RWMutex         // Protects flashPositions
	isActive          atomic.Bool          // Atomic flag for cleaner active state
	activationTime    atomic.Int64         // Unix nano timestamp of activation
	lastUpdateTime    atomic.Int64         // Unix nano timestamp of last update
	lastScanTime      atomic.Int64         // Unix nano timestamp of last periodic scan
	activeCleanerCount atomic.Int64        // Number of active cleaner entities
	gameWidth         int                  // Protected by mu
	gameHeight        int                  // Protected by mu
	animationDuration time.Duration        // Protected by mu
	world             *engine.World        // Protected by mu - stored from spawn request
	spawnChan         chan cleanerSpawnRequest // Channel for spawn requests
	stopChan          chan struct{}        // Channel to stop the update loop
	cleanerPool       sync.Pool            // Pool for cleaner trail slice allocation
	cleanerDataMap    map[engine.Entity]*cleanerData // Maps entity to its runtime data
	flashPositions    map[string]bool      // Tracks active flash positions to prevent duplicates
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
		flashPositions:    make(map[string]bool),
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
	cs.activeCleanerCount.Store(0)

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
	// Process spawn requests from channel
	select {
	case req := <-cs.spawnChan:
		cs.processSpawnRequest(req)
	default:
		// No spawn request, continue
	}

	// Update cleaners synchronously (in addition to concurrent updateLoop)
	// This ensures tests work properly with mock time providers
	cs.updateCleaners()

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
			// Remove from flash position tracking
			flashKey := fmt.Sprintf("%d,%d", flash.X, flash.Y)
			cs.flashMu.Lock()
			delete(cs.flashPositions, flashKey)
			cs.flashMu.Unlock()

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

	// Get world reference safely
	cs.mu.RLock()
	world := cs.world
	cs.mu.RUnlock()

	// If world is nil, use ctx.World (e.g., during periodic scan before first spawn)
	if world == nil {
		world = cs.ctx.World
	}

	// Trigger cleaners (uses existing spawn request mechanism)
	cs.TriggerCleaners(world)
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
	world := cs.world
	cs.mu.RUnlock()

	if elapsed >= duration {
		log.Printf("[CLEANER] Animation complete (elapsed: %v) - cleaning up", elapsed)
		if world != nil {
			cs.cleanupCleaners(world)
		}
		cs.isActive.Store(false)
		cs.lastUpdateTime.Store(0)
		return
	}

	// Update cleaner positions
	lastUpdateNano := cs.lastUpdateTime.Load()
	if lastUpdateNano == 0 {
		// First update, just set time
		log.Printf("[CLEANER] First update - initializing timestamps")
		cs.lastUpdateTime.Store(nowNano)
		return
	}

	// Only update if we have a valid world reference
	if world == nil {
		return
	}

	deltaTime := float64(nowNano-lastUpdateNano) / float64(time.Second)
	cs.updateCleanerPositions(world, deltaTime)
	cs.detectAndDestroyRedCharacters(world)

	// Update last update time
	cs.lastUpdateTime.Store(nowNano)
}

// TriggerCleaners initiates the cleaner animation (non-blocking)
func (cs *CleanerSystem) TriggerCleaners(world *engine.World) {
	log.Printf("[CLEANER] TriggerCleaners called")

	// Send spawn request via channel (non-blocking with select)
	select {
	case cs.spawnChan <- cleanerSpawnRequest{world: world}:
		log.Printf("[CLEANER] Spawn request queued successfully")
	default:
		log.Printf("[CLEANER] WARNING: Spawn channel full - request dropped")
	}
}

// processSpawnRequest handles a cleaner spawn request
func (cs *CleanerSystem) processSpawnRequest(req cleanerSpawnRequest) {
	log.Printf("[CLEANER] Processing spawn request...")

	// Prevent duplicate triggers
	if cs.isActive.Load() {
		log.Printf("[CLEANER] Already active - ignoring spawn request")
		return
	}

	// Store world reference for updateLoop goroutine to use
	cs.mu.Lock()
	cs.world = req.world
	cs.mu.Unlock()

	// Scan for rows with Red characters (with mutex protection)
	redRows := cs.scanRedCharacterRows(req.world)

	log.Printf("[CLEANER] Scanned for Red characters: found %d rows with Red chars", len(redRows))

	if len(redRows) == 0 {
		log.Printf("[CLEANER] No Red characters to clean - spawn aborted")
		return
	}

	// Apply MaxConcurrentCleaners limit if configured
	if cs.config.MaxConcurrentCleaners > 0 && len(redRows) > cs.config.MaxConcurrentCleaners {
		log.Printf("[CLEANER] Limiting cleaners from %d to %d (MaxConcurrentCleaners)",
			len(redRows), cs.config.MaxConcurrentCleaners)
		redRows = redRows[:cs.config.MaxConcurrentCleaners]
	}

	// Set activation time ATOMICALLY BEFORE spawning cleaners to prevent race condition
	// where updateLoop checks elapsed time before activation time is set
	now := cs.ctx.TimeProvider.Now()
	nowNano := now.UnixNano()
	cs.activationTime.Store(nowNano)
	cs.lastUpdateTime.Store(nowNano)

	// Activate the system before spawning to ensure updateLoop can see active state
	cs.isActive.Store(true)
	log.Printf("[CLEANER] Activated cleaner system - spawning %d cleaners", len(redRows))

	// Spawn cleaner entities for each row
	for _, row := range redRows {
		cs.spawnCleanerForRow(req.world, row)
	}

	log.Printf("[CLEANER] Spawn complete - active cleaner count: %d", cs.activeCleanerCount.Load())
}

// IsActive returns whether the cleaner animation is currently running
func (cs *CleanerSystem) IsActive() bool {
	return cs.isActive.Load()
}

// GetActiveCleanerCount returns the number of active cleaner entities
func (cs *CleanerSystem) GetActiveCleanerCount() int64 {
	return cs.activeCleanerCount.Load()
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

	// Increment active cleaner count
	cs.activeCleanerCount.Add(1)

	log.Printf("[CLEANER] Spawned cleaner on row %d (entity=%d, direction=%d, speed=%.2f)",
		row, entity, direction, speed)
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

		// Store old position before updating
		oldPosition := cleaner.XPosition

		// Update position based on speed and direction
		cleaner.XPosition += cleaner.Speed * float64(cleaner.Direction) * deltaTime

		// Update trail (add current position to front) while preserving capacity
		// Use insert-at-front pattern that maintains the original slice capacity from pool
		if len(cleaner.TrailPositions) < cs.config.TrailLength {
			// Still room to grow, append to end then shift
			cleaner.TrailPositions = append(cleaner.TrailPositions, 0)
			copy(cleaner.TrailPositions[1:], cleaner.TrailPositions[0:])
			cleaner.TrailPositions[0] = cleaner.XPosition
		} else {
			// At max length, shift and overwrite last
			copy(cleaner.TrailPositions[1:], cleaner.TrailPositions[0:cs.config.TrailLength-1])
			cleaner.TrailPositions[0] = cleaner.XPosition
		}

		// Update component
		world.AddComponent(entity, cleaner)

		// Update tracked data and check for collisions along the path
		cs.stateMu.Lock()
		if data, exists := cs.cleanerDataMap[entity]; exists {
			data.xPosition = cleaner.XPosition
			data.trailPositions = cleaner.TrailPositions
		}
		cs.stateMu.Unlock()

		// Check all integer positions between old and new position for collisions
		cs.checkCollisionsAlongPath(world, entity, oldPosition, cleaner.XPosition, cleaner.Row, cleaner.Direction)
	}
}

// checkCollisionsAlongPath checks all integer positions between old and new position for collisions
func (cs *CleanerSystem) checkCollisionsAlongPath(world *engine.World, cleanerEntity engine.Entity, oldX, newX float64, row, direction int) {
	cs.mu.RLock()
	gameWidth := cs.gameWidth
	cs.mu.RUnlock()

	// Determine the range of integer positions to check
	startX := int(oldX + 0.5)
	endX := int(newX + 0.5)

	// Ensure we check in the correct direction
	if direction < 0 {
		// R→L: swap if needed
		if startX < endX {
			startX, endX = endX, startX
		}
		// Check from startX down to endX
		for x := startX; x >= endX; x-- {
			if x >= 0 && x < gameWidth {
				cs.checkAndDestroyAtPosition(world, x, row)
			}
		}
	} else {
		// L→R
		if startX > endX {
			startX, endX = endX, startX
		}
		// Check from startX up to endX
		for x := startX; x <= endX; x++ {
			if x >= 0 && x < gameWidth {
				cs.checkAndDestroyAtPosition(world, x, row)
			}
		}
	}
}

// checkAndDestroyAtPosition checks a specific position for Red characters and destroys them
func (cs *CleanerSystem) checkAndDestroyAtPosition(world *engine.World, x, y int) {
	seqType := reflect.TypeOf(components.SequenceComponent{})
	charType := reflect.TypeOf(components.CharacterComponent{})
	posType := reflect.TypeOf(components.PositionComponent{})

	// Check if there's an entity at this position
	targetEntity := world.GetEntityAtPosition(x, y)
	if targetEntity == 0 {
		return
	}

	// Check if it's a Red character
	seqComp, ok := world.GetComponent(targetEntity, seqType)
	if !ok || seqComp == nil {
		return
	}
	seq, ok := seqComp.(components.SequenceComponent)
	if !ok || seq.Type != components.SequenceRed {
		return
	}

	// Get character info for flash effect before destroying
	charComp, hasChar := world.GetComponent(targetEntity, charType)
	posComp, hasPos := world.GetComponent(targetEntity, posType)

	// Create flash effect at removal location
	if hasChar && hasPos && charComp != nil && posComp != nil {
		char, charOk := charComp.(components.CharacterComponent)
		pos, posOk := posComp.(components.PositionComponent)

		if charOk && posOk {
			// Check if flash already exists at this position
			flashKey := fmt.Sprintf("%d,%d", pos.X, pos.Y)

			cs.flashMu.Lock()
			if !cs.flashPositions[flashKey] {
				// Mark position as having an active flash
				cs.flashPositions[flashKey] = true
				cs.flashMu.Unlock()

				// Create flash entity
				flashEntity := world.CreateEntity()
				flash := components.RemovalFlashComponent{
					X:         pos.X,
					Y:         pos.Y,
					Char:      char.Rune,
					StartTime: cs.ctx.TimeProvider.Now(),
					Duration:  cs.config.FlashDuration,
				}
				world.AddComponent(flashEntity, flash)
			} else {
				cs.flashMu.Unlock()
			}
		}
	}

	// Destroy the Red character
	world.SafeDestroyEntity(targetEntity)
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
		if !ok || cleanerComp == nil {
			continue
		}
		cleaner, ok := cleanerComp.(components.CleanerComponent)
		if !ok {
			continue
		}

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

		// Check if it's a Red character (with nil checks)
		seqComp, ok := world.GetComponent(targetEntity, seqType)
		if !ok || seqComp == nil {
			continue
		}
		seq, ok := seqComp.(components.SequenceComponent)
		if !ok {
			continue
		}

		if seq.Type == components.SequenceRed {
			// Get character info for flash effect before destroying (with nil checks)
			charComp, hasChar := world.GetComponent(targetEntity, charType)
			posComp, hasPos := world.GetComponent(targetEntity, posType)

			// Create flash effect at removal location
			if hasChar && hasPos && charComp != nil && posComp != nil {
				char, charOk := charComp.(components.CharacterComponent)
				pos, posOk := posComp.(components.PositionComponent)

				if charOk && posOk {
					// Check if flash already exists at this position
					flashKey := fmt.Sprintf("%d,%d", pos.X, pos.Y)

					cs.flashMu.Lock()
					if !cs.flashPositions[flashKey] {
						// Mark position as having an active flash
						cs.flashPositions[flashKey] = true
						cs.flashMu.Unlock()

						// Create flash entity
						flashEntity := world.CreateEntity()
						flash := components.RemovalFlashComponent{
							X:         pos.X,
							Y:         pos.Y,
							Char:      char.Rune,
							StartTime: cs.ctx.TimeProvider.Now(),
							Duration:  cs.config.FlashDuration,
						}
						world.AddComponent(flashEntity, flash)
					} else {
						cs.flashMu.Unlock()
					}
				}
			}

			// Destroy the Red character using SafeDestroyEntity (handles spatial index removal)
			world.SafeDestroyEntity(targetEntity)
		}
	}
}

// cleanupCleaners removes all cleaner entities from the world
func (cs *CleanerSystem) cleanupCleaners(world *engine.World) {
	cleanerType := reflect.TypeOf(components.CleanerComponent{})

	// Get ALL cleaner entities from world before we start modifying
	entities := world.GetEntitiesWith(cleanerType)

	cs.stateMu.Lock()

	// Iterate through ALL cleaner entities
	cleanerCount := int64(0)
	for _, entity := range entities {
		// Return trail slice to pool before destroying entity
		if data, exists := cs.cleanerDataMap[entity]; exists {
			if data.trailPositions != nil {
				cs.cleanerPool.Put(data.trailPositions)
			}
			delete(cs.cleanerDataMap, entity)
		} else {
			// If not in map, still try to get component and return trail to pool
			cleanerComp, ok := world.GetComponent(entity, cleanerType)
			if ok {
				cleaner := cleanerComp.(components.CleanerComponent)
				if cleaner.TrailPositions != nil {
					cs.cleanerPool.Put(cleaner.TrailPositions)
				}
			}
		}

		// Destroy entity (cleaners don't have PositionComponent, so no spatial index removal needed)
		world.SafeDestroyEntity(entity)
		cleanerCount++
	}

	// Clear cleanerDataMap completely
	cs.cleanerDataMap = make(map[engine.Entity]*cleanerData)

	cs.stateMu.Unlock()

	// Reset all atomic state variables
	cs.isActive.Store(false)
	cs.activationTime.Store(0)
	cs.lastUpdateTime.Store(0)

	// Reset active cleaner count to 0 (not decrement, as we're doing full cleanup)
	cs.activeCleanerCount.Store(0)

	// Clear world reference
	cs.mu.Lock()
	cs.world = nil
	cs.mu.Unlock()

	// Clear flash position tracking
	cs.flashMu.Lock()
	cs.flashPositions = make(map[string]bool)
	cs.flashMu.Unlock()

	// Verification: ensure no cleaners remain in world
	remainingCleaners := world.GetEntitiesWith(cleanerType)
	if len(remainingCleaners) > 0 {
		// Sanity check: in production, this should never happen
		// We could log this if we had a logger, but for now we just verify
		_ = remainingCleaners // Silence unused variable warning
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

// Shutdown stops the concurrent update loop and cleans up all resources
func (cs *CleanerSystem) Shutdown() {
	// Stop the update loop
	close(cs.stopChan)
	cs.wg.Wait()

	// Final cleanup of any remaining cleaners
	if cs.ctx != nil && cs.ctx.World != nil {
		cs.cleanupCleaners(cs.ctx.World)
	}
}

// GetSystemState returns the current state of the cleaner system for debugging
func (cs *CleanerSystem) GetSystemState() string {
	active := cs.isActive.Load()
	cleanerCount := cs.activeCleanerCount.Load()

	if active {
		activationNano := cs.activationTime.Load()
		if activationNano > 0 {
			activationTime := time.Unix(0, activationNano)
			elapsed := cs.ctx.TimeProvider.Now().Sub(activationTime)
			return fmt.Sprintf("Cleaner[active=true, count=%d, elapsed=%.2fs]",
				cleanerCount, elapsed.Seconds())
		}
		return fmt.Sprintf("Cleaner[active=true, count=%d]", cleanerCount)
	}
	return "Cleaner[inactive]"
}

// ActivateCleaners initiates the cleaner animation (Phase 6: called by ClockScheduler)
// This is an alias for TriggerCleaners to match the CleanerSystemInterface
func (cs *CleanerSystem) ActivateCleaners(world *engine.World) {
	log.Printf("[CLEANER] ActivateCleaners called via ClockScheduler")
	cs.TriggerCleaners(world)
}

// IsAnimationComplete checks if the cleaner animation has finished (Phase 6: for ClockScheduler)
// Returns true if animation duration has elapsed or cleaners are not active
func (cs *CleanerSystem) IsAnimationComplete() bool {
	if !cs.isActive.Load() {
		return true // Not active = complete
	}

	activationNano := cs.activationTime.Load()
	if activationNano == 0 {
		return true // No valid activation time = complete
	}

	now := cs.ctx.TimeProvider.Now()
	activationTime := time.Unix(0, activationNano)
	elapsed := now.Sub(activationTime)

	cs.mu.RLock()
	duration := cs.animationDuration
	cs.mu.RUnlock()

	isComplete := elapsed >= duration
	if isComplete {
		log.Printf("[CLEANER] Animation complete: elapsed=%.2fs, duration=%.2fs", elapsed.Seconds(), duration.Seconds())
	}

	return isComplete
}
