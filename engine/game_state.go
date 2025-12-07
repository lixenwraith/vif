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

	// // Shield activation state (atomic for real-time access)
	// ShieldActive atomic.Bool

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
	// Updated during clock tick cycles, read by systems via snapshot methods

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

	// Gold Sequence State
	// Tracks active gold sequence timing and lifecycle
	GoldActive      bool      // Whether gold sequence is active
	GoldSequenceID  int       // Current gold sequence ID
	GoldStartTime   time.Time // When gold spawned
	GoldTimeoutTime time.Time // When gold will timeout

	// Stores the Entity ID of the currently active nugget, or 0 if none
	ActiveNuggetID atomic.Uint64

	// Decay Timer State
	// Manages countdown between gold completion and decay animation trigger
	DecayTimerActive bool      // Whether decay timer has been started
	DecayNextTime    time.Time // When decay will trigger

	// Decay Animation State
	// Tracks decay animation lifecycle
	DecayAnimating bool      // Whether decay animation is running
	DecayStartTime time.Time // When decay animation started

	// Game Lifecycle State
	GameStartTime time.Time // When game started
}

// initState initializes all game state fields to starting values
// Called by both NewGameState and Reset to avoid duplication
func (gs *GameState) initState(now time.Time) {
	// Reset atomics
	gs.Energy.Store(0)
	gs.Heat.Store(0)
	gs.BoostEnabled.Store(false)
	gs.BoostEndTime.Store(0)
	gs.BoostColor.Store(0)
	gs.CursorError.Store(false)
	gs.CursorErrorTime.Store(0)
	gs.EnergyBlinkActive.Store(false)
	gs.EnergyBlinkType.Store(0)
	gs.EnergyBlinkLevel.Store(0)
	gs.EnergyBlinkTime.Store(0)
	gs.PingActive.Store(false)
	gs.PingGridTimer.Store(0)
	gs.GrayoutActive.Store(false)
	gs.GrayoutStartTime.Store(0)
	gs.NextSeqID.Store(1)
	gs.FrameNumber.Store(0)
	gs.ActiveNuggetID.Store(0)

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

	// Gold state
	gs.GoldActive = false
	gs.GoldSequenceID = 0
	gs.GoldStartTime = time.Time{}
	gs.GoldTimeoutTime = time.Time{}

	// Decay state
	gs.DecayTimerActive = false
	gs.DecayNextTime = time.Time{}
	gs.DecayAnimating = false
	gs.DecayStartTime = time.Time{}

	// Phase state
	gs.GameStartTime = now
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

// ===== HEAT ACCESSORS (atomic) =====

// GetHeat returns the current heat value
func (gs *GameState) GetHeat() int {
	return int(gs.Heat.Load())
}

// SetHeat sets the heat value
func (gs *GameState) SetHeat(heat int) {
	if heat < 0 {
		heat = 0
	}
	if heat > constants.MaxHeat {
		heat = constants.MaxHeat
	}
	gs.Heat.Store(int64(heat))
}

// AddHeat adds a delta to the current heat value
func (gs *GameState) AddHeat(delta int) {
	for {
		current := gs.Heat.Load()
		newVal := current + int64(delta)
		if newVal < 0 {
			newVal = 0
		}
		if newVal > int64(constants.MaxHeat) {
			newVal = int64(constants.MaxHeat)
		}
		if gs.Heat.CompareAndSwap(current, newVal) {
			return
		}
	}
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

// // ===== SHIELD STATE ACCESSORS (atomic) =====
//
// // GetShieldActive returns whether shield is currently active
// func (gs *GameState) GetShieldActive() bool {
// 	return gs.ShieldActive.Load()
// }
//
// // SetShieldActive sets the shield active state
// func (gs *GameState) SetShieldActive(active bool) {
// 	gs.ShieldActive.Store(active)
// }

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

// ===== ACTIVE NUGGET ACCESSORS (atomic) =====

// GetActiveNuggetID returns the entity ID of the active nugget (0 if none)
func (gs *GameState) GetActiveNuggetID() uint64 {
	return gs.ActiveNuggetID.Load()
}

// SetActiveNuggetID sets the active nugget entity ID
func (gs *GameState) SetActiveNuggetID(id uint64) {
	gs.ActiveNuggetID.Store(id)
}

// ClearActiveNuggetID atomically clears the active nugget if it matches expected
// Returns true if cleared, false if already changed
func (gs *GameState) ClearActiveNuggetID(expected uint64) bool {
	return gs.ActiveNuggetID.CompareAndSwap(expected, 0)
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

// ===== GAME LIFECYCLE ACCESSORS (mutex protected) =====

// GetGameStartTime returns when the current game/round started
func (gs *GameState) GetGameStartTime() time.Time {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.GameStartTime
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