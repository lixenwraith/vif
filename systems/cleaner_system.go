package systems

import (
	"fmt"
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// cleanerSpawnRequest represents a request to spawn cleaners
type cleanerSpawnRequest struct {
	// Empty - world will be passed via Update() method
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
// Synchronous Update Model:
// - Updates run in main game loop via Update() method (no autonomous goroutine)
// - Uses delta time from frame ticker for smooth animation
// - Non-blocking spawn requests via buffered channel
// - Atomic operations for lock-free state checks (isActive, activationTime, activeCleanerCount)
// - Mutex protection for cleanerDataMap and flashPositions
// - Frame-coherent snapshots for thread-safe rendering
// - sync.Pool for efficient trail slice allocation/deallocation
type CleanerSystem struct {
	ctx                *engine.GameContext
	config             constants.CleanerConfig        // Configuration for cleaner behavior
	mu                 sync.RWMutex                   // Protects animationDuration
	stateMu            sync.RWMutex                   // Protects cleanerDataMap
	flashMu            sync.RWMutex                   // Protects flashPositions
	isActive           atomic.Bool                    // Atomic flag for cleaner active state
	firstUpdate        atomic.Bool                    // Atomic flag to skip first update (set on activation)
	activationTime     atomic.Int64                   // Unix nano timestamp of activation
	lastScanTime       atomic.Int64                   // Unix nano timestamp of last periodic scan
	activeCleanerCount atomic.Int64                   // Number of active cleaner entities
	animationDuration  time.Duration                  // Protected by mu
	spawnChan          chan cleanerSpawnRequest       // Channel for spawn requests
	cleanerPool        sync.Pool                      // Pool for cleaner trail slice allocation
	cleanerDataMap     map[engine.Entity]*cleanerData // Maps entity to its runtime data
	flashPositions     map[string]bool                // Tracks active flash positions to prevent duplicates
}

// NewCleanerSystem creates a new cleaner system with the specified configuration
func NewCleanerSystem(ctx *engine.GameContext, gameWidth, gameHeight int, config constants.CleanerConfig) *CleanerSystem {
	cs := &CleanerSystem{
		ctx:               ctx,
		config:            config,
		animationDuration: config.AnimationDuration,
		spawnChan:         make(chan cleanerSpawnRequest, 10), // Buffered channel
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
	cs.firstUpdate.Store(false)
	cs.activationTime.Store(0)
	cs.lastScanTime.Store(0)
	cs.activeCleanerCount.Store(0)

	return cs
}

// Priority returns the system's priority (runs after decay system)
func (cs *CleanerSystem) Priority() int {
	return 30
}

// Update runs the cleaner system logic synchronously in the main game loop.
// This method processes spawn requests, updates cleaner positions using delta time,
// and cleans up expired flash effects. All entity modifications happen here,
// not in a separate goroutine, eliminating race conditions.
func (cs *CleanerSystem) Update(world *engine.World, dt time.Duration) {
	// Process spawn requests from channel
	select {
	case <-cs.spawnChan:
		cs.processSpawnRequest(world)
	default:
		// No spawn request, continue
	}

	// Update cleaners if active (now runs in main game loop)
	if cs.isActive.Load() {
		cs.updateCleaners(world, dt)
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

// updateCleaners performs the actual cleaner update logic
func (cs *CleanerSystem) updateCleaners(world *engine.World, dt time.Duration) {
	if !cs.isActive.Load() {
		return
	}

	// Skip first update to match old behavior (cleaners spawn but don't move yet)
	if cs.firstUpdate.Load() {
		cs.firstUpdate.Store(false)
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

	// Check if animation is complete (read duration under lock)
	cs.mu.RLock()
	duration := cs.animationDuration
	cs.mu.RUnlock()

	if elapsed >= duration {
		cs.cleanupCleaners(world)
		cs.isActive.Store(false)
		return
	}

	// Calculate deltaTime from the dt parameter
	deltaTime := dt.Seconds()

	// Update cleaner positions - handles all collision detection via trail
	cs.updateCleanerPositions(world, deltaTime)
}

// TriggerCleaners initiates the cleaner animation (non-blocking)
// Sets activation state atomically BEFORE sending to channel to prevent race condition
// where IsAnimationComplete() is called before processSpawnRequest() runs
func (cs *CleanerSystem) TriggerCleaners(world *engine.World) {
	// Prevent duplicate triggers - check if already active
	if cs.isActive.Load() {
		return
	}

	// CRITICAL: Set activation state BEFORE sending to channel
	// This ensures IsAnimationComplete() returns false even if processSpawnRequest() hasn't run yet
	now := cs.ctx.TimeProvider.Now()
	nowNano := now.UnixNano()
	cs.activationTime.Store(nowNano)
	cs.isActive.Store(true)
	cs.firstUpdate.Store(true)

	// Send spawn request via channel (non-blocking with select)
	// World will be passed via Update() method
	select {
	case cs.spawnChan <- cleanerSpawnRequest{}:
		// Spawn request queued successfully
	default:
		// Spawn channel full - request dropped
	}
}

// processSpawnRequest handles a cleaner spawn request
// Note: Activation state (isActive, activationTime, firstUpdate) is already set by TriggerCleaners()
// This method only handles the actual spawning of visual cleaner entities
func (cs *CleanerSystem) processSpawnRequest(world *engine.World) {
	// Check if already active - if so, this is likely a duplicate request
	// The activation state was already set by TriggerCleaners(), so we just need to spawn cleaners
	if !cs.isActive.Load() {
		// This should not happen - TriggerCleaners should have set isActive before sending to channel
		// But handle gracefully by returning without spawning
		return
	}

	// Scan for rows with Red characters
	redRows := cs.scanRedCharacterRows(world)

	// Only spawn visual entities if Red characters exist
	// Even if no Red characters exist, cleaners are still "active" (phantom cleaners)
	// for proper phase transitions - the activation state was set by ActivateCleaners()
	if len(redRows) == 0 {
		return
	}

	// Apply MaxConcurrentCleaners limit if configured
	if cs.config.MaxConcurrentCleaners > 0 && len(redRows) > cs.config.MaxConcurrentCleaners {
		redRows = redRows[:cs.config.MaxConcurrentCleaners]
	}

	// Spawn cleaner entities for each row
	for _, row := range redRows {
		cs.spawnCleanerForRow(world, row)
	}
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

	// Read game height from context
	gameHeight := cs.ctx.GameHeight

	seqType := reflect.TypeOf(components.SequenceComponent{})
	posType := reflect.TypeOf(components.PositionComponent{})

	entities := world.GetEntitiesWith(seqType, posType)

	for _, entity := range entities {
		seqComp, ok := world.GetComponent(entity, seqType)
		if !ok || seqComp == nil {
			continue
		}
		seq, ok := seqComp.(components.SequenceComponent)
		if !ok {
			continue
		}

		// Only care about Red sequences
		if seq.Type != components.SequenceRed {
			continue
		}

		posComp, ok := world.GetComponent(entity, posType)
		if !ok || posComp == nil {
			continue
		}
		pos, ok := posComp.(components.PositionComponent)
		if !ok {
			continue
		}

		// Bounds check for row
		if pos.Y < 0 || pos.Y >= gameHeight {
			continue
		}

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
	// Read game width from context
	gameWidth := cs.ctx.GameWidth

	// Read animation duration under lock
	cs.mu.RLock()
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

	// Initialize trail with starting position to ensure edge positions are checked
	// This prevents missing characters at x=0 (L→R) or x=gameWidth-1 (R→L)
	trail = append(trail, startX)

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
}

// updateCleanerPositions updates the position of all cleaner entities
func (cs *CleanerSystem) updateCleanerPositions(world *engine.World, deltaTime float64) {
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	entities := world.GetEntitiesWith(cleanerType)

	for _, entity := range entities {
		cleanerComp, ok := world.GetComponent(entity, cleanerType)
		if !ok || cleanerComp == nil {
			continue
		}
		cleaner, ok := cleanerComp.(components.CleanerComponent)
		if !ok {
			continue
		}

		// Update position based on speed and direction
		cleaner.XPosition += cleaner.Speed * float64(cleaner.Direction) * deltaTime

		// Acquire lock BEFORE modifying trail positions since the slice is shared with cleanerDataMap
		cs.stateMu.Lock()

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

		// Update tracked data (already holding lock)
		if data, exists := cs.cleanerDataMap[entity]; exists {
			data.xPosition = cleaner.XPosition
			data.trailPositions = cleaner.TrailPositions
		}

		cs.stateMu.Unlock()

		// Update component (after lock is released)
		world.AddComponent(entity, cleaner)

		// Check all trail positions for collisions (not just head)
		cs.checkTrailCollisions(world, cleaner.Row, cleaner.TrailPositions)
	}
}

// checkTrailCollisions checks all trail positions for Red character collisions
// Comprehensive collision detection that checks ALL integer positions between
// consecutive trail points to prevent gaps when cleaner moves >1 char per frame.
//
// Mathematical basis:
// - Cleaner speed: ~80 chars/sec (gameWidth=80, duration=1s)
// - Frame time: 16ms
// - Movement per frame: 80 × 0.016 = 1.28 characters
// - Without range checking, positions can be skipped (e.g., 8.84→10.12 skips 9)
//
// Solution: Check all integer positions between consecutive trail points
func (cs *CleanerSystem) checkTrailCollisions(world *engine.World, row int, trailPositions []float64) {
	// Defensive: Check for nil world
	if world == nil {
		return
	}

	// Read dimensions from context
	gameWidth := cs.ctx.GameWidth
	gameHeight := cs.ctx.GameHeight

	// Bounds check for row
	if row < 0 || row >= gameHeight {
		return
	}

	// Track which integer positions we've already checked to avoid duplicate checks
	checkedPositions := make(map[int]bool)

	// Check positions between consecutive trail points
	for i := 0; i < len(trailPositions); i++ {
		currentPos := trailPositions[i]

		// For first position, check single point
		// For subsequent positions, check range between previous and current
		var prevPos float64
		if i == 0 {
			// Current head position - check single point
			prevPos = currentPos
		} else {
			// Check range between previous and current
			prevPos = trailPositions[i-1]
		}

		// Check all integer positions in range [min, max]
		minX := int(math.Min(prevPos, currentPos))
		maxX := int(math.Max(prevPos, currentPos))

		// Clamp range to valid bounds [0, gameWidth-1] to include edge positions
		// This ensures we check x=0 (L→R) and x=gameWidth-1 (R→L) even when
		// the trail extends beyond the game area
		if minX < 0 {
			minX = 0
		}
		if maxX >= gameWidth {
			maxX = gameWidth - 1
		}

		// Skip if range is completely out of bounds
		if minX >= gameWidth || maxX < 0 {
			continue
		}

		for x := minX; x <= maxX; x++ {
			// Skip if already checked
			if checkedPositions[x] {
				continue
			}
			checkedPositions[x] = true

			// Check and destroy Red character at this position
			cs.checkAndDestroyAtPosition(world, x, row)
		}
	}
}

// checkAndDestroyAtPosition checks a specific position for Red characters and destroys them
// Checks entire trail with integer truncation for more reliable collision detection
func (cs *CleanerSystem) checkAndDestroyAtPosition(world *engine.World, x, y int) {
	// Defensive: Check for nil world
	if world == nil {
		return
	}

	// Read dimensions from context for bounds check
	gameWidth := cs.ctx.GameWidth
	gameHeight := cs.ctx.GameHeight

	if x < 0 || x >= gameWidth || y < 0 || y >= gameHeight {
		return
	}

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
	cs.firstUpdate.Store(false)
	cs.activationTime.Store(0)

	// Reset active cleaner count to 0 (not decrement, as we're doing full cleanup)
	cs.activeCleanerCount.Store(0)

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

// GetCleanerEntities returns all active cleaner entities (for rendering)
func (cs *CleanerSystem) GetCleanerEntities(world *engine.World) []engine.Entity {
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	return world.GetEntitiesWith(cleanerType)
}

// GetCleanerSnapshots returns a thread-safe snapshot of all active cleaners for rendering
// This prevents race conditions between the concurrent updateLoop and the render thread
func (cs *CleanerSystem) GetCleanerSnapshots() []render.CleanerSnapshot {
	cs.stateMu.RLock()
	defer cs.stateMu.RUnlock()

	snapshots := make([]render.CleanerSnapshot, 0, len(cs.cleanerDataMap))
	for _, data := range cs.cleanerDataMap {
		// Create a deep copy of trail positions to avoid sharing the slice
		trailCopy := make([]float64, len(data.trailPositions))
		copy(trailCopy, data.trailPositions)

		snapshots = append(snapshots, render.CleanerSnapshot{
			Row:            data.row,
			XPosition:      data.xPosition,
			TrailPositions: trailCopy,
			Char:           cs.config.Char,
		})
	}

	return snapshots
}

// Shutdown cleans up all resources
func (cs *CleanerSystem) Shutdown() {
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

// ActivateCleaners initiates the cleaner animation (called by ClockScheduler)
// This is an alias for TriggerCleaners to match the CleanerSystemInterface
func (cs *CleanerSystem) ActivateCleaners(world *engine.World) {
	cs.TriggerCleaners(world)
}

// IsAnimationComplete checks if the cleaner animation has finished (for ClockScheduler)
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

	return elapsed >= duration
}