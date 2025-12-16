package engine

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine/status"
	"github.com/lixenwraith/vi-fighter/events"
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
	default:
		return "Unknown"
	}
}

// ClockScheduler manages game logic on a fixed 50ms tick
// Provides infrastructure for phase transitions and state ownership
// Handles pause-aware scheduling without busy-wait
type ClockScheduler struct {
	ctx          *GameContext
	timeProvider *PausableClock
	// TimeResource singleton for in-place updates
	timeRes *TimeResource

	// Tick configuration
	tickInterval     time.Duration
	lastGameTickTime time.Time // Last tick in game time
	nextTickDeadline time.Time // Next tick deadline for drift correction

	// Tick counter for debugging and metrics
	tickCount atomic.Uint64
	mu        sync.RWMutex

	// Control channels
	stopChan chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup // Ensures goroutine exits before Stop() returns
	running  atomic.Bool

	// Frame synchronization channels
	frameReady <-chan struct{} // Receive signal that frame is ready
	updateDone chan<- struct{} // Send signal that update is complete

	// Event routing
	eventRouter *events.Router[*World]

	// Cached metric pointers
	statTicks *atomic.Int64
}

// NewClockScheduler creates a new clock scheduler with specified tick interval
func NewClockScheduler(ctx *GameContext, tickInterval time.Duration, frameReady <-chan struct{}) (*ClockScheduler, <-chan struct{}) {
	updateDone := make(chan struct{}, 1)

	// Look up the singleton TimeResource once at startup
	timeRes := MustGetResource[*TimeResource](ctx.World.Resources)
	statusReg := MustGetResource[*status.Registry](ctx.World.Resources)

	cs := &ClockScheduler{
		ctx:              ctx,
		timeProvider:     ctx.PausableClock,
		tickInterval:     tickInterval,
		timeRes:          timeRes,
		lastGameTickTime: ctx.PausableClock.Now(),
		tickCount:        atomic.Uint64{},
		eventRouter:      events.NewRouter[*World](ctx.eventQueue),
		frameReady:       frameReady,
		updateDone:       updateDone,
		stopChan:         make(chan struct{}),
		statTicks:        statusReg.Ints.Get("engine.ticks"), // TODO: review
	}

	return cs, updateDone
}

// RegisterEventHandler adds an event handler to the router, must be called before Start()
func (cs *ClockScheduler) RegisterEventHandler(handler events.Handler[*World]) {
	cs.eventRouter.Register(handler)
}

// EventTypes returns event types ClockScheduler handles
func (cs *ClockScheduler) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventGoldSpawned,
		events.EventGoldComplete,
		events.EventGoldTimeout,
		events.EventGoldDestroyed,
		events.EventDecayStart,
		events.EventDecayComplete,
	}
}

// HandleEvent processes phase transition events
func (cs *ClockScheduler) HandleEvent(world *World, event events.GameEvent) {
	now := cs.timeRes.GameTime
	switch event.Type {
	case events.EventGoldSpawned:
		cs.ctx.State.SetPhase(PhaseGoldActive, now)

	case events.EventGoldComplete, events.EventGoldTimeout, events.EventGoldDestroyed:
		cs.ctx.State.SetPhase(PhaseDecayWait, now)
		cs.emitPhaseChange(PhaseDecayWait)

	case events.EventDecayStart:
		cs.ctx.State.SetPhase(PhaseDecayAnimation, now)

	case events.EventDecayComplete:
		cs.ctx.State.SetPhase(PhaseNormal, now)
		cs.emitPhaseChange(PhaseNormal)
	}
}

func (cs *ClockScheduler) emitPhaseChange(phase GamePhase) {
	event := events.GameEvent{
		Type:    events.EventPhaseChange,
		Payload: &events.PhaseChangePayload{NewPhase: int(phase)},
		Frame:   cs.ctx.State.GetFrameNumber(),
	}
	cs.ctx.eventQueue.Push(event)
}

// Start begins the scheduler loop
func (cs *ClockScheduler) Start() {
	if cs.running.CompareAndSwap(false, true) {
		cs.wg.Add(1)
		// Use core.Go for safe execution with centralized crash handling
		core.Go(cs.schedulerLoop)
	}
}

// Stop halts the scheduler loop
func (cs *ClockScheduler) Stop() {
	cs.stopOnce.Do(func() {
		if cs.running.CompareAndSwap(true, false) {
			close(cs.stopChan)
			cs.wg.Wait()
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

	// Initialize the timer outside the loop to prevent creating garbage on every tick
	// Starts with 0 and immediately stopped so it is ready for Reset() inside the loop
	timer := time.NewTimer(0)
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	defer timer.Stop()

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
			// GC Optimization: Reset() existing timer instead of allocating a new one with NewTimer()
			timer.Reset(sleepDuration)
			select {
			case <-timer.C:
				// Continue loop
			case <-cs.stopChan:
				// No need to Stop() the timer, handled by defer time.Stop()
				return
			}
		}
	}
}

// DispatchEventsImmediately processes all pending events synchronously
// Call from main loop after input handling to eliminate input latency
func (cs *ClockScheduler) DispatchEventsImmediately() {
	cs.ctx.World.RunSafe(func() {
		cs.eventRouter.DispatchAll(cs.ctx.World)
	})
}

// processTick executes one clock cycle (called every game tick when not paused)
func (cs *ClockScheduler) processTick() {
	// Skip tick execution when paused (defensive check)
	if cs.ctx.IsPaused.Load() {
		return
	}

	// Dispatch + Update under single lock to serialize with DispatchEventsImmediately
	cs.ctx.World.RunSafe(func() {
		now := cs.timeProvider.Now()
		cs.timeRes.Update(
			now,
			cs.timeProvider.RealTime(),
			cs.tickInterval,
			cs.ctx.State.GetFrameNumber(),
		)

		cs.eventRouter.DispatchAll(cs.ctx.World)
		cs.ctx.World.UpdateLocked(cs.tickInterval)
	})

	cs.ctx.State.IncrementGameTicks()

	if cs.tickCount.Load()%20 == 0 {
		statusReg, _ := GetResource[*status.Registry](cs.ctx.World.Resources)
		cs.ctx.State.UpdateAPM(statusReg)
	}
}