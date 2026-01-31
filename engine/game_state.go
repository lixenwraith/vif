package engine

import (
	"sync"
	"sync/atomic"
	"time"

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
	MusicAPM       atomic.Uint64 // Short-term APM for dynamic music (last 5s)

	// === CLOCK-TICK STATE (mutex protected) ===

	mu sync.RWMutex

	// APM History (mutex protected)
	apmHistory      [60]uint64
	apmHistoryIndex int
	lastAPMTime     time.Time // Last time APM was updated
}

// initState initializes all game state fields to starting values
// Called by both NewGameState and Reset to avoid duplication
func (gs *GameState) initState() {
	// Reset game mode
	gs.Mode.Store(int32(core.ModeNormal))
	// Reset metrics
	gs.GameTicks.Store(0)
	gs.CurrentAPM.Store(0)
	gs.MusicAPM.Store(0)
	gs.PendingActions.Store(0)

	// Mutex-protected fields (caller may or may not hold lock)
	gs.apmHistory = [60]uint64{}
	gs.apmHistoryIndex = 0
	gs.lastAPMTime = time.Time{} // Zero value forces immediate update on first tick
}

// NewGameState creates a new centralized game state
func NewGameState() *GameState {
	gs := &GameState{}
	gs.initState()
	return gs
}

// Reset clears and resets the game state for a new game without recreation
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

// GetMusicAPM returns the short-term APM (5s average) for dynamic music
func (gs *GameState) GetMusicAPM() uint64 {
	return gs.MusicAPM.Load()
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
func (gs *GameState) UpdateAPM(registry *status.Registry, currentTime time.Time) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Only update if >= 1 second has passed
	// This ensures buckets represent 1s of data, making the multiplier (12) correct
	if currentTime.Sub(gs.lastAPMTime) < time.Second {
		return
	}

	// Commit this second's data
	gs.lastAPMTime = currentTime // Advance time anchor

	// Atomically swap pending actions to 0 to start new bucket
	actions := gs.PendingActions.Swap(0)

	// Update history ring buffer
	gs.apmHistory[gs.apmHistoryIndex] = actions
	gs.apmHistoryIndex = (gs.apmHistoryIndex + 1) % len(gs.apmHistory)

	// Calculate total over last 60 seconds (Standard APM)
	var total uint64
	for _, count := range gs.apmHistory {
		total += count
	}
	gs.CurrentAPM.Store(total)

	// Calculate total over last 5 seconds (Music/Burst APM)
	// We traverse backwards from current index
	var burstTotal uint64
	idx := gs.apmHistoryIndex - 1
	if idx < 0 {
		idx = len(gs.apmHistory) - 1
	}

	// Sum last 5 entries
	const burstWindow = 5
	for i := 0; i < burstWindow; i++ {
		burstTotal += gs.apmHistory[idx]
		idx--
		if idx < 0 {
			idx = len(gs.apmHistory) - 1
		}
	}
	// Normalize 5s window to 1-minute rate
	gs.MusicAPM.Store(burstTotal * (60 / burstWindow))

	// Publish to registry
	if registry != nil {
		registry.Ints.Get("engine.apm").Store(int64(total))
		registry.Ints.Get("engine.music_apm").Store(int64(gs.MusicAPM.Load()))
	}
}