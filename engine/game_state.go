package engine

import (
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/status"
)

// GameState centralizes game state with clear ownership boundaries
type GameState struct {
	// === REAL-TIME STATE (lock-free atomics) ===
	Mode atomic.Int32 // Game mode (core.GameMode); set by Router

	// Runtime Metrics
	GameTicks      atomic.Uint64
	CurrentAPM     atomic.Uint64
	PendingActions atomic.Uint64 // Actions in the current second bucket

	// === CLOCK-TICK STATE (mutex protected) ===

	mu sync.RWMutex

	// APM History (mutex protected)
	apmHistory      [60]uint64
	apmHistoryIndex int
}

// initState initializes all game state fields to starting values
// Called by both NewGameState and Reset to avoid duplication
func (gs *GameState) initState() {
	// Reset game mode
	gs.Mode.Store(int32(core.ModeNormal))
	// Reset metrics
	gs.GameTicks.Store(0)
	gs.CurrentAPM.Store(0)
	gs.PendingActions.Store(0)

	// Mutex-protected fields (caller may or may not hold lock)
	gs.apmHistory = [60]uint64{}
	gs.apmHistoryIndex = 0
}

// NewGameState creates a new centralized game state
func NewGameState() *GameState {
	gs := &GameState{}
	gs.initState()
	return gs
}

// Reset resets the game state for a new game
// Ensures clean state for :new command without recreation
func (gs *GameState) Reset() {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.initState()
}

// === RUNTIME METRICS ACCESSORS ===

// IncrementGameTicks increments the game tick counter
func (gs *GameState) IncrementGameTicks() (new uint64) {
	return gs.GameTicks.Add(1)
}

// GetGameTicks returns the current game tick count
func (gs *GameState) GetGameTicks() uint64 {
	return gs.GameTicks.Load()
}

// RecordAction increments the pending action counter for APM calculation
func (gs *GameState) RecordAction() {
	gs.PendingActions.Add(1)
}

// GetAPM returns the current calculated APM
func (gs *GameState) GetAPM() uint64 {
	return gs.CurrentAPM.Load()
}

// GetMode returns current game mode
func (gs *GameState) GetMode() core.GameMode {
	return core.GameMode(gs.Mode.Load())
}

// SetMode sets current game mode
func (gs *GameState) SetMode(m core.GameMode) {
	gs.Mode.Store(int32(m))
}

// UpdateAPM rolls action history window and recalculates APM, called ~1/sec by scheduler, publishes results to status registry
func (gs *GameState) UpdateAPM(registry *status.Registry) {
	// Atomically swap pending actions to 0 to start new bucket
	actions := gs.PendingActions.Swap(0)

	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Update history ring buffer
	gs.apmHistory[gs.apmHistoryIndex] = actions
	gs.apmHistoryIndex = (gs.apmHistoryIndex + 1) % len(gs.apmHistory)

	// Calculate total over last 60 seconds
	var total uint64
	for _, count := range gs.apmHistory {
		total += count
	}

	gs.CurrentAPM.Store(total)

	// Publish to registry
	if registry != nil {
		registry.Ints.Get("engine.apm").Store(int64(total))
	}
}