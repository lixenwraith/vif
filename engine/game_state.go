package engine

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/constants"
)

// GameState centralizes game state with clear ownership boundaries
type GameState struct {
	// ===== REAL-TIME STATE (lock-free atomics) =====

	// Grayout visual effect state
	GrayoutActive    atomic.Bool
	GrayoutStartTime atomic.Int64 // UnixNano

	// Sequence ID generation (atomic for thread-safety)
	NextSeqID atomic.Int64

	// Frame counter (atomic for thread-safety, incremented each render)
	FrameNumber atomic.Int64

	// Runtime Metrics
	GameTicks      atomic.Uint64
	CurrentAPM     atomic.Uint64
	PendingActions atomic.Uint64 // Actions in the current second bucket

	// ===== CLOCK-TICK STATE (mutex protected) =====

	mu sync.RWMutex

	// APM History (mutex protected)
	apmHistory      [60]uint64
	apmHistoryIndex int

	// Spawn/Content State
	SpawnLastTime       time.Time // When last spawn occurred
	SpawnNextTime       time.Time // When next spawn should occur
	SpawnRateMultiplier float64   // 0.5x, 1.0x, 2.0x based on screen fill
	SpawnEnabled        bool      // Whether spawning is active

	// Screen fill tracking (for adaptive spawn rate)
	EntityCount   int     // Current number of entities on screen
	MaxEntities   int     // Maximum allowed entities
	ScreenDensity float64 // Percentage of screen filled (0.0-1.0)

	// Phase State (Infrastructure)
	// Controls which game mechanic is active (Normal, Gold, Decay Wait, Decay Animation)
	CurrentPhase   GamePhase // Current game phase
	PhaseStartTime time.Time // When current phase started
}

// initState initializes all game state fields to starting values
// Called by both NewGameState and Reset to avoid duplication
func (gs *GameState) initState(now time.Time) {
	// Reset atomics
	gs.GrayoutActive.Store(false)
	gs.GrayoutStartTime.Store(0)
	gs.NextSeqID.Store(1)
	gs.FrameNumber.Store(0)

	// Reset metrics
	gs.GameTicks.Store(0)
	gs.CurrentAPM.Store(0)
	gs.PendingActions.Store(0)

	// Mutex-protected fields (caller may or may not hold lock)
	gs.apmHistory = [60]uint64{}
	gs.apmHistoryIndex = 0

	// Spawn state
	gs.SpawnLastTime = now
	gs.SpawnNextTime = now
	gs.SpawnRateMultiplier = 1.0
	gs.SpawnEnabled = true
	gs.EntityCount = 0
	gs.ScreenDensity = 0.0

	// Phase state
	gs.CurrentPhase = PhaseNormal
	gs.PhaseStartTime = now
}

// NewGameState creates a new centralized game state
func NewGameState(maxEntities int, now time.Time) *GameState {
	gs := &GameState{
		MaxEntities: maxEntities,
	}
	gs.initState(now)
	return gs
}

// Reset resets the game state for a new game
// Ensures clean state for :new command without recreation
func (gs *GameState) Reset(now time.Time) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.initState(now)
}

// ===== PHASE STATE ACCESSORS (mutex protected) =====

// PhaseSnapshot provides a consistent view of phase state
type PhaseSnapshot struct {
	Phase     GamePhase
	StartTime time.Time
	Duration  time.Duration
}

// SetPhase updates phase state (called by ClockScheduler)
func (gs *GameState) SetPhase(phase GamePhase, now time.Time) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.CurrentPhase = phase
	gs.PhaseStartTime = now
}

// ReadPhaseState returns current phase snapshot
func (gs *GameState) ReadPhaseState(now time.Time) PhaseSnapshot {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return PhaseSnapshot{
		Phase:     gs.CurrentPhase,
		StartTime: gs.PhaseStartTime,
		Duration:  now.Sub(gs.PhaseStartTime),
	}
}

// ===== SEQUENCE ID ACCESSORS (atomic) =====

// GetNextSeqID returns the next sequence ID
func (gs *GameState) GetNextSeqID() int {
	return int(gs.NextSeqID.Load())
}

// IncrementSeqID increments and returns the next sequence ID
func (gs *GameState) IncrementSeqID() int {
	return int(gs.NextSeqID.Add(1))
}

// ===== FRAME COUNTER ACCESSORS (atomic) =====

// GetFrameNumber returns the current frame number
func (gs *GameState) GetFrameNumber() int64 {
	return gs.FrameNumber.Load()
}

// IncrementFrameNumber increments and returns the frame number
func (gs *GameState) IncrementFrameNumber() int64 {
	return gs.FrameNumber.Add(1)
}

// ===== SPAWN STATE ACCESSORS (mutex protected) =====

// SpawnStateSnapshot is a read-only snapshot for safe concurrent access
type SpawnStateSnapshot struct {
	LastTime       time.Time
	NextTime       time.Time
	RateMultiplier float64
	Enabled        bool
	EntityCount    int
	MaxEntities    int
	ScreenDensity  float64
}

// ReadSpawnState returns a consistent snapshot of spawn state
func (gs *GameState) ReadSpawnState() SpawnStateSnapshot {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	return SpawnStateSnapshot{
		LastTime:       gs.SpawnLastTime,
		NextTime:       gs.SpawnNextTime,
		RateMultiplier: gs.SpawnRateMultiplier,
		Enabled:        gs.SpawnEnabled,
		EntityCount:    gs.EntityCount,
		MaxEntities:    gs.MaxEntities,
		ScreenDensity:  gs.ScreenDensity,
	}
}

// UpdateSpawnTiming updates spawn timing state (called after successful spawn)
func (gs *GameState) UpdateSpawnTiming(lastTime, nextTime time.Time) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	gs.SpawnLastTime = lastTime
	gs.SpawnNextTime = nextTime
}

// UpdateSpawnRate updates the spawn rate multiplier based on screen density
func (gs *GameState) UpdateSpawnRate(entityCount, maxEntities int) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	gs.EntityCount = entityCount
	gs.MaxEntities = maxEntities

	// Calculate screen density (0.0 to 1.0)
	density := 0.0
	if maxEntities > 0 {
		density = float64(entityCount) / float64(maxEntities)
	}
	gs.ScreenDensity = density

	// Update spawn rate multiplier based on density
	// <30% filled: 2x faster (0.5s interval)
	// 30-70% filled: normal (2.0s interval)
	// >70% filled: 2x slower (4.0s interval)
	if density < constants.SpawnDensityLowThreshold {
		gs.SpawnRateMultiplier = constants.SpawnRateFast // Spawn faster
	} else if density > constants.SpawnDensityHighThreshold {
		gs.SpawnRateMultiplier = constants.SpawnRateSlow // Spawn slower
	} else {
		gs.SpawnRateMultiplier = constants.SpawnRateNormal // Normal rate
	}
}

// GetSpawnNextTime checks if it's time to spawn new content
func (gs *GameState) GetSpawnNextTime() time.Time {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.SpawnNextTime
}

// GetSpawnEnabled returns if content spawn is enabled
func (gs *GameState) GetSpawnEnabled() bool {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.SpawnEnabled
}

// SetSpawnEnabled enables or disables spawning
func (gs *GameState) SetSpawnEnabled(enabled bool) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	gs.SpawnEnabled = enabled
}

// ===== RUNTIME METRICS ACCESSORS =====

// IncrementGameTicks increments the game tick counter
func (gs *GameState) IncrementGameTicks() {
	gs.GameTicks.Add(1)
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

// UpdateAPM rolls the action history window and recalculates APM
// Should be called approximately every second by the scheduler
func (gs *GameState) UpdateAPM() {
	// atomically swap pending actions to 0 to start new bucket
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
}

// ResetRuntimeStats resets Ticks and APM statistics (for new game)
func (gs *GameState) ResetRuntimeStats() {
	gs.GameTicks.Store(0)
	gs.CurrentAPM.Store(0)
	gs.PendingActions.Store(0)

	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.apmHistory = [60]uint64{}
	gs.apmHistoryIndex = 0
}

// ===== GRAYOUT EFFECT ACCESSORS (atomic) =====

// TriggerGrayout activates the grayscale visual effect
func (gs *GameState) TriggerGrayout(now time.Time) {
	gs.GrayoutStartTime.Store(now.UnixNano())
	gs.GrayoutActive.Store(true)
}

// GetGrayoutIntensity returns current effect intensity (0.0 to 1.0)
// Returns 0.0 if effect inactive or duration exceeded
func (gs *GameState) GetGrayoutIntensity(now time.Time, duration time.Duration) float64 {
	if !gs.GrayoutActive.Load() {
		return 0.0
	}

	startNano := gs.GrayoutStartTime.Load()
	if startNano == 0 {
		return 0.0
	}

	elapsed := now.Sub(time.Unix(0, startNano))
	if elapsed >= duration {
		gs.GrayoutActive.Store(false)
		return 0.0
	}

	return 1.0 - (float64(elapsed) / float64(duration))
}