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

	// Scoring and Heat (typing feedback)
	Energy atomic.Int64 // Current energy
	Heat   atomic.Int64 // Current heat value

	// Boost state (real-time feedback)
	BoostEnabled atomic.Bool
	BoostEndTime atomic.Int64 // UnixNano
	BoostColor   atomic.Int32 // 0=None, 1=Blue, 2=Green

	// Visual feedback (error flash, energy blink)
	CursorError       atomic.Bool
	CursorErrorTime   atomic.Int64 // UnixNano
	EnergyBlinkActive atomic.Bool
	EnergyBlinkType   atomic.Uint32 // 0=error, 1=blue, 2=green, 3=red, 4=gold
	EnergyBlinkLevel  atomic.Uint32 // 0=dark, 1=normal, 2=bright
	EnergyBlinkTime   atomic.Int64  // UnixNano

	// Ping grid (immediate visual aid)
	PingActive    atomic.Bool
	PingGridTimer atomic.Uint64 // float64 bits

	// Drain entity tracking (real-time state for renderer snapshot)
	DrainActive atomic.Bool // Whether drain entity exists

	// Sequence ID generation (atomic for thread-safety)
	NextSeqID atomic.Int64

	// Frame counter (atomic for thread-safety, incremented each render)
	FrameNumber atomic.Int64

	// ===== CLOCK-TICK STATE (mutex protected) =====
	// Updated only during clock tick, read by all systems

	mu sync.RWMutex

	// Spawn/Content State
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
	CurrentPhase   GamePhase // Current game phase
	PhaseStartTime time.Time // When current phase started

	// Gold Sequence State
	// Tracks active gold sequence timing and lifecycle
	GoldActive      bool      // Whether gold sequence is active
	GoldSequenceID  int       // Current gold sequence ID
	GoldStartTime   time.Time // When gold spawned
	GoldTimeoutTime time.Time // When gold will timeout (10s from start)

	// Decay Timer State
	// Manages countdown between gold completion and decay animation trigger
	DecayTimerActive bool      // Whether decay timer has been started
	DecayNextTime    time.Time // When decay will trigger

	// Decay Animation State
	// Tracks falling decay animation lifecycle
	DecayAnimating bool      // Whether decay animation is running
	DecayStartTime time.Time // When decay animation started

	// Game Lifecycle State
	FirstUpdateTime      time.Time // When the game first started (first Update call)
	InitialSpawnComplete bool      // Whether initial gold spawn has been attempted

	// ===== CONFIGURATION (read-only after init) =====
	// NOTE: These should ideally be removed in favor of ConfigResource,
	// but kept here strictly for GameState internal calculations if necessary.
	// Systems should use Resources.

	// Set once at initialization, never mutated

	GameWidth   int
	GameHeight  int
	ScreenWidth int

	// DEPRECATED: TimeProvider should be removed in Phase 3 cleanup
	// Logic methods now accept time.Time explicitly
	// Time provider (for consistent timing)
	// TimeProvider TimeProvider
}

// NewGameState creates a new centralized game state
func NewGameState(gameWidth, gameHeight, screenWidth int, timeProvider TimeProvider) *GameState {
	gs := &GameState{
		GameWidth:   gameWidth,
		GameHeight:  gameHeight,
		ScreenWidth: screenWidth,
		// TimeProvider: timeProvider,
		MaxEntities: constants.MaxEntities,
	}

	// Initialize atomics to zero values
	gs.Energy.Store(0)
	gs.Heat.Store(0)

	// Initialize boost state
	gs.BoostEnabled.Store(false)
	gs.BoostEndTime.Store(0)
	gs.BoostColor.Store(0)

	// Initialize visual feedback
	gs.CursorError.Store(false)
	gs.CursorErrorTime.Store(0)
	gs.EnergyBlinkActive.Store(false)
	gs.EnergyBlinkType.Store(0)
	gs.EnergyBlinkLevel.Store(0)
	gs.EnergyBlinkTime.Store(0)

	// Initialize ping grid
	gs.PingActive.Store(false)
	gs.PingGridTimer.Store(0)

	// Initialize drain entity tracking
	gs.DrainActive.Store(false)

	// Initialize sequence ID
	gs.NextSeqID.Store(1)

	// Initialize frame counter
	gs.FrameNumber.Store(0)

	// Initialize clock-tick state
	now := timeProvider.Now()
	gs.SpawnLastTime = now
	gs.SpawnNextTime = now.Add(constants.InitialSpawnDelay) // Initial spawn delay
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

	// Initialize Decay timer state
	gs.DecayTimerActive = false
	gs.DecayNextTime = time.Time{}

	// Initialize Decay animation state
	gs.DecayAnimating = false
	gs.DecayStartTime = time.Time{}

	// Initialize Game Lifecycle state
	gs.FirstUpdateTime = time.Time{} // Will be set on first Update
	gs.InitialSpawnComplete = false

	return gs
}

// ===== HEAT ACCESSORS (atomic) =====

// GetHeat returns the current heat value
func (gs *GameState) GetHeat() int {
	return int(gs.Heat.Load())
}

// SetHeat sets the heat value
func (gs *GameState) SetHeat(heat int) {
	gs.Heat.Store(int64(heat))
}

// AddHeat adds a delta to the current heat value
func (gs *GameState) AddHeat(delta int) {
	gs.Heat.Add(int64(delta))
}

// ===== SCORE ACCESSORS (atomic) =====

// GetEnergy returns the current energy value
func (gs *GameState) GetEnergy() int {
	return int(gs.Energy.Load())
}

// SetEnergy sets the energy value
func (gs *GameState) SetEnergy(energy int) {
	gs.Energy.Store(int64(energy))
}

// AddEnergy adds a delta to the current energy value
func (gs *GameState) AddEnergy(delta int) {
	gs.Energy.Add(int64(delta))
}

// ReadHeatAndEnergy returns consistent snapshot of both heat and energy
func (gs *GameState) ReadHeatAndEnergy() (heat int64, energy int64) {
	// Read both atomic values sequentially for consistent view
	heat = gs.Heat.Load()
	energy = gs.Energy.Load()
	return heat, energy
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

// ===== BOOST ACCESSORS (atomic) =====

// GetBoostEnabled returns whether boost is currently enabled
func (gs *GameState) GetBoostEnabled() bool {
	return gs.BoostEnabled.Load()
}

// SetBoostEnabled sets the boost enabled state
func (gs *GameState) SetBoostEnabled(enabled bool) {
	gs.BoostEnabled.Store(enabled)
}

// GetBoostEndTime returns when the boost will end
func (gs *GameState) GetBoostEndTime() time.Time {
	nano := gs.BoostEndTime.Load()
	if nano == 0 {
		return time.Time{}
	}
	return time.Unix(0, nano)
}

// SetBoostEndTime sets when the boost will end
func (gs *GameState) SetBoostEndTime(t time.Time) {
	gs.BoostEndTime.Store(t.UnixNano())
}

// GetBoostColor returns the current boost color
func (gs *GameState) GetBoostColor() int32 {
	return gs.BoostColor.Load()
}

// SetBoostColor sets the boost color
func (gs *GameState) SetBoostColor(color int32) {
	gs.BoostColor.Store(color)
}

// UpdateBoostTimerAtomic atomically checks if boost should expire and disables it
func (gs *GameState) UpdateBoostTimerAtomic(now time.Time) bool {
	if !gs.BoostEnabled.Load() {
		return false
	}

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

// ReadBoostState returns a consistent snapshot of the boost state
func (gs *GameState) ReadBoostState(now time.Time) BoostSnapshot {
	// All boost fields are atomic, so we can read them without mutex
	enabled := gs.BoostEnabled.Load()
	endTimeNano := gs.BoostEndTime.Load()
	color := gs.BoostColor.Load()

	var endTime time.Time
	var remaining time.Duration

	if endTimeNano != 0 {
		endTime = time.Unix(0, endTimeNano)
		if enabled {
			remaining = endTime.Sub(now)
			if remaining < 0 {
				remaining = 0
			}
		}
	}

	return BoostSnapshot{
		Enabled:   enabled,
		EndTime:   endTime,
		Color:     color,
		Remaining: remaining,
	}
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
func (gs *GameState) ShouldSpawn(now time.Time) bool {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	if !gs.SpawnEnabled {
		return false
	}

	return now.After(gs.SpawnNextTime) || now.Equal(gs.SpawnNextTime)
}

// SetSpawnEnabled enables or disables spawning
func (gs *GameState) SetSpawnEnabled(enabled bool) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	gs.SpawnEnabled = enabled
}

// ===== VISUAL FEEDBACK ACCESSORS (atomic) =====

// GetCursorError returns whether a cursor error is active
func (gs *GameState) GetCursorError() bool {
	return gs.CursorError.Load()
}

// SetCursorError sets the cursor error state
func (gs *GameState) SetCursorError(err bool) {
	gs.CursorError.Store(err)
}

// GetCursorErrorTime returns when the cursor error started
func (gs *GameState) GetCursorErrorTime() time.Time {
	nano := gs.CursorErrorTime.Load()
	if nano == 0 {
		return time.Time{}
	}
	return time.Unix(0, nano)
}

// SetCursorErrorTime sets when the cursor error started
func (gs *GameState) SetCursorErrorTime(t time.Time) {
	gs.CursorErrorTime.Store(t.UnixNano())
}

// GetEnergyBlinkActive returns whether energy blink is active
func (gs *GameState) GetEnergyBlinkActive() bool {
	return gs.EnergyBlinkActive.Load()
}

// SetEnergyBlinkActive sets the energy blink active state
func (gs *GameState) SetEnergyBlinkActive(active bool) {
	gs.EnergyBlinkActive.Store(active)
}

// GetEnergyBlinkType returns the energy blink type
func (gs *GameState) GetEnergyBlinkType() uint32 {
	return gs.EnergyBlinkType.Load()
}

// SetEnergyBlinkType sets the energy blink type
func (gs *GameState) SetEnergyBlinkType(seqType uint32) {
	gs.EnergyBlinkType.Store(seqType)
}

// GetEnergyBlinkLevel returns the energy blink level
func (gs *GameState) GetEnergyBlinkLevel() uint32 {
	return gs.EnergyBlinkLevel.Load()
}

// SetEnergyBlinkLevel sets the energy blink level
func (gs *GameState) SetEnergyBlinkLevel(level uint32) {
	gs.EnergyBlinkLevel.Store(level)
}

// GetEnergyBlinkTime returns when the energy blink started
func (gs *GameState) GetEnergyBlinkTime() time.Time {
	nano := gs.EnergyBlinkTime.Load()
	if nano == 0 {
		return time.Time{}
	}
	return time.Unix(0, nano)
}

// SetEnergyBlinkTime sets when the energy blink started
func (gs *GameState) SetEnergyBlinkTime(t time.Time) {
	gs.EnergyBlinkTime.Store(t.UnixNano())
}

// ===== PHASE STATE ACCESSORS (mutex protected) =====

// GetPhase returns the current game phase
func (gs *GameState) GetPhase() GamePhase {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.CurrentPhase
}

// CanTransition checks if a phase transition is valid
func (gs *GameState) CanTransition(from, to GamePhase) bool {
	validTransitions := map[GamePhase][]GamePhase{
		PhaseNormal:         {PhaseGoldActive},
		PhaseGoldActive:     {PhaseGoldComplete},
		PhaseGoldComplete:   {PhaseDecayWait},
		PhaseDecayWait:      {PhaseDecayAnimation},
		PhaseDecayAnimation: {PhaseNormal},
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
func (gs *GameState) TransitionPhase(to GamePhase, now time.Time) bool {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	if !gs.CanTransition(gs.CurrentPhase, to) {
		return false
	}

	gs.CurrentPhase = to
	gs.PhaseStartTime = now
	return true
}

// GetPhaseStartTime returns when the current phase started
func (gs *GameState) GetPhaseStartTime() time.Time {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.PhaseStartTime
}

// GetPhaseDuration returns how long the current phase has been active
func (gs *GameState) GetPhaseDuration(now time.Time) time.Duration {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return now.Sub(gs.PhaseStartTime)
}

// PhaseSnapshot provides a consistent view of phase state
type PhaseSnapshot struct {
	Phase     GamePhase
	StartTime time.Time
	Duration  time.Duration
}

// ReadPhaseState returns a consistent snapshot of the current phase state
func (gs *GameState) ReadPhaseState(now time.Time) PhaseSnapshot {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

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
func (gs *GameState) ActivateGoldSequence(sequenceID int, duration time.Duration, now time.Time) bool {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Validate phase transition
	if !gs.CanTransition(gs.CurrentPhase, PhaseGoldActive) {
		return false
	}

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
func (gs *GameState) DeactivateGoldSequence(now time.Time) bool {
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
	gs.PhaseStartTime = now
	return true
}

// GetGoldTimeoutTime returns when the gold sequence will timeout
func (gs *GameState) GetGoldTimeoutTime() time.Time {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.GoldTimeoutTime
}

// IsGoldTimedOut checks if the gold sequence has timed out
func (gs *GameState) IsGoldTimedOut(now time.Time) bool {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	if !gs.GoldActive {
		return false
	}
	// Direct comparison - both timestamps are on the same timeline
	return now.After(gs.GoldTimeoutTime)
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
func (gs *GameState) ReadGoldState(now time.Time) GoldSnapshot {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

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
func (gs *GameState) StartDecayTimer(baseSeconds, rangeSeconds float64, now time.Time) bool {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Validate phase transition
	if !gs.CanTransition(gs.CurrentPhase, PhaseDecayWait) {
		return false
	}

	// Calculate decay timer based on heat percentage
	heat := int(gs.Heat.Load())
	heatPercentage := float64(heat) / float64(constants.MaxHeat)
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
func (gs *GameState) IsDecayReady(now time.Time) bool {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	if !gs.DecayTimerActive {
		return false
	}
	return now.After(gs.DecayNextTime) || now.Equal(gs.DecayNextTime)
}

// GetTimeUntilDecay returns seconds until next decay trigger
func (gs *GameState) GetTimeUntilDecay(now time.Time) float64 {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	if !gs.DecayTimerActive || gs.DecayAnimating {
		return 0.0
	}
	remaining := gs.DecayNextTime.Sub(now).Seconds()
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
func (gs *GameState) StartDecayAnimation(now time.Time) bool {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Validate phase transition
	if !gs.CanTransition(gs.CurrentPhase, PhaseDecayAnimation) {
		return false
	}

	gs.DecayAnimating = true
	gs.DecayStartTime = now
	gs.DecayTimerActive = false // Timer is no longer active once animation starts
	gs.CurrentPhase = PhaseDecayAnimation
	gs.PhaseStartTime = now
	return true
}

// StopDecayAnimation stops the decay animation and returns to Normal phase
// Only allowed from PhaseDecayAnimation (checked by phase transition validation)
func (gs *GameState) StopDecayAnimation(now time.Time) bool {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Validate phase transition
	if !gs.CanTransition(gs.CurrentPhase, PhaseNormal) {
		return false
	}

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
func (gs *GameState) ReadDecayState(now time.Time) DecaySnapshot {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	timeUntil := 0.0
	if gs.DecayTimerActive && !gs.DecayAnimating {
		remaining := gs.DecayNextTime.Sub(now).Seconds()
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

// BoostSnapshot provides consistent view of boost state
type BoostSnapshot struct {
	Enabled   bool
	EndTime   time.Time
	Color     int32 // 0=None, 1=Blue, 2=Green
	Remaining time.Duration
}

// // CursorSnapshot provides consistent view of cursor position
// type CursorSnapshot struct {
// 	X int
// 	Y int
// }

// ===== GAME LIFECYCLE ACCESSORS (mutex protected) =====

// GetFirstUpdateTime returns when the game first started
func (gs *GameState) GetFirstUpdateTime() time.Time {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.FirstUpdateTime
}

// SetFirstUpdateTime sets when the game first started (should only be called once)
func (gs *GameState) SetFirstUpdateTime(t time.Time) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	// Only set if not already set
	if gs.FirstUpdateTime.IsZero() {
		gs.FirstUpdateTime = t
	}
}

// GetInitialSpawnComplete returns whether initial gold spawn has been attempted
func (gs *GameState) GetInitialSpawnComplete() bool {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.InitialSpawnComplete
}

// SetInitialSpawnComplete marks that initial gold spawn has been attempted
func (gs *GameState) SetInitialSpawnComplete() {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.InitialSpawnComplete = true
}