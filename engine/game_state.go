package engine

import (
	"sync"
	"sync/atomic"
	"time"
)

// GameState centralizes game state with clear ownership boundaries
// Phase 1: Focus on spawn/content state management
type GameState struct {
	// ===== REAL-TIME STATE (lock-free atomics) =====
	// Updated immediately on user input/spawn, read by all systems

	// Scoring and Heat (typing feedback)
	Score atomic.Int64 // Current score
	Heat  atomic.Int64 // Current heat (was scoreIncrement)

	// Cursor position (game coordinates) - needed for spawn exclusion zone
	CursorX atomic.Int32
	CursorY atomic.Int32

	// Color tracking counters (6 states: Blue×3 + Green×3)
	// Updated atomically on spawn, typing, and decay
	BlueCountBright  atomic.Int64
	BlueCountNormal  atomic.Int64
	BlueCountDark    atomic.Int64
	GreenCountBright atomic.Int64
	GreenCountNormal atomic.Int64
	GreenCountDark   atomic.Int64

	// Boost state (real-time feedback)
	BoostEnabled atomic.Bool
	BoostEndTime atomic.Int64 // UnixNano
	BoostColor   atomic.Int32 // 0=None, 1=Blue, 2=Green

	// Visual feedback (error flash, score blink)
	CursorError      atomic.Bool
	CursorErrorTime  atomic.Int64 // UnixNano
	ScoreBlinkActive atomic.Bool
	ScoreBlinkColor  atomic.Uint32
	ScoreBlinkTime   atomic.Int64 // UnixNano

	// Ping grid (immediate visual aid)
	PingActive    atomic.Bool
	PingGridTimer atomic.Uint64 // float64 bits
	PingRow       atomic.Int32
	PingCol       atomic.Int32

	// Sequence ID generation (atomic for thread-safety)
	NextSeqID atomic.Int64

	// ===== CLOCK-TICK STATE (mutex protected) =====
	// Updated only during clock tick, read by all systems

	mu sync.RWMutex

	// Spawn/Content State (what we're migrating in Phase 1)
	SpawnLastTime       time.Time // When last spawn occurred
	SpawnNextTime       time.Time // When next spawn should occur
	SpawnRateMultiplier float64   // 0.5x, 1.0x, 2.0x based on screen fill
	SpawnEnabled        bool      // Whether spawning is active

	// Screen fill tracking (for adaptive spawn rate)
	EntityCount    int // Current number of entities on screen
	MaxEntities    int // Maximum allowed entities (200)
	ScreenDensity  float64 // Percentage of screen filled (0.0-1.0)

	// ===== CONFIGURATION (read-only after init) =====
	// Set once at initialization, never mutated

	GameWidth   int
	GameHeight  int
	ScreenWidth int

	// Time provider (for consistent timing)
	TimeProvider TimeProvider
}

// NewGameState creates a new centralized game state
func NewGameState(gameWidth, gameHeight, screenWidth int, timeProvider TimeProvider) *GameState {
	gs := &GameState{
		GameWidth:    gameWidth,
		GameHeight:   gameHeight,
		ScreenWidth:  screenWidth,
		TimeProvider: timeProvider,
		MaxEntities:  200, // constants.MAX_CHARACTERS
	}

	// Initialize atomics to zero values
	gs.Score.Store(0)
	gs.Heat.Store(0)
	gs.CursorX.Store(int32(gameWidth / 2))
	gs.CursorY.Store(int32(gameHeight / 2))

	// Initialize color counters to 0
	gs.BlueCountBright.Store(0)
	gs.BlueCountNormal.Store(0)
	gs.BlueCountDark.Store(0)
	gs.GreenCountBright.Store(0)
	gs.GreenCountNormal.Store(0)
	gs.GreenCountDark.Store(0)

	// Initialize boost state
	gs.BoostEnabled.Store(false)
	gs.BoostEndTime.Store(0)
	gs.BoostColor.Store(0)

	// Initialize visual feedback
	gs.CursorError.Store(false)
	gs.CursorErrorTime.Store(0)
	gs.ScoreBlinkActive.Store(false)
	gs.ScoreBlinkColor.Store(0)
	gs.ScoreBlinkTime.Store(0)

	// Initialize ping grid
	gs.PingActive.Store(false)
	gs.PingGridTimer.Store(0)
	gs.PingRow.Store(0)
	gs.PingCol.Store(0)

	// Initialize sequence ID
	gs.NextSeqID.Store(1)

	// Initialize clock-tick state
	now := timeProvider.Now()
	gs.SpawnLastTime = now
	gs.SpawnNextTime = now.Add(2 * time.Second) // Initial spawn delay
	gs.SpawnRateMultiplier = 1.0
	gs.SpawnEnabled = true
	gs.EntityCount = 0
	gs.ScreenDensity = 0.0

	return gs
}

// ===== HEAT ACCESSORS (atomic) =====

func (gs *GameState) GetHeat() int {
	return int(gs.Heat.Load())
}

func (gs *GameState) SetHeat(heat int) {
	gs.Heat.Store(int64(heat))
}

func (gs *GameState) AddHeat(delta int) {
	gs.Heat.Add(int64(delta))
}

// ===== SCORE ACCESSORS (atomic) =====

func (gs *GameState) GetScore() int {
	return int(gs.Score.Load())
}

func (gs *GameState) SetScore(score int) {
	gs.Score.Store(int64(score))
}

func (gs *GameState) AddScore(delta int) {
	gs.Score.Add(int64(delta))
}

// ===== CURSOR ACCESSORS (atomic) =====

func (gs *GameState) GetCursorX() int {
	return int(gs.CursorX.Load())
}

func (gs *GameState) SetCursorX(x int) {
	gs.CursorX.Store(int32(x))
}

func (gs *GameState) GetCursorY() int {
	return int(gs.CursorY.Load())
}

func (gs *GameState) SetCursorY(y int) {
	gs.CursorY.Store(int32(y))
}

// ===== COLOR COUNTER ACCESSORS (atomic) =====

// AddColorCount atomically updates the color counter for a given type and level
func (gs *GameState) AddColorCount(seqType, seqLevel int, delta int) {
	// seqType: 0=Blue, 1=Green (simplified from components.SequenceType)
	// seqLevel: 0=Dark, 1=Normal, 2=Bright (simplified from components.SequenceLevel)

	var counter *atomic.Int64

	if seqType == 0 { // Blue
		switch seqLevel {
		case 2: // Bright
			counter = &gs.BlueCountBright
		case 1: // Normal
			counter = &gs.BlueCountNormal
		case 0: // Dark
			counter = &gs.BlueCountDark
		}
	} else if seqType == 1 { // Green
		switch seqLevel {
		case 2: // Bright
			counter = &gs.GreenCountBright
		case 1: // Normal
			counter = &gs.GreenCountNormal
		case 0: // Dark
			counter = &gs.GreenCountDark
		}
	}

	if counter != nil {
		counter.Add(int64(delta))
		// Prevent negative counts
		for {
			current := counter.Load()
			if current >= 0 {
				break
			}
			if counter.CompareAndSwap(current, 0) {
				break
			}
		}
	}
}

// GetTotalColorCount returns the total number of tracked color/level combinations
func (gs *GameState) GetTotalColorCount() int {
	total := 0
	if gs.BlueCountBright.Load() > 0 {
		total++
	}
	if gs.BlueCountNormal.Load() > 0 {
		total++
	}
	if gs.BlueCountDark.Load() > 0 {
		total++
	}
	if gs.GreenCountBright.Load() > 0 {
		total++
	}
	if gs.GreenCountNormal.Load() > 0 {
		total++
	}
	if gs.GreenCountDark.Load() > 0 {
		total++
	}
	return total
}

// CanSpawnNewColor returns true if a new color/level combination can be spawned
// Limited to 6 color/level combinations (Blue×3 + Green×3)
func (gs *GameState) CanSpawnNewColor() bool {
	return gs.GetTotalColorCount() < 6
}

// ===== SEQUENCE ID ACCESSORS (atomic) =====

func (gs *GameState) GetNextSeqID() int {
	return int(gs.NextSeqID.Load())
}

func (gs *GameState) IncrementSeqID() int {
	return int(gs.NextSeqID.Add(1))
}

// ===== BOOST ACCESSORS (atomic) =====

func (gs *GameState) GetBoostEnabled() bool {
	return gs.BoostEnabled.Load()
}

func (gs *GameState) SetBoostEnabled(enabled bool) {
	gs.BoostEnabled.Store(enabled)
}

func (gs *GameState) GetBoostEndTime() time.Time {
	nano := gs.BoostEndTime.Load()
	if nano == 0 {
		return time.Time{}
	}
	return time.Unix(0, nano)
}

func (gs *GameState) SetBoostEndTime(t time.Time) {
	gs.BoostEndTime.Store(t.UnixNano())
}

func (gs *GameState) GetBoostColor() int32 {
	return gs.BoostColor.Load()
}

func (gs *GameState) SetBoostColor(color int32) {
	gs.BoostColor.Store(color)
}

// UpdateBoostTimerAtomic atomically checks if boost should expire and disables it
func (gs *GameState) UpdateBoostTimerAtomic() bool {
	if !gs.BoostEnabled.Load() {
		return false
	}

	now := gs.TimeProvider.Now()
	endTimeNano := gs.BoostEndTime.Load()
	if endTimeNano == 0 {
		return false
	}
	endTime := time.Unix(0, endTimeNano)

	if now.After(endTime) {
		if gs.BoostEnabled.CompareAndSwap(true, false) {
			gs.BoostColor.Store(0) // Reset to None
			return true
		}
	}

	return false
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
	if density < 0.3 {
		gs.SpawnRateMultiplier = 2.0 // Spawn faster
	} else if density > 0.7 {
		gs.SpawnRateMultiplier = 0.5 // Spawn slower
	} else {
		gs.SpawnRateMultiplier = 1.0 // Normal rate
	}
}

// ShouldSpawn checks if it's time to spawn new content
func (gs *GameState) ShouldSpawn() bool {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	if !gs.SpawnEnabled {
		return false
	}

	now := gs.TimeProvider.Now()
	return now.After(gs.SpawnNextTime) || now.Equal(gs.SpawnNextTime)
}

// SetSpawnEnabled enables or disables spawning
func (gs *GameState) SetSpawnEnabled(enabled bool) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	gs.SpawnEnabled = enabled
}

// ===== VISUAL FEEDBACK ACCESSORS (atomic) =====

func (gs *GameState) GetCursorError() bool {
	return gs.CursorError.Load()
}

func (gs *GameState) SetCursorError(err bool) {
	gs.CursorError.Store(err)
}

func (gs *GameState) GetCursorErrorTime() time.Time {
	nano := gs.CursorErrorTime.Load()
	if nano == 0 {
		return time.Time{}
	}
	return time.Unix(0, nano)
}

func (gs *GameState) SetCursorErrorTime(t time.Time) {
	gs.CursorErrorTime.Store(t.UnixNano())
}

func (gs *GameState) GetScoreBlinkActive() bool {
	return gs.ScoreBlinkActive.Load()
}

func (gs *GameState) SetScoreBlinkActive(active bool) {
	gs.ScoreBlinkActive.Store(active)
}

func (gs *GameState) GetScoreBlinkColor() uint32 {
	return gs.ScoreBlinkColor.Load()
}

func (gs *GameState) SetScoreBlinkColor(color uint32) {
	gs.ScoreBlinkColor.Store(color)
}

func (gs *GameState) GetScoreBlinkTime() time.Time {
	nano := gs.ScoreBlinkTime.Load()
	if nano == 0 {
		return time.Time{}
	}
	return time.Unix(0, nano)
}

func (gs *GameState) SetScoreBlinkTime(t time.Time) {
	gs.ScoreBlinkTime.Store(t.UnixNano())
}
