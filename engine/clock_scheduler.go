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
	eventRouter *events.Router

	// Finite State Machine
	fsm *fsm.Machine[*World]

	// Cached metric pointers
	statusReg *status.Registry
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
		eventRouter:      events.NewRouter(ctx.eventQueue),
		frameReady:       frameReady,
		updateDone:       updateDone,
		stopChan:         make(chan struct{}),
		statusReg:        statusReg,
		statTicks:        statusReg.Ints.Get("engine.ticks"),
		fsm:              fsm.NewMachine[*World](),
	}

	return cs, updateDone
}

// RegisterEventHandler adds an event handler to the router, must be called before Start()
func (cs *ClockScheduler) RegisterEventHandler(handler events.Handler) {
	cs.eventRouter.Register(handler)
}

// LoadFSM initializes the HFSM with the provided JSON config and registry bridge
// MUST be called before Start()
func (cs *ClockScheduler) LoadFSM(configJSON string, registerComponents func(*fsm.Machine[*World])) error {
	// Register Actions/Guards
	registerComponents(cs.fsm)

	// Load Graph
	if err := cs.fsm.LoadJSON([]byte(configJSON)); err != nil {
		return fmt.Errorf("failed to load FSM JSON: %w", err)
	}

	// Initialize State (enters initial state)
	if err := cs.fsm.Init(cs.ctx.World, cs.fsm.InitialStateID); err != nil {
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
	cs.nextTickDeadline = cs.timeProvider.Now().Add(cs.tickInterval)
	cs.lastGameTickTime = cs.timeProvider.Now()
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
		default:
		}

		var sleepDuration time.Duration

		if cs.ctx.IsPaused.Load() {
			sleepDuration = cs.tickInterval * 2
		} else {
			gameNow := cs.timeProvider.Now()

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

				sleepDuration = deadline.Sub(cs.timeProvider.Now())
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
			case <-cs.stopChan:
				return
			}
		}
	}
}

// dispatchAndProcessEvents processes pending events through Router AND FSM
func (cs *ClockScheduler) dispatchAndProcessEvents() {
	eventsList := cs.ctx.eventQueue.Consume()
	for _, ev := range eventsList {
		cs.fsm.HandleEvent(cs.ctx.World, ev.Type)

		if handlers, ok := cs.eventRouter.GetHandlers(ev.Type); ok {
			for _, h := range handlers {
				h.HandleEvent(ev)
			}
		}
	}
}

// DispatchEventsImmediately processes all pending events synchronously
func (cs *ClockScheduler) DispatchEventsImmediately() {
	cs.ctx.World.RunSafe(func() {
		cs.dispatchAndProcessEvents()
	})
}

// processTick executes one clock cycle
func (cs *ClockScheduler) processTick() {
	if cs.ctx.IsPaused.Load() {
		return
	}

	cs.ctx.World.RunSafe(func() {
		now := cs.timeProvider.Now()
		cs.timeRes.Update(
			now,
			cs.timeProvider.RealTime(),
			cs.tickInterval,
			cs.ctx.State.GetFrameNumber(),
		)

		// Process Events (Input -> FSM -> Systems)
		cs.dispatchAndProcessEvents()

		// Update FSM Logic (Tick transitions)
		cs.fsm.Update(cs.ctx.World, cs.tickInterval)

		// Run Systems
		cs.ctx.World.UpdateLocked()
	})

	ticks := cs.ctx.State.IncrementGameTicks()
	cs.statTicks.Store(int64(ticks))

	if cs.tickCount.Load()%20 == 0 {
		cs.ctx.State.UpdateAPM(cs.statusReg)
	}
}