package engine

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine/fsm"
	"github.com/lixenwraith/vi-fighter/engine/status"
	"github.com/lixenwraith/vi-fighter/events"
)

// ClockScheduler manages game logic on a fixed tick
// Provides infrastructure for phase transitions and state ownership
// Handles pause-aware scheduling without busy-wait
type ClockScheduler struct {
	world    *World
	timeRes  *TimeResource
	stateRes *GameStateResource
	eqRes    *EventQueueResource

	pausableClock *PausableClock
	isPaused      *atomic.Bool

	// Tick configuration
	tickInterval     time.Duration
	lastGameTickTime time.Time // Last tick in game time
	nextTickDeadline time.Time // Next tick deadline for drift correction

	// Tick counter for debugging and metrics
	tickCount atomic.Uint64
	mu        sync.RWMutex

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
	eventRouter *events.Router

	// Finite State Machine
	fsm *fsm.Machine[*World]

	// Cached metric pointers
	statusReg *status.Registry
	statTicks *atomic.Int64
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

	timeRes := MustGetResource[*TimeResource](world.Resources)
	stateRes := MustGetResource[*GameStateResource](world.Resources)
	statusReg := MustGetResource[*status.Registry](world.Resources)
	eqRes := MustGetResource[*EventQueueResource](world.Resources)

	cs := &ClockScheduler{
		world:            world,
		pausableClock:    pausableClock,
		isPaused:         isPaused,
		tickInterval:     tickInterval,
		timeRes:          timeRes,
		stateRes:         stateRes,
		eqRes:            eqRes,
		lastGameTickTime: pausableClock.Now(),
		tickCount:        atomic.Uint64{},
		eventRouter:      events.NewRouter(MustGetResource[*EventQueueResource](world.Resources).Queue),
		frameReady:       frameReady,
		updateDone:       updateDone,
		resetChan:        resetChan,
		stopChan:         make(chan struct{}),
		statusReg:        statusReg,
		statTicks:        statusReg.Ints.Get("engine.ticks"),
		fsm:              fsm.NewMachine[*World](),
	}

	return cs, updateDone, resetChan
}

// RegisterEventHandler adds an event handler to router, must be called before Start()
func (cs *ClockScheduler) RegisterEventHandler(handler events.Handler) {
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

	// Initialize State (enters initial state)
	if err := cs.fsm.Init(cs.world, cs.fsm.InitialStateID); err != nil {
		return fmt.Errorf("failed to init FSM: %w", err)
	}

	return nil
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
func (cs *ClockScheduler) schedulerLoop() {
	defer cs.wg.Done()

	cs.mu.Lock()
	cs.nextTickDeadline = cs.pausableClock.Now().Add(cs.tickInterval)
	cs.lastGameTickTime = cs.pausableClock.Now()
	cs.mu.Unlock()

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

			cs.mu.RLock()
			deadline := cs.nextTickDeadline
			cs.mu.RUnlock()

			if !gameNow.Before(deadline) {
				select {
				case <-cs.frameReady:
				case <-time.After(cs.tickInterval * 2):
				case <-cs.stopChan:
					return
				}

				cs.processTick()

				cs.mu.Lock()
				cs.lastGameTickTime = gameNow
				cs.nextTickDeadline = cs.nextTickDeadline.Add(cs.tickInterval)

				maxBehind := cs.tickInterval * 2
				if gameNow.Sub(cs.nextTickDeadline) > maxBehind {
					cs.nextTickDeadline = gameNow.Add(cs.tickInterval)
				}
				deadline = cs.nextTickDeadline
				cs.mu.Unlock()

				cs.tickCount.Add(1)

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

// executeReset performs FSM reset while scheduler mutex is held
func (cs *ClockScheduler) executeReset() {
	// NOTE: Do not use RunSafe if called from a blocking system
	// 1. Drain and discard stale events from the previous game session
	_ = cs.eqRes.Queue.Consume()

	// 2. Synchronize with world lock
	// Acquire lock explicitly, wait till MetaSystem finishes its synchronous cleanup and releases the lock
	cs.world.Lock()
	defer cs.world.Unlock()

	// 3. Reset Scheduler internal timing
	cs.mu.Lock()
	cs.tickCount.Store(0)
	cs.lastGameTickTime = cs.pausableClock.Now()
	cs.nextTickDeadline = cs.lastGameTickTime.Add(cs.tickInterval)
	cs.mu.Unlock()

	// 4. Reset FSM state - This will trigger OnEnter actions
	if err := cs.fsm.Reset(cs.world); err != nil {
		panic(fmt.Errorf("FSM reset failed: %v", err))
	}

	// 5. Process the events emitted by FSM Reset while holding World lock to ensure initial entities are spawned in world BEFORE unpause
	cs.dispatchAndProcessEvents()

	// 6. Transition to Running state
	// Scheduler is the unpause authority during reset, preventing systems from ticking against an uninitialized FSM
	cs.isPaused.Store(false)
	cs.pausableClock.Resume()
}

// dispatchAndProcessEvents processes pending events through Router AND FSM
func (cs *ClockScheduler) dispatchAndProcessEvents() {
	eventsList := cs.eqRes.Queue.Consume()
	for _, ev := range eventsList {
		cs.fsm.HandleEvent(cs.world, ev.Type)

		if handlers, ok := cs.eventRouter.GetHandlers(ev.Type); ok {
			for _, h := range handlers {
				h.HandleEvent(ev)
			}
		}
	}
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
		cs.timeRes.Update(
			now,
			cs.pausableClock.RealTime(),
			cs.tickInterval,
			MustGetResource[*GameStateResource](cs.world.Resources).State.GetFrameNumber(),
		)

		// Process Events (Input -> FSM -> Systems)
		cs.dispatchAndProcessEvents()

		// Update FSM Logic (Tick transitions)
		cs.fsm.Update(cs.world, cs.tickInterval)

		// Run Systems
		cs.world.UpdateLocked()
	})

	ticks := cs.stateRes.State.IncrementGameTicks()
	cs.statTicks.Store(int64(ticks))

	if cs.tickCount.Load()%20 == 0 {
		cs.stateRes.State.UpdateAPM(cs.statusReg)
	}
}