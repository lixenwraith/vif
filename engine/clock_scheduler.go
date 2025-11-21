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
	nextTickDeadline time.Time // Next tick deadline for drift correction

	// Control channels
	stopChan chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup // Ensures goroutine exits before Stop() returns
	running  atomic.Bool

	// Frame synchronization channels
	frameReady <-chan struct{} // Receive signal that frame is ready
	updateDone chan<- struct{} // Send signal that update is complete

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
func NewClockScheduler(ctx *GameContext, tickInterval time.Duration, frameReady <-chan struct{}) (*ClockScheduler, <-chan struct{}) {
	updateDone := make(chan struct{}, 1)

	cs := &ClockScheduler{
		ctx:              ctx,
		timeProvider:     ctx.TimeProvider,
		tickInterval:     tickInterval,
		lastGameTickTime: ctx.TimeProvider.Now(),
		frameReady:       frameReady,
		updateDone:       updateDone,
		tickCount:        atomic.Uint64{},
		stopChan:         make(chan struct{}),
	}

	// Register for pause resume notifications to adjust deadline
	ctx.PausableClock.OnResume(cs.HandlePauseResume)

	return cs, updateDone
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

	// Initialize next tick deadline
	cs.mu.Lock()
	cs.nextTickDeadline = cs.timeProvider.Now().Add(cs.tickInterval)
	cs.lastGameTickTime = cs.timeProvider.Now()
	cs.mu.Unlock()

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
			sleepDuration = cs.tickInterval * 2
		} else {
			gameNow := cs.timeProvider.Now()

			cs.mu.RLock()
			deadline := cs.nextTickDeadline
			cs.mu.RUnlock()

			// Check if we've reached or passed the deadline
			if !gameNow.Before(deadline) {
				// Wait for frame ready signal (with timeout to prevent deadlock)
				select {
				case <-cs.frameReady:
					// Frame is ready, proceed with tick
				case <-time.After(cs.tickInterval * 2):
					// Timeout - proceed anyway to prevent game freeze
					// This handles case where renderer is blocked
				case <-cs.stopChan:
					return
				}

				// Tick is due, process immediately
				cs.processTick()

				// Update timing with drift protection
				cs.mu.Lock()
				cs.lastGameTickTime = gameNow

				// Advance deadline by exactly one interval (prevents drift)
				cs.nextTickDeadline = cs.nextTickDeadline.Add(cs.tickInterval)

				// If we're severely behind (>2 intervals), catch up to avoid spiral
				maxBehind := cs.tickInterval * 2
				if gameNow.Sub(cs.nextTickDeadline) > maxBehind {
					// Reset to next interval from current time
					cs.nextTickDeadline = gameNow.Add(cs.tickInterval)
				}

				deadline = cs.nextTickDeadline

				cs.mu.Unlock()

				// Increment tick counter for debugging/tests
				cs.tickCount.Add(1)

				// Signal update complete (non-blocking)
				select {
				case cs.updateDone <- struct{}{}:
				default:
					// Channel full, renderer will catch up
				}

				// Sleep until next deadline
				sleepDuration = deadline.Sub(cs.timeProvider.Now())
				if sleepDuration < 0 {
					sleepDuration = 0 // Process next tick immediately if behind
				}
			} else {
				// Sleep until next tick
				sleepDuration = deadline.Sub(gameNow)
			}
		}

		// Sleep with interruptible timer
		if sleepDuration > 0 {
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
}

// processTick executes one clock cycle (called every 50ms when not paused)
// Implements phase transition logic for Gold→GoldComplete→Decay→Normal cycle
// Implements cleaner trigger logic (parallel to main phase cycle)
func (cs *ClockScheduler) processTick() {
	// Skip tick execution when paused (defensive check)
	if cs.ctx.IsPaused.Load() {
		return
	}

	// Update all ECS systems
	cs.ctx.World.Update(cs.tickInterval)

	// Update game-specific timers
	cs.ctx.State.UpdateBoostTimerAtomic()

	// Update ping grid timer
	if cs.ctx.UpdatePingGridTimerAtomic(cs.tickInterval.Seconds()) {
		cs.ctx.SetPingActive(false)
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

// HandlePauseResume adjusts the tick deadline when resuming from pause
// Called by PausableClock when resuming
func (cs *ClockScheduler) HandlePauseResume(pauseDuration time.Duration) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Adjust the deadline by the pause duration to maintain rhythm
	cs.nextTickDeadline = cs.nextTickDeadline.Add(pauseDuration)
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