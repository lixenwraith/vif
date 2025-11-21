package engine

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/constants"
)

// GamePhase represents the current phase of the game's mechanic cycle
// ClockScheduler manages game phase transitions on a 50ms clock tick
type GamePhase int

const (
	// PhaseNormal - Regular gameplay, spawning content, no special mechanics active
	PhaseNormal GamePhase = iota

	// PhaseGoldActive - Gold sequence is active and can be typed with timeout tracking
	PhaseGoldActive

	// PhaseGoldComplete - Gold sequence was completed, ready to transition to decay or cleaner
	PhaseGoldComplete

	// PhaseDecayWait - Waiting for decay timer to expire after gold completion/timeout (heat-based interval)
	PhaseDecayWait

	// PhaseDecayAnimation - Decay animation is running with falling entities
	PhaseDecayAnimation

	// PhaseCleanerPending - Cleaners have been requested and will activate on next clock tick
	PhaseCleanerPending

	// PhaseCleanerActive - Cleaners are currently running
	PhaseCleanerActive
)

// String returns the name of the game phase for debugging
func (p GamePhase) String() string {
	switch p {
	case PhaseNormal:
		return "Normal"
	case PhaseGoldActive:
		return "GoldActive"
	case PhaseGoldComplete:
		return "GoldComplete"
	case PhaseDecayWait:
		return "DecayWait"
	case PhaseDecayAnimation:
		return "DecayAnimation"
	case PhaseCleanerPending:
		return "CleanerPending"
	case PhaseCleanerActive:
		return "CleanerActive"
	default:
		return "Unknown"
	}
}

// ClockScheduler manages game logic on a fixed 50ms tick
// Provides infrastructure for phase transitions and state ownership
// Handles pause-aware scheduling without busy-wait
type ClockScheduler struct {
	ctx          *GameContext
	timeProvider TimeProvider

	// Tick configuration
	tickInterval     time.Duration
	lastGameTickTime time.Time // Last tick in game time

	// Control channels
	stopChan chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup // Ensures goroutine exits before Stop() returns
	running  atomic.Bool

	// Tick counter for debugging and metrics
	tickCount atomic.Uint64
	mu        sync.RWMutex

	// System references needed for triggering transitions
	// These will be set via SetSystems() after scheduler creation
	goldSystem    GoldSystemInterface
	decaySystem   DecaySystemInterface
	cleanerSystem CleanerSystemInterface
}

// GoldSystemInterface defines the interface for gold sequence system
type GoldSystemInterface interface {
	TimeoutGoldSequence(world *World)
}

// DecaySystemInterface defines the interface for decay system
type DecaySystemInterface interface {
	TriggerDecayAnimation(world *World)
}

// CleanerSystemInterface defines the interface for cleaner system
type CleanerSystemInterface interface {
	ActivateCleaners(world *World)
	IsAnimationComplete() bool
}

// NewClockScheduler creates a new clock scheduler with specified tick interval
// Standard tick interval is 50ms for game logic updates
func NewClockScheduler(ctx *GameContext, tickInterval time.Duration) *ClockScheduler {
	return &ClockScheduler{
		ctx:              ctx,
		timeProvider:     ctx.TimeProvider,
		tickInterval:     tickInterval,
		lastGameTickTime: ctx.TimeProvider.Now(),
		tickCount:        atomic.Uint64{},
		stopChan:         make(chan struct{}),
	}
}

// SetSystems sets the system references needed for phase transitions
// Must be called before Start() to enable phase transition logic
func (cs *ClockScheduler) SetSystems(goldSystem GoldSystemInterface, decaySystem DecaySystemInterface, cleanerSystem CleanerSystemInterface) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.goldSystem = goldSystem
	cs.decaySystem = decaySystem
	cs.cleanerSystem = cleanerSystem
}

// SetGoldSequenceSystem sets the gold sequence system for timeout handling
func (cs *ClockScheduler) SetGoldSequenceSystem(system GoldSystemInterface) {
	cs.goldSystem = system
}

// SetDecaySystem sets the decay system for animation triggering
func (cs *ClockScheduler) SetDecaySystem(system DecaySystemInterface) {
	cs.decaySystem = system
}

// SetCleanerSystem sets the cleaner system for activation
func (cs *ClockScheduler) SetCleanerSystem(system CleanerSystemInterface) {
	cs.cleanerSystem = system
}

// Start begins the scheduler loop
func (cs *ClockScheduler) Start() {
	if cs.running.CompareAndSwap(false, true) {
		cs.wg.Add(1)
		go cs.schedulerLoop()
	}
}

// Stop halts the scheduler loop and waits for goroutine to exit
func (cs *ClockScheduler) Stop() {
	cs.stopOnce.Do(func() {
		if cs.running.CompareAndSwap(true, false) {
			close(cs.stopChan)
			cs.wg.Wait() // Wait for goroutine to fully exit
		}
	})
}

// schedulerLoop runs the main scheduling loop with pause awareness
// Implements adaptive sleeping that respects pause state to avoid busy-waiting
func (cs *ClockScheduler) schedulerLoop() {
	defer cs.wg.Done()

	// Adaptive ticker that respects pause state
	for {
		select {
		case <-cs.stopChan:
			return
		default:
		}

		// Calculate next tick time
		var sleepDuration time.Duration

		if cs.ctx.IsPaused.Load() {
			// During pause, sleep longer to avoid busy-wait
			sleepDuration = 100 * time.Millisecond
		} else {
			// Calculate how long until next game tick
			cs.mu.RLock()
			lastTick := cs.lastGameTickTime
			cs.mu.RUnlock()

			gameNow := cs.timeProvider.Now()
			elapsed := gameNow.Sub(lastTick)

			if elapsed >= cs.tickInterval {
				// Tick is due, process immediately
				cs.processTick()

				// Update last tick time
				cs.mu.Lock()
				cs.lastGameTickTime = gameNow
				cs.mu.Unlock()

				// Increment tick counter for debugging/tests
				cs.tickCount.Add(1)

				sleepDuration = cs.tickInterval
			} else {
				// Sleep until next tick
				sleepDuration = cs.tickInterval - elapsed
				// Cap sleep duration to avoid oversleeping
				if sleepDuration > cs.tickInterval {
					sleepDuration = cs.tickInterval
				}
			}
		}

		// Sleep with interruptible timer
		timer := time.NewTimer(sleepDuration)
		select {
		case <-timer.C:
			// Continue loop
		case <-cs.stopChan:
			timer.Stop()
			return
		}
	}
}

// processTick executes one clock cycle (called every 50ms when not paused)
// Implements phase transition logic for Gold→GoldComplete→Decay→Normal cycle
// Implements cleaner trigger logic (parallel to main phase cycle)
func (cs *ClockScheduler) processTick() {
	// Skip tick execution when paused (defensive check)
	if cs.ctx.IsPaused.Load() {
		return
	}

	// Get systems references with mutex protection
	cs.mu.RLock()
	goldSys := cs.goldSystem
	decaySys := cs.decaySystem
	cleanerSys := cs.cleanerSystem
	cs.mu.RUnlock()

	// Get world reference
	world := cs.ctx.World

	// Get current phase from GameState
	phaseSnapshot := cs.ctx.State.ReadPhaseState()

	// Handle phase transitions based on current phase
	switch phaseSnapshot.Phase {
	case PhaseGoldActive:
		// Check if gold sequence has timed out (pausable clock handles pause adjustment internally)
		if cs.ctx.State.IsGoldTimedOut() {
			// Gold timeout - call gold system to remove gold entities
			if goldSys != nil {
				goldSys.TimeoutGoldSequence(world)
			} else {
				// No gold system - just deactivate gold sequence directly
				cs.ctx.State.DeactivateGoldSequence()
			}
		}

	case PhaseGoldComplete:
		// Gold sequence completed or timed out
		// Check if cleaners should be triggered (handled by ScoreSystem via RequestCleaners)
		// If no cleaners pending, start decay timer
		if !cs.ctx.State.GetCleanerPending() {
			// Start decay timer (reads heat atomically, no cached value)
			// This will transition to PhaseDecayWait
			cs.ctx.State.StartDecayTimer(
				cs.ctx.State.ScreenWidth,
				constants.DecayIntervalBaseSeconds,
				constants.DecayIntervalRangeSeconds,
			)
		}
		// Note: If cleaners are pending, they will be handled in PhaseCleanerPending

	case PhaseDecayWait:
		// Check if decay timer has expired (pausable clock handles pause adjustment internally)
		if cs.ctx.State.IsDecayReady() {
			// Timer expired - transition to DecayAnimation
			if cs.ctx.State.StartDecayAnimation() {
				// Trigger decay system to spawn falling entities
				if decaySys != nil {
					decaySys.TriggerDecayAnimation(world)
				}
			}
		}

	case PhaseDecayAnimation:
		// Animation is handled by DecaySystem
		// When animation completes, DecaySystem will call StopDecayAnimation()
		// which transitions back to PhaseNormal
		// Nothing to do in clock tick for this phase

	case PhaseCleanerPending:
		// Activate cleaners
		// This will transition to PhaseCleanerActive
		if cs.ctx.State.ActivateCleaners() {
			// Trigger cleaner system to spawn cleaners
			if cleanerSys != nil {
				cleanerSys.ActivateCleaners(world)
			}
		}

	case PhaseCleanerActive:
		// Check if cleaner animation has completed
		if cleanerSys != nil && cleanerSys.IsAnimationComplete() {
			// Deactivate cleaners first
			cs.ctx.State.DeactivateCleaners()

			// Start decay timer after cleaners complete
			// This transitions to PhaseDecayWait
			cs.ctx.State.StartDecayTimer(
				cs.ctx.State.ScreenWidth,
				constants.DecayIntervalBaseSeconds,
				constants.DecayIntervalRangeSeconds,
			)
		}

	case PhaseNormal:
		// Normal gameplay - no phase transitions
		// Gold spawning is handled by GoldSequenceSystem's Update() method
		// Nothing to do in clock tick for this phase
	}

	// Update boost timer (check for expiration)
	// Pausable clock handles pause adjustment internally
	cs.ctx.State.UpdateBoostTimerAtomic()
}

// GetTickCount returns the current tick count for debugging/testing
func (cs *ClockScheduler) GetTickCount() uint64 {
	return cs.tickCount.Load()
}

// IsRunning returns true if the scheduler is running
func (cs *ClockScheduler) IsRunning() bool {
	return cs.running.Load()
}

// GetTickInterval returns the configured tick interval
func (cs *ClockScheduler) GetTickInterval() time.Duration {
	return cs.tickInterval
}

// GetTickRate returns the clock tick interval (always 50ms)
func (cs *ClockScheduler) GetTickRate() time.Duration {
	return 50 * time.Millisecond
}