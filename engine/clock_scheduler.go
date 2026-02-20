package engine

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine/fsm"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/status"
)

// ClockScheduler manages game logic on a fixed tick
// Provides infrastructure for phase transitions and state ownership
// Handles pause-aware scheduling without busy-wait
type ClockScheduler struct {
	world *World

	pausableClock *PausableClock
	isPaused      *atomic.Bool

	// Tick configuration
	tickInterval     time.Duration
	lastGameTickTime time.Time // Last tick in game time
	gameStartTime    time.Time // Game session start for elapsed calculation
	nextTickDeadline time.Time // Next tick deadline for drift correction

	// Control channels
	stopChan  chan struct{}
	stopOnce  sync.Once
	wg        sync.WaitGroup
	running   atomic.Bool
	resetChan <-chan struct{}

	// Frame synchronization channels
	frameReady <-chan struct{} // Receive signal that frame is ready
	updateDone chan<- struct{} // Send signal that update is complete

	// Event routing
	eventRouter *event.Router

	// Finite GameState Machine
	fsm *fsm.Machine[*World]

	// Event loop configuration
	eventLoopInterval   time.Duration
	eventLoopBackoffMax int

	// Cached metric pointers
	statusReg         *status.Registry
	statTicks         *atomic.Int64
	statEvBackoffs    *atomic.Int64
	statEvDispatches  *atomic.Int64
	statEntityCount   *atomic.Int64
	statQueueLen      *atomic.Int64
	statGameElapsedMs *atomic.Int64

	// FSM telemetry
	statFSMName    *status.AtomicString
	statFSMElapsed *atomic.Int64
	statFSMMaxDur  *atomic.Int64
	statFSMIndex   *atomic.Int64
	statFSMTotal   *atomic.Int64
}

// NewClockScheduler creates a new clock scheduler with specified tick interval
// Receives frameReady sync (receive) channel and returns game updateDone (send) and resetRequest (send) channels
func NewClockScheduler(
	world *World,
	pausableClock *PausableClock,
	isPaused *atomic.Bool,
	tickInterval time.Duration,
	frameReady <-chan struct{},
) (*ClockScheduler, <-chan struct{}, chan<- struct{}) {
	updateDone := make(chan struct{}, 1)
	resetChan := make(chan struct{}, 1)

	statusReg := world.Resources.Status

	cs := &ClockScheduler{
		world: world,

		pausableClock: pausableClock,
		isPaused:      isPaused,
		tickInterval:  tickInterval,

		lastGameTickTime: pausableClock.Now(),
		gameStartTime:    pausableClock.Now(),

		eventRouter: event.NewRouter(world.Resources.Event.Queue),

		frameReady: frameReady,
		updateDone: updateDone,
		resetChan:  resetChan,
		stopChan:   make(chan struct{}),

		fsm: fsm.NewMachine[*World](),

		eventLoopInterval:   parameter.EventLoopInterval,
		eventLoopBackoffMax: parameter.EventLoopBackoffMax,

		statTicks:         statusReg.Ints.Get("engine.ticks"),
		statEvBackoffs:    statusReg.Ints.Get("event.backoffs"),
		statEvDispatches:  statusReg.Ints.Get("event.dispatches"),
		statEntityCount:   statusReg.Ints.Get("entity.count"),
		statQueueLen:      statusReg.Ints.Get("event.queue_len"),
		statGameElapsedMs: statusReg.Ints.Get("time.game_elapsed_ms"),

		statFSMName:    statusReg.Strings.Get("fsm.state"),
		statFSMElapsed: statusReg.Ints.Get("fsm.elapsed"),
		statFSMMaxDur:  statusReg.Ints.Get("fsm.max_duration"),
		statFSMIndex:   statusReg.Ints.Get("fsm.state_index"),
		statFSMTotal:   statusReg.Ints.Get("fsm.state_count"),
	}

	return cs, updateDone, resetChan
}

// RegisterEventHandler adds an event handler to router, must be called before Start()
func (cs *ClockScheduler) RegisterEventHandler(handler event.Handler) {
	cs.eventRouter.Register(handler)
}

// LoadFSM initializes HFSM with provided config and registry bridge, must be called before Start()
func (cs *ClockScheduler) LoadFSM(config string, registerComponents func(*fsm.Machine[*World])) error {
	// Register Actions/Guards
	registerComponents(cs.fsm)

	// Load Graph
	if err := cs.fsm.LoadConfig([]byte(config)); err != nil {
		return fmt.Errorf("failed to load FSM config: %w", err)
	}

	// Initialize GameState (enters initial state)
	if err := cs.fsm.Init(cs.world); err != nil {
		return fmt.Errorf("failed to init FSM: %w", err)
	}

	// Apply global system configuration
	cs.fsm.ExecuteAction(cs.world, "ApplyGlobalSystemConfig", nil)

	return nil
}

// LoadFSMAuto initializes HFSM with priority: customPath > external detection > embedded
// customPath is used if non-empty, otherwise falls back to auto-detection
func (cs *ClockScheduler) LoadFSMAuto(customPath, embeddedFallback string, registerComponents func(*fsm.Machine[*World])) error {
	// Register Actions/Guards
	registerComponents(cs.fsm)

	// Load with priority resolution
	if err := fsm.LoadConfigAuto(cs.fsm, customPath, embeddedFallback); err != nil {
		return fmt.Errorf("failed to load FSM: %w", err)
	}

	// Initialize GameState (enters initial state)
	if err := cs.fsm.Init(cs.world); err != nil {
		return fmt.Errorf("failed to init FSM: %w", err)
	}

	// Apply global system configuration
	cs.fsm.ExecuteAction(cs.world, "ApplyGlobalSystemConfig", nil)

	return nil
}

// Start begins the scheduler loop
func (cs *ClockScheduler) Start() {
	if cs.running.CompareAndSwap(false, true) {
		cs.wg.Add(2) // 2 Goroutines
		// Use core.Go for safe execution with centralized crash handling
		core.Go(cs.schedulerLoop)
		core.Go(cs.eventLoop)
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
func (cs *ClockScheduler) schedulerLoop() {
	defer cs.wg.Done()

	cs.nextTickDeadline = cs.pausableClock.Now().Add(cs.tickInterval)
	cs.lastGameTickTime = cs.pausableClock.Now()

	timer := time.NewTimer(0)
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	defer timer.Stop()

	for {
		select {
		case <-cs.stopChan:
			return

		case <-cs.resetChan:
			// Execute reset regardless of current pause state to prevent channel clogging
			cs.executeReset()
			continue

		default:
		}

		var sleepDuration time.Duration

		if cs.isPaused.Load() {
			// Increase sleep interval while paused to save CPU
			sleepDuration = cs.tickInterval * 2
		} else {
			gameNow := cs.pausableClock.Now()

			deadline := cs.nextTickDeadline

			if !gameNow.Before(deadline) {
				select {
				case <-cs.frameReady:
				case <-time.After(cs.tickInterval * 2):
				case <-cs.stopChan:
					return
				}

				cs.processTick()

				cs.lastGameTickTime = gameNow
				cs.nextTickDeadline = cs.nextTickDeadline.Add(cs.tickInterval)

				maxBehind := cs.tickInterval * 2
				if gameNow.Sub(cs.nextTickDeadline) > maxBehind {
					cs.nextTickDeadline = gameNow.Add(cs.tickInterval)
				}
				deadline = cs.nextTickDeadline

				select {
				case cs.updateDone <- struct{}{}:
				default:
				}

				sleepDuration = deadline.Sub(cs.pausableClock.Now())
				if sleepDuration < 0 {
					sleepDuration = 0
				}
			} else {
				sleepDuration = deadline.Sub(gameNow)
			}
		}

		if sleepDuration > 0 {
			timer.Reset(sleepDuration)
			select {
			case <-timer.C:
			case <-cs.resetChan:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				if cs.isPaused.Load() {
					cs.executeReset()
				}
			case <-cs.stopChan:
				return
			}
		}
	}
}

// eventLoop runs at 1ms frequency for reactive event settling
func (cs *ClockScheduler) eventLoop() {
	defer cs.wg.Done()

	ticker := time.NewTicker(cs.eventLoopInterval)
	defer ticker.Stop()

	backoffCount := 0

	for {
		select {
		case <-cs.stopChan:
			return

		case <-ticker.C:
			if cs.isPaused.Load() {
				continue
			}

			// Skip if queue empty (prevents busy-wait contention)
			if cs.world.Resources.Event.Queue.Len() == 0 {
				backoffCount = 0
				continue
			}

			// Attempt non-blocking lock
			if cs.world.TryLock() {
				cs.dispatchOnePass()
				cs.world.Unlock()
				backoffCount = 0
				continue
			}

			// Backoff tracking
			backoffCount++
			cs.statEvBackoffs.Add(1)

			// Force progress after threshold
			if backoffCount >= cs.eventLoopBackoffMax {
				// Check shutdown before blocking lock to prevent Stop() hang
				if !cs.running.Load() {
					return
				}
				cs.world.Lock()
				cs.dispatchOnePass()
				cs.world.Unlock()
				backoffCount = 0
			}
		}
	}
}

// dispatchOnePass consumes and dispatches pending events exactly once
// Returns number of events processed
func (cs *ClockScheduler) dispatchOnePass() int {
	eventsList := cs.world.Resources.Event.Queue.Consume()
	if len(eventsList) == 0 {
		return 0
	}

	for _, ev := range eventsList {
		cs.fsm.HandleEvent(cs.world, ev)

		if handlers, ok := cs.eventRouter.GetHandlers(ev.Type); ok {
			for _, h := range handlers {
				h.HandleEvent(ev)
			}
		}
	}

	cs.statEvDispatches.Add(int64(len(eventsList)))
	return len(eventsList)
}

// dispatchAndProcessEvents settles pending events with iteration cap
// Used by reset path where immediate settling is required
func (cs *ClockScheduler) dispatchAndProcessEvents() {
	for i := 0; i < parameter.EventLoopIterations; i++ {
		if cs.dispatchOnePass() == 0 {
			return
		}
	}
}

// executeReset performs FSM reset while scheduler mutex is held
func (cs *ClockScheduler) executeReset() {
	// NOTE: Do not use RunSafe if called from a blocking systems
	// 1. Drain and discard stale events from the previous game session
	_ = cs.world.Resources.Event.Queue.Consume()

	// 2. Synchronize with world lock
	// Acquire lock explicitly, wait till MetaSystem finishes its synchronous cleanup and releases the lock
	cs.world.Lock()
	defer cs.world.Unlock()

	// 3. Reset Scheduler internal timing
	cs.lastGameTickTime = cs.pausableClock.Now()
	cs.nextTickDeadline = cs.lastGameTickTime.Add(cs.tickInterval)
	cs.gameStartTime = cs.lastGameTickTime

	// 4. Reset FSM state - This will trigger OnEnter actions
	if err := cs.fsm.Reset(cs.world); err != nil {
		panic(fmt.Errorf("FSM reset failed: %v", err))
	}

	// 5. Re-apply global system configuration (mirrors LoadFSM behavior)
	cs.fsm.ExecuteAction(cs.world, "ApplyGlobalSystemConfig", nil)

	// 6. Process the events emitted by FSM Reset while holding World lock to ensure initial entities are spawned in world BEFORE unpause
	cs.dispatchAndProcessEvents()

	// 6. Transition to Running state
	// Scheduler is the unpause authority during reset, preventing systems from ticking against an uninitialized FSM
	cs.isPaused.Store(false)
	cs.pausableClock.Resume()
}

// DispatchEventsImmediately processes all pending events synchronously
func (cs *ClockScheduler) DispatchEventsImmediately() {
	cs.world.RunSafe(func() {
		cs.dispatchAndProcessEvents()
	})
}

// processTick executes one clock cycle
func (cs *ClockScheduler) processTick() {
	if cs.isPaused.Load() {
		return
	}

	cs.world.RunSafe(func() {
		now := cs.pausableClock.Now()

		// 1. Sync Time
		cs.world.Resources.Time.Update(
			now,
			cs.pausableClock.RealTime(),
			cs.tickInterval,
		)

		// 2. Update game elapsed time status
		elapsedMs := now.Sub(cs.gameStartTime).Milliseconds()
		cs.statGameElapsedMs.Store(elapsedMs)

		// 3. Initial Settling: Resolve everything accumulated during game tick

		// Ensures FSM and Systems start with a consistent, settled world
		cs.dispatchAndProcessEvents()

		// 4. FSM Update: Advance state machine (may emit new events via Actions)
		cs.fsm.Update(cs.world, cs.tickInterval)

		// 5. FSM Telemetry (after update, before post-settling)
		stateName, stateID, timeInState := cs.fsm.GetActiveRegionTelemetry()
		cs.statFSMName.Store(stateName)
		cs.statFSMElapsed.Store(int64(timeInState))
		if maxDur, ok := cs.fsm.StateDurations[stateID]; ok {
			cs.statFSMMaxDur.Store(int64(maxDur))
		} else {
			cs.statFSMMaxDur.Store(0)
		}
		if idx, ok := cs.fsm.StateIndices[stateID]; ok {
			cs.statFSMIndex.Store(int64(idx))
		} else {
			cs.statFSMIndex.Store(0)
		}
		cs.statFSMTotal.Store(int64(cs.fsm.StateCount))

		// 6. Post-FSM Settling: Resolve events emitted by FSM state transitions
		cs.dispatchAndProcessEvents()

		// 7. System Execution: Systems run on the final, settled state for this tick
		cs.world.UpdateLocked()
	})

	// Increment ticks
	ticks := cs.world.Resources.Game.State.IncrementGameTicks()

	// Update APM based on game time
	cs.world.Resources.Game.State.UpdateAPM(
		cs.world.Resources.Status,
		cs.pausableClock.Now(),
	)

	cs.statTicks.Store(int64(ticks))
	cs.statEntityCount.Store(int64(cs.world.Positions.CountEntities()))
	cs.statQueueLen.Store(int64(cs.world.Resources.Event.Queue.Len()))
}