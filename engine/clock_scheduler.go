package engine

import (
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/constants"
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
// Phase 3: Gold/Decay transition logic
type ClockScheduler struct {
	ctx          *GameContext
	timeProvider TimeProvider

	// 50ms ticker for clock-based game logic
	ticker *time.Ticker

	// Control channels
	stopChan chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup // Ensures goroutine exits before Stop() returns

	// Tick counter for debugging and metrics
	tickCount uint64
	mu        sync.RWMutex

	// System references (Phase 3: needed for triggering transitions)
	// These will be set via SetSystems() after scheduler creation
	goldSystem  GoldSequenceSystemInterface
	decaySystem DecaySystemInterface
}

// GoldSequenceSystemInterface defines the methods needed by the clock scheduler
type GoldSequenceSystemInterface interface {
	TimeoutGoldSequence(world *World)
}

// DecaySystemInterface defines the methods needed by the clock scheduler
type DecaySystemInterface interface {
	TriggerDecayAnimation(world *World)
}

// NewClockScheduler creates a new clock scheduler with 50ms tick rate
func NewClockScheduler(ctx *GameContext) *ClockScheduler {
	return &ClockScheduler{
		ctx:          ctx,
		timeProvider: ctx.TimeProvider,
		ticker:       time.NewTicker(50 * time.Millisecond),
		stopChan:     make(chan struct{}),
		tickCount:    0,
		goldSystem:   nil,
		decaySystem:  nil,
	}
}

// SetSystems sets the system references needed for phase transitions
// Must be called before Start() to enable phase transition logic
func (cs *ClockScheduler) SetSystems(goldSystem GoldSequenceSystemInterface, decaySystem DecaySystemInterface) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.goldSystem = goldSystem
	cs.decaySystem = decaySystem
}

// Start begins the clock scheduler in a separate goroutine
// Returns immediately; scheduler runs until Stop() is called
func (cs *ClockScheduler) Start() {
	cs.wg.Add(1)
	go cs.run()
}

// run is the main clock loop (runs in goroutine)
func (cs *ClockScheduler) run() {
	defer cs.wg.Done()
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
// Phase 3: Implements phase transition logic for Gold→Decay→Normal cycle
func (cs *ClockScheduler) tick() {
	cs.mu.Lock()
	cs.tickCount++
	goldSys := cs.goldSystem
	decaySys := cs.decaySystem
	cs.mu.Unlock()

	// Get current phase from GameState
	phase := cs.ctx.State.GetPhase()

	// Handle phase transitions based on current phase
	switch phase {
	case PhaseGoldActive:
		// Check if gold sequence has timed out
		if cs.ctx.State.IsGoldTimedOut() {
			// Gold timeout - transition to DecayWait
			// Call gold system to remove gold entities
			if goldSys != nil {
				goldSys.TimeoutGoldSequence(cs.ctx.World)
			}

			// Start decay timer (reads heat atomically, no cached value)
			cs.ctx.State.StartDecayTimer(
				cs.ctx.State.ScreenWidth,
				constants.HeatBarIndicatorWidth,
				constants.DecayIntervalBaseSeconds,
				constants.DecayIntervalRangeSeconds,
			)
		}

	case PhaseDecayWait:
		// Check if decay timer has expired
		if cs.ctx.State.IsDecayReady() {
			// Timer expired - transition to DecayAnimation
			// Start decay animation
			cs.ctx.State.StartDecayAnimation()

			// Trigger decay system to spawn falling entities
			if decaySys != nil {
				decaySys.TriggerDecayAnimation(cs.ctx.World)
			}
		}

	case PhaseDecayAnimation:
		// Animation is handled by DecaySystem
		// When animation completes, DecaySystem will call StopDecayAnimation()
		// which transitions back to PhaseNormal
		// Nothing to do in clock tick for this phase

	case PhaseNormal:
		// Normal gameplay - no phase transitions
		// Gold spawning is handled by GoldSequenceSystem's Update() method
		// Nothing to do in clock tick for this phase
	}
}

// Stop halts the clock scheduler gracefully
// Waits for the goroutine to fully exit before returning
func (cs *ClockScheduler) Stop() {
	cs.stopOnce.Do(func() {
		cs.ticker.Stop()
		close(cs.stopChan)
		cs.wg.Wait() // Wait for goroutine to exit
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
