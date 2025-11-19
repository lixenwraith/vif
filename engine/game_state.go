package engine

import (
	"sync"
	"sync/atomic"
	"time"
)

// GameState centralizes game state with clear ownership boundaries
type GameState struct {
	// ===== REAL-TIME STATE (lock-free atomics) =====
	// Updated immediately on user input/spawn, read by all systems

	// Scoring and Heat (typing feedback)
	Score atomic.Int64 // Current score
	Heat  atomic.Int64 // Current heat value

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
	ScoreBlinkType   atomic.Uint32 // 0=error, 1=blue, 2=green, 3=red, 4=gold
	ScoreBlinkLevel  atomic.Uint32 // 0=dark, 1=normal, 2=bright
	ScoreBlinkTime   atomic.Int64  // UnixNano

	// Ping grid (immediate visual aid)
	PingActive    atomic.Bool
	PingGridTimer atomic.Uint64 // float64 bits
	PingRow       atomic.Int32
	PingCol       atomic.Int32

	// Sequence ID generation (atomic for thread-safety)
	NextSeqID atomic.Int64

	// Frame counter (atomic for thread-safety, incremented each render)
	FrameNumber atomic.Int64

	// ===== CLOCK-TICK STATE (mutex protected) =====
	// Updated only during clock tick, read by all systems

	mu sync.RWMutex

	// Spawn/Content State (what we're migrating in Phase 1)
	SpawnLastTime       time.Time // When last spawn occurred
	SpawnNextTime       time.Time // When next spawn should occur
	SpawnRateMultiplier float64   // 0.5x, 1.0x, 2.0x based on screen fill
	SpawnEnabled        bool      // Whether spawning is active

	// Screen fill tracking (for adaptive spawn rate)
	EntityCount   int     // Current number of entities on screen
	MaxEntities   int     // Maximum allowed entities (200)
	ScreenDensity float64 // Percentage of screen filled (0.0-1.0)

	// Phase State (Infrastructure)
	// Controls which game mechanic is active (Normal, Gold, Decay Wait, Decay Animation)
	// Will add transition logic between phases
	CurrentPhase   GamePhase // Current game phase
	PhaseStartTime time.Time // When current phase started

	// Gold Sequence State (Migrated from GoldSequenceSystem)
	GoldActive      bool      // Whether gold sequence is active
	GoldSequenceID  int       // Current gold sequence ID
	GoldStartTime   time.Time // When gold spawned
	GoldTimeoutTime time.Time // When gold will timeout (10s from start)

	// Decay Timer State (Migrated from DecaySystem)
	DecayTimerActive bool      // Whether decay timer has been started
	DecayNextTime    time.Time // When decay will trigger

	// Decay Animation State (Migrated from DecaySystem)
	DecayAnimating bool      // Whether decay animation is running
	DecayStartTime time.Time // When decay animation started

	// Cleaner State (Migrated from CleanerSystem)
	// Cleaners run in parallel with other phases (not blocking)
	CleanerPending   bool      // Whether cleaners should be triggered on next clock tick
	CleanerActive    bool      // Whether cleaners are currently running
	CleanerStartTime time.Time // When cleaners were activated

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
	gs.ScoreBlinkType.Store(0)
	gs.ScoreBlinkLevel.Store(0)
	gs.ScoreBlinkTime.Store(0)

	// Initialize ping grid
	gs.PingActive.Store(false)
	gs.PingGridTimer.Store(0)
	gs.PingRow.Store(0)
	gs.PingCol.Store(0)

	// Initialize sequence ID
	gs.NextSeqID.Store(1)

	// Initialize frame counter
	gs.FrameNumber.Store(0)

	// Initialize clock-tick state
	now := timeProvider.Now()
	gs.SpawnLastTime = now
	gs.SpawnNextTime = now.Add(2 * time.Second) // Initial spawn delay
	gs.SpawnRateMultiplier = 1.0
	gs.SpawnEnabled = true
	gs.EntityCount = 0
	gs.ScreenDensity = 0.0

	// Initialize phase state (Start in Normal phase)
	gs.CurrentPhase = PhaseNormal
	gs.PhaseStartTime = now

	// Initialize Gold sequence state
	gs.GoldActive = false
	gs.GoldSequenceID = 0
	gs.GoldStartTime = time.Time{}
	gs.GoldTimeoutTime = time.Time{}

	// Initialize Decay timer state (Phase 3)
	gs.DecayTimerActive = false
	gs.DecayNextTime = time.Time{}

	// Initialize Decay animation state (Phase 3)
	gs.DecayAnimating = false
	gs.DecayStartTime = time.Time{}

	// Initialize Cleaner state (Phase 6)
	gs.CleanerPending = false
	gs.CleanerActive = false
	gs.CleanerStartTime = time.Time{}

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

// ===== FRAME COUNTER ACCESSORS (atomic) =====

func (gs *GameState) GetFrameNumber() int64 {
	return gs.FrameNumber.Load()
}

func (gs *GameState) IncrementFrameNumber() int64 {
	return gs.FrameNumber.Add(1)
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

func (gs *GameState) GetScoreBlinkType() uint32 {
	return gs.ScoreBlinkType.Load()
}

func (gs *GameState) SetScoreBlinkType(seqType uint32) {
	gs.ScoreBlinkType.Store(seqType)
}

func (gs *GameState) GetScoreBlinkLevel() uint32 {
	return gs.ScoreBlinkLevel.Load()
}

func (gs *GameState) SetScoreBlinkLevel(level uint32) {
	gs.ScoreBlinkLevel.Store(level)
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

// ===== PHASE STATE ACCESSORS (mutex protected) =====

// GetPhase returns the current game phase
func (gs *GameState) GetPhase() GamePhase {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.CurrentPhase
}

// SetPhase updates the current game phase and resets the phase start time
// DEPRECATED: Use TransitionPhase() for validated transitions
func (gs *GameState) SetPhase(phase GamePhase) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.CurrentPhase = phase
	gs.PhaseStartTime = gs.TimeProvider.Now()
}

// CanTransition checks if a phase transition is valid
func (gs *GameState) CanTransition(from, to GamePhase) bool {
	validTransitions := map[GamePhase][]GamePhase{
		PhaseNormal:         {PhaseGoldActive, PhaseCleanerPending},
		PhaseGoldActive:     {PhaseGoldComplete, PhaseCleanerPending},
		PhaseGoldComplete:   {PhaseDecayWait, PhaseCleanerPending},
		PhaseDecayWait:      {PhaseDecayAnimation},
		PhaseDecayAnimation: {PhaseNormal},
		PhaseCleanerPending: {PhaseCleanerActive},
		PhaseCleanerActive:  {PhaseDecayWait},
	}

	allowed := validTransitions[from]
	for _, phase := range allowed {
		if phase == to {
			return true
		}
	}
	return false
}

// TransitionPhase attempts to transition to a new phase with validation
// Returns true if transition succeeded, false if transition is invalid
func (gs *GameState) TransitionPhase(to GamePhase) bool {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	if !gs.CanTransition(gs.CurrentPhase, to) {
		return false
	}

	gs.CurrentPhase = to
	gs.PhaseStartTime = gs.TimeProvider.Now()
	return true
}

// GetPhaseStartTime returns when the current phase started
func (gs *GameState) GetPhaseStartTime() time.Time {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.PhaseStartTime
}

// GetPhaseDuration returns how long the current phase has been active
func (gs *GameState) GetPhaseDuration() time.Duration {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.TimeProvider.Now().Sub(gs.PhaseStartTime)
}

// PhaseSnapshot provides a consistent view of phase state
type PhaseSnapshot struct {
	Phase     GamePhase
	StartTime time.Time
	Duration  time.Duration
}

// ReadPhaseState returns a consistent snapshot of the current phase state
func (gs *GameState) ReadPhaseState() PhaseSnapshot {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	now := gs.TimeProvider.Now()
	return PhaseSnapshot{
		Phase:     gs.CurrentPhase,
		StartTime: gs.PhaseStartTime,
		Duration:  now.Sub(gs.PhaseStartTime),
	}
}

// ===== GOLD SEQUENCE STATE ACCESSORS (mutex protected) =====

// GetGoldActive returns whether a gold sequence is active
func (gs *GameState) GetGoldActive() bool {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.GoldActive
}

// SetGoldActive sets whether a gold sequence is active
func (gs *GameState) SetGoldActive(active bool) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.GoldActive = active
}

// GetGoldSequenceID returns the current gold sequence ID
func (gs *GameState) GetGoldSequenceID() int {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.GoldSequenceID
}

// IncrementGoldSequenceID increments and returns the next gold sequence ID
func (gs *GameState) IncrementGoldSequenceID() int {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.GoldSequenceID++
	return gs.GoldSequenceID
}

// ActivateGoldSequence atomically activates a gold sequence with timeout
// Only allowed from PhaseNormal (checked by phase transition validation)
func (gs *GameState) ActivateGoldSequence(sequenceID int, duration time.Duration) bool {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Validate phase transition
	if !gs.CanTransition(gs.CurrentPhase, PhaseGoldActive) {
		return false
	}

	now := gs.TimeProvider.Now()
	gs.GoldActive = true
	gs.GoldSequenceID = sequenceID
	gs.GoldStartTime = now
	gs.GoldTimeoutTime = now.Add(duration)
	gs.CurrentPhase = PhaseGoldActive
	gs.PhaseStartTime = now
	return true
}

// DeactivateGoldSequence atomically deactivates the gold sequence
// Transitions to PhaseGoldComplete to allow decay or cleaner to start
func (gs *GameState) DeactivateGoldSequence() bool {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Validate phase transition
	if !gs.CanTransition(gs.CurrentPhase, PhaseGoldComplete) {
		return false
	}

	gs.GoldActive = false
	gs.GoldStartTime = time.Time{}
	gs.GoldTimeoutTime = time.Time{}
	gs.CurrentPhase = PhaseGoldComplete
	gs.PhaseStartTime = gs.TimeProvider.Now()
	return true
}

// GetGoldTimeoutTime returns when the gold sequence will timeout
func (gs *GameState) GetGoldTimeoutTime() time.Time {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.GoldTimeoutTime
}

// IsGoldTimedOut checks if the gold sequence has timed out
func (gs *GameState) IsGoldTimedOut() bool {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	if !gs.GoldActive {
		return false
	}
	return gs.TimeProvider.Now().After(gs.GoldTimeoutTime)
}

// GoldSnapshot provides a consistent view of gold state
type GoldSnapshot struct {
	Active      bool
	SequenceID  int
	StartTime   time.Time
	TimeoutTime time.Time
	Elapsed     time.Duration
	Remaining   time.Duration
}

// ReadGoldState returns a consistent snapshot of the gold sequence state
func (gs *GameState) ReadGoldState() GoldSnapshot {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	now := gs.TimeProvider.Now()
	var elapsed, remaining time.Duration
	if gs.GoldActive {
		elapsed = now.Sub(gs.GoldStartTime)
		remaining = gs.GoldTimeoutTime.Sub(now)
		if remaining < 0 {
			remaining = 0
		}
	}

	return GoldSnapshot{
		Active:      gs.GoldActive,
		SequenceID:  gs.GoldSequenceID,
		StartTime:   gs.GoldStartTime,
		TimeoutTime: gs.GoldTimeoutTime,
		Elapsed:     elapsed,
		Remaining:   remaining,
	}
}

// ===== DECAY TIMER STATE ACCESSORS (mutex protected) =====

// GetDecayTimerActive returns whether the decay timer is active
func (gs *GameState) GetDecayTimerActive() bool {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.DecayTimerActive
}

// StartDecayTimer starts the decay timer with the given interval
// Calculates interval based on current heat atomically
// Only allowed from PhaseGoldComplete (checked by phase transition validation)
func (gs *GameState) StartDecayTimer(screenWidth int, baseSeconds, rangeSeconds float64) bool {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Validate phase transition
	if !gs.CanTransition(gs.CurrentPhase, PhaseDecayWait) {
		return false
	}

	// Read heat atomically (no cached value)
	heat := int(gs.Heat.Load())

	// Calculate heat bar width (now uses full screen width)
	heatBarWidth := screenWidth
	if heatBarWidth < 1 {
		heatBarWidth = 1
	}

	// Calculate heat percentage
	heatPercentage := float64(heat) / float64(heatBarWidth)
	if heatPercentage > 1.0 {
		heatPercentage = 1.0
	}
	if heatPercentage < 0.0 {
		heatPercentage = 0.0
	}

	// Formula: base - range * heat_percentage
	// Empty heat bar (0): 60 - 50 * 0 = 60 seconds
	// Full heat bar (max): 60 - 50 * 1 = 10 seconds
	intervalSeconds := baseSeconds - rangeSeconds*heatPercentage
	interval := time.Duration(intervalSeconds * float64(time.Second))

	now := gs.TimeProvider.Now()
	gs.DecayTimerActive = true
	gs.DecayNextTime = now.Add(interval)
	gs.CurrentPhase = PhaseDecayWait
	gs.PhaseStartTime = now
	return true
}

// GetDecayNextTime returns when the next decay will trigger
func (gs *GameState) GetDecayNextTime() time.Time {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.DecayNextTime
}

// IsDecayReady checks if the decay timer has expired
func (gs *GameState) IsDecayReady() bool {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	if !gs.DecayTimerActive {
		return false
	}
	now := gs.TimeProvider.Now()
	return now.After(gs.DecayNextTime) || now.Equal(gs.DecayNextTime)
}

// GetTimeUntilDecay returns seconds until next decay trigger
func (gs *GameState) GetTimeUntilDecay() float64 {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	if !gs.DecayTimerActive || gs.DecayAnimating {
		return 0.0
	}
	remaining := gs.DecayNextTime.Sub(gs.TimeProvider.Now()).Seconds()
	if remaining < 0 {
		remaining = 0
	}
	return remaining
}

// ===== DECAY ANIMATION STATE ACCESSORS (mutex protected) =====

// GetDecayAnimating returns whether decay animation is running
func (gs *GameState) GetDecayAnimating() bool {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.DecayAnimating
}

// StartDecayAnimation starts the decay animation
// Only allowed from PhaseDecayWait (checked by phase transition validation)
func (gs *GameState) StartDecayAnimation() bool {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Validate phase transition
	if !gs.CanTransition(gs.CurrentPhase, PhaseDecayAnimation) {
		return false
	}

	now := gs.TimeProvider.Now()
	gs.DecayAnimating = true
	gs.DecayStartTime = now
	gs.DecayTimerActive = false // Timer is no longer active once animation starts
	gs.CurrentPhase = PhaseDecayAnimation
	gs.PhaseStartTime = now
	return true
}

// StopDecayAnimation stops the decay animation and returns to Normal phase
// Only allowed from PhaseDecayAnimation (checked by phase transition validation)
func (gs *GameState) StopDecayAnimation() bool {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Validate phase transition
	if !gs.CanTransition(gs.CurrentPhase, PhaseNormal) {
		return false
	}

	now := gs.TimeProvider.Now()
	gs.DecayAnimating = false
	gs.DecayStartTime = time.Time{}
	gs.CurrentPhase = PhaseNormal
	gs.PhaseStartTime = now
	return true
}

// GetDecayStartTime returns when the decay animation started
func (gs *GameState) GetDecayStartTime() time.Time {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.DecayStartTime
}

// DecaySnapshot provides a consistent view of decay state
type DecaySnapshot struct {
	TimerActive bool
	NextTime    time.Time
	Animating   bool
	StartTime   time.Time
	TimeUntil   float64
}

// ReadDecayState returns a consistent snapshot of the decay state
func (gs *GameState) ReadDecayState() DecaySnapshot {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	timeUntil := 0.0
	if gs.DecayTimerActive && !gs.DecayAnimating {
		remaining := gs.DecayNextTime.Sub(gs.TimeProvider.Now()).Seconds()
		if remaining > 0 {
			timeUntil = remaining
		}
	}

	return DecaySnapshot{
		TimerActive: gs.DecayTimerActive,
		NextTime:    gs.DecayNextTime,
		Animating:   gs.DecayAnimating,
		StartTime:   gs.DecayStartTime,
		TimeUntil:   timeUntil,
	}
}

// ===== CLEANER STATE ACCESSORS (mutex protected) =====

// RequestCleaners requests that cleaners be triggered on the next clock tick
// Called by ScoreSystem when gold sequence is completed at max heat
// Transitions to PhaseCleanerPending from PhaseGoldActive, PhaseNormal or PhaseGoldComplete
// Also deactivates gold sequence if it was active
func (gs *GameState) RequestCleaners() bool {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Validate phase transition
	if !gs.CanTransition(gs.CurrentPhase, PhaseCleanerPending) {
		return false
	}

	// Deactivate gold sequence if it was active
	if gs.GoldActive {
		gs.GoldActive = false
		gs.GoldStartTime = time.Time{}
		gs.GoldTimeoutTime = time.Time{}
	}

	gs.CleanerPending = true
	gs.CurrentPhase = PhaseCleanerPending
	gs.PhaseStartTime = gs.TimeProvider.Now()
	return true
}

// GetCleanerPending returns whether cleaners are pending activation
func (gs *GameState) GetCleanerPending() bool {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.CleanerPending
}

// ActivateCleaners atomically activates cleaners and clears pending flag
// Called by ClockScheduler when processing pending cleaner request
// Transitions to PhaseCleanerActive from PhaseCleanerPending
func (gs *GameState) ActivateCleaners() bool {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Validate phase transition
	if !gs.CanTransition(gs.CurrentPhase, PhaseCleanerActive) {
		return false
	}

	now := gs.TimeProvider.Now()
	gs.CleanerPending = false
	gs.CleanerActive = true
	gs.CleanerStartTime = now
	gs.CurrentPhase = PhaseCleanerActive
	gs.PhaseStartTime = now
	return true
}

// GetCleanerActive returns whether cleaners are currently active
func (gs *GameState) GetCleanerActive() bool {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.CleanerActive
}

// DeactivateCleaners atomically deactivates cleaners
// Called by ClockScheduler when cleaner animation completes
// Transitions to PhaseNormal from PhaseCleanerActive
func (gs *GameState) DeactivateCleaners() bool {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Validate phase transition
	if !gs.CanTransition(gs.CurrentPhase, PhaseNormal) {
		return false
	}

	gs.CleanerActive = false
	gs.CleanerPending = false
	gs.CleanerStartTime = time.Time{}
	gs.CurrentPhase = PhaseDecayWait
	gs.PhaseStartTime = gs.TimeProvider.Now()
	return true
}

// GetCleanerStartTime returns when cleaners were activated
func (gs *GameState) GetCleanerStartTime() time.Time {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.CleanerStartTime
}

// CleanerSnapshot provides a consistent view of cleaner state
type CleanerSnapshot struct {
	Pending   bool
	Active    bool
	StartTime time.Time
	Elapsed   time.Duration
}

// ReadCleanerState returns a consistent snapshot of the cleaner state
func (gs *GameState) ReadCleanerState() CleanerSnapshot {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	elapsed := time.Duration(0)
	if gs.CleanerActive {
		elapsed = gs.TimeProvider.Now().Sub(gs.CleanerStartTime)
	}

	return CleanerSnapshot{
		Pending:   gs.CleanerPending,
		Active:    gs.CleanerActive,
		StartTime: gs.CleanerStartTime,
		Elapsed:   elapsed,
	}
}