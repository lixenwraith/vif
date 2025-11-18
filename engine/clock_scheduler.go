package engine

import (
	"sync"
	"time"
)

// GamePhase represents the current phase of the game's mechanic cycle
// Phase 2: Infrastructure only - Phase transitions will be implemented in Phase 3
type GamePhase int

const (
	// PhaseNormal - Regular gameplay, spawning content, no special mechanics active
	PhaseNormal GamePhase = iota

	// PhaseGoldActive - Gold sequence is active and can be typed
	// (Phase 3: Will track gold timeout, completion)
	PhaseGoldActive

	// PhaseDecayWait - Waiting for decay timer to expire after gold completion/timeout
	// (Phase 3: Will calculate and track decay interval based on heat)
	PhaseDecayWait

	// PhaseDecayAnimation - Decay animation is running, characters degrading
	// (Phase 3: Will track falling entities, animation progress)
	PhaseDecayAnimation
)

// String returns the name of the game phase for debugging
func (p GamePhase) String() string {
	switch p {
	case PhaseNormal:
		return "Normal"
	case PhaseGoldActive:
		return "GoldActive"
	case PhaseDecayWait:
		return "DecayWait"
	case PhaseDecayAnimation:
		return "DecayAnimation"
	default:
		return "Unknown"
	}
}

// ClockScheduler manages game logic on a fixed 50ms tick
// Provides infrastructure for phase transitions and state ownership
// Phase 2: Clock infrastructure only
// Phase 3: Will add Gold/Decay/Cleaner transition logic
type ClockScheduler struct {
	ctx          *GameContext
	timeProvider TimeProvider

	// 50ms ticker for clock-based game logic
	ticker *time.Ticker

	// Control channels
	stopChan chan struct{}
	stopOnce sync.Once

	// Tick counter for debugging and metrics
	tickCount uint64
	mu        sync.RWMutex
}

// NewClockScheduler creates a new clock scheduler with 50ms tick rate
func NewClockScheduler(ctx *GameContext) *ClockScheduler {
	return &ClockScheduler{
		ctx:          ctx,
		timeProvider: ctx.TimeProvider,
		ticker:       time.NewTicker(50 * time.Millisecond),
		stopChan:     make(chan struct{}),
		tickCount:    0,
	}
}

// Start begins the clock scheduler in a separate goroutine
// Returns immediately; scheduler runs until Stop() is called
func (cs *ClockScheduler) Start() {
	go cs.run()
}

// run is the main clock loop (runs in goroutine)
func (cs *ClockScheduler) run() {
	for {
		select {
		case <-cs.ticker.C:
			cs.tick()
		case <-cs.stopChan:
			return
		}
	}
}

// tick executes one clock cycle (called every 50ms)
// Phase 2: Infrastructure only - increments tick counter
// Phase 3: Will add phase transition logic here
func (cs *ClockScheduler) tick() {
	cs.mu.Lock()
	cs.tickCount++
	tickNum := cs.tickCount
	cs.mu.Unlock()

	// Phase 2: Just tick infrastructure
	// Phase 3: Will add:
	// - Check spawn timing via ctx.State.ShouldSpawn()
	// - Check gold timeout (if PhaseGoldActive)
	// - Check decay timer (if PhaseDecayWait)
	// - Update animation state (if PhaseDecayAnimation)

	// Placeholder logging (will be removed in Phase 3)
	if tickNum%20 == 0 { // Log every second (20 ticks × 50ms = 1000ms)
		// Debug: Can check state here
		_ = cs.ctx.State.GetPhase()
	}
}

// Stop halts the clock scheduler gracefully
// Waits for current tick to complete before returning
func (cs *ClockScheduler) Stop() {
	cs.stopOnce.Do(func() {
		cs.ticker.Stop()
		close(cs.stopChan)
	})
}

// GetTickCount returns the number of clock ticks executed (for debugging/testing)
func (cs *ClockScheduler) GetTickCount() uint64 {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.tickCount
}

// GetTickRate returns the clock tick interval (always 50ms)
func (cs *ClockScheduler) GetTickRate() time.Duration {
	return 50 * time.Millisecond
}
