package engine

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter"
)

// GameState centralizes game state with clear ownership boundaries
type GameState struct {
	// === REAL-TIME STATE (lock-free atomics) ===
	Mode atomic.Int32 // Game mode (core.GameMode); set by Router

	// Runtime Metrics
	GameTicks      atomic.Uint64
	CurrentAPM     atomic.Uint64
	PendingActions atomic.Uint64 // Admitted actions in the current second bucket
	MusicAPM       atomic.Uint64 // Short-term APM for dynamic music (last 5s)

	// === CLOCK-TICK STATE (mutex protected) ===

	mu sync.RWMutex

	// APM History (mutex protected)
	apmHistory      [60]uint64
	apmHistoryIndex int
	lastAPMTime     time.Time // Last time APM was updated

	// Pointer travel not yet counted, in columns, and the last cell the pointer named.
	// Written only by dispatch, under the world lock.
	pointerTravel      int
	pointerX, pointerY int
	pointerSeen        bool
	pointerCounted     uint64 // pointer gestures in the current bucket
	// The last keyed gesture, when it came, and how many times it has repeated in a row
	lastKey     uint64
	lastKeyTick uint64
	keyStreak   uint64
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
	gs.pointerTravel, gs.pointerX, gs.pointerY, gs.pointerSeen, gs.pointerCounted = 0, 0, 0, false, 0
	gs.lastKey, gs.lastKeyTick, gs.keyStreak = 0, 0, 0
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

// SetGameTicks adopts a tick count from installed shared state (D-19).
//
// The tick is shared identity, not a local counter: it names the instant every
// participant's simulation is at, and since SimTime derives the simulation clock
// from it, adopting the tick adopts the clock. A world installed without it would
// hold the sender's entities while reading a different instant, so every duration
// measured against a stored one — a quasar's speed step, a gold deadline — would
// resolve on a different tick than the run it claims to reproduce.
func (gs *GameState) SetGameTicks(ticks uint64) {
	gs.GameTicks.Store(ticks)
}

// MovePointer accrues the travel of a pointer placement. A row counts two columns,
// as a cell is twice as tall as it is wide.
func (gs *GameState) MovePointer(x, y int) {
	if gs.pointerSeen {
		gs.pointerTravel += abs(x-gs.pointerX) + 2*abs(y-gs.pointerY)
	}
	gs.pointerX, gs.pointerY, gs.pointerSeen = x, y, true
}

// AdmitAction counts one gesture for a dispatch pass: a keyed input, zero for none, or
// pointer travel once it covers APMPointerCells, which then spends all of it so one
// flick is one action however far it went. Pointer travel fills at most
// APMPointerPerSecond of a bucket, so the top of the tempo range asks for keys too.
func (gs *GameState) AdmitAction(key uint64) {
	counted := key != 0 && gs.pressKey(key)
	if gs.pointerTravel >= parameter.APMPointerCells {
		gs.pointerTravel = 0
		if !counted && gs.pointerCounted < parameter.APMPointerPerSecond {
			gs.pointerCounted++
			counted = true
		}
	}
	if counted {
		gs.PendingActions.Add(1)
	}
}

// pressKey reports whether a keyed gesture counts. One key mashed diminishes: each
// repeat within APMRepeatTicks of the last extends a streak, and only its 1st, 2nd,
// 4th, 8th... press counts, so a held or hammered key grows APM by its logarithm.
func (gs *GameState) pressKey(key uint64) bool {
	t := gs.GameTicks.Load()
	if key == gs.lastKey && t-gs.lastKeyTick < parameter.APMRepeatTicks {
		gs.keyStreak++
	} else {
		gs.keyStreak = 1
	}
	gs.lastKey, gs.lastKeyTick = key, t
	return gs.keyStreak&(gs.keyStreak-1) == 0
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

// UpdateAPM rolls the action history once per simulated second; the scheduler calls it
// every tick and owns the metric cells it publishes to.
func (gs *GameState) UpdateAPM(currentTime time.Time) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// First call anchors the window. Folding here would commit a bucket covering the
	// interval since the zero time and leave every session one tick out of phase.
	if gs.lastAPMTime.IsZero() {
		gs.lastAPMTime = currentTime
		return
	}

	// Only update if >= 1 second has passed
	// This ensures buckets represent 1s of data, making the multiplier (12) correct
	if currentTime.Sub(gs.lastAPMTime) < time.Second {
		return
	}

	// Commit this second's data
	gs.lastAPMTime = currentTime // Advance time anchor

	actions := min(gs.PendingActions.Swap(0), parameter.APMMaxPerSecond)
	gs.pointerCounted = 0

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
	for range burstWindow {
		burstTotal += gs.apmHistory[idx]
		idx--
		if idx < 0 {
			idx = len(gs.apmHistory) - 1
		}
	}
	// Normalize 5s window to 1-minute rate
	gs.MusicAPM.Store(burstTotal * (60 / burstWindow))
}
