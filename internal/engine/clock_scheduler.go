package engine

import (
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/fsm"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/status"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// ClockScheduler manages game logic on a fixed tick
// Provides infrastructure for phase transitions and state ownership
// Handles pause-aware scheduling without busy-wait
type ClockScheduler struct {
	world *World

	ctl *TimeControl // sole time surface; pause is read from the clock it schedules

	// Tick configuration
	tickInterval     time.Duration
	stepping         bool      // scheduler-goroutine only; opens the pause gate for one tick
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
	statTicks           *atomic.Int64
	statAPM             *atomic.Int64
	statMusicAPM        *atomic.Int64
	statEvBackoffs      *atomic.Int64
	statEvDispatches    *atomic.Int64
	statEvDead          *atomic.Int64
	statEntityCount     *atomic.Int64
	statEntityCreated   *atomic.Int64
	statEntityDestroyed *atomic.Int64
	statQueueLen        *atomic.Int64
	statQueueMax        *atomic.Int64
	statGameElapsedMs   *atomic.Int64
	statEvDropped       *atomic.Int64
	statTickSlips       *atomic.Int64

	// Log state: overflow edge detection
	lastEvDropped uint64

	// FSM telemetry: foreground summary plus one metric set per declared region
	statFSMName    *status.AtomicString
	statFSMElapsed *atomic.Int64
	statFSMMaxDur  *atomic.Int64
	statFSMIndex   *atomic.Int64
	statFSMTotal   *atomic.Int64
	regionStats    []regionStat
}

// regionStat caches the status pointers for one declared FSM region.
// Keys are "fsm.<region>.<field>", so SplitKey folds every region into the
// single "fsm" stat record and the viewer drills down without new machinery.
type regionStat struct {
	name    string
	state   *status.AtomicString
	index   *atomic.Int64
	elapsed *atomic.Int64
	maxDur  *atomic.Int64
	paused  *atomic.Bool
}

// NewClockScheduler creates a new clock scheduler with specified tick interval
// Receives frameReady sync (receive) channel and returns game updateDone (send) and resetRequest (send) channels
func NewClockScheduler(
	world *World,
	ctl *TimeControl,
	tickInterval time.Duration,
	frameReady <-chan struct{},
) (*ClockScheduler, <-chan struct{}, chan<- struct{}) {
	updateDone := make(chan struct{}, 1)
	resetChan := make(chan struct{}, 1)

	statusReg := world.Resources.Status

	cs := &ClockScheduler{
		world:        world,
		ctl:          ctl,
		tickInterval: tickInterval,

		gameStartTime: ctl.Now(),

		eventRouter: event.NewRouter(world.Resources.Event.Queue),

		frameReady: frameReady,
		updateDone: updateDone,
		resetChan:  resetChan,
		stopChan:   make(chan struct{}),

		fsm: fsm.NewMachine[*World](),

		eventLoopInterval:   parameter.EventLoopInterval,
		eventLoopBackoffMax: parameter.EventLoopBackoffMax,

		statTicks:           statusReg.Ints.Get("engine.ticks"),
		statAPM:             statusReg.Ints.Get("engine.apm"),
		statMusicAPM:        statusReg.Ints.Get("engine.music_apm"),
		statEvBackoffs:      statusReg.Ints.Get("event.backoffs"),
		statEvDispatches:    statusReg.Ints.Get("event.dispatches"),
		statEvDead:          statusReg.Ints.Get("event.dead"),
		statEntityCount:     statusReg.Ints.Get("entity.count"),
		statEntityCreated:   statusReg.Ints.Get("entity.created_total"),
		statEntityDestroyed: statusReg.Ints.Get("entity.destroyed_total"),
		statQueueLen:        statusReg.Ints.Get("event.queue_len"),
		statQueueMax:        statusReg.Ints.Get("event.queue_max"),
		statGameElapsedMs:   statusReg.Ints.Get("time.game_elapsed_ms"),
		statEvDropped:       statusReg.Ints.Get("event.dropped"),
		statTickSlips:       statusReg.Ints.Get("engine.tick_slips"),

		statFSMName:    statusReg.Strings.Get("fsm.state"),
		statFSMElapsed: statusReg.Ints.Get("fsm.elapsed"),
		statFSMMaxDur:  statusReg.Ints.Get("fsm.max_duration"),
		statFSMIndex:   statusReg.Ints.Get("fsm.state_index"),
		statFSMTotal:   statusReg.Ints.Get("fsm.state_count"),
	}

	// The FSM is scheduler-owned, so region control arrives as an event rather
	// than as an API pair reaching through App
	cs.eventRouter.Register(cs)

	return cs, updateDone, resetChan
}

// EventTypes returns the event types the scheduler handles directly
func (cs *ClockScheduler) EventTypes() []event.EventType {
	return []event.EventType{event.EventFSMRegionRequest}
}

// HandleEvent applies a region operation to the scheduler-owned FSM.
// Runs inside dispatchOnePass with the world lock held; anything the operation
// emits settles on a later pass.
func (cs *ClockScheduler) HandleEvent(ev event.GameEvent) {
	p, ok := ev.Payload.(*event.FSMRegionPayload)
	if !ok {
		cs.report("region: missing payload")
		return
	}
	if err := cs.applyRegionOp(p); err != nil {
		vlog.Error("fsm", "msg", "region request failed",
			"op", p.Op, "region", p.Region, "state", p.State, "error", err.Error())
		cs.report("region: " + err.Error())
		return
	}
	vlog.Info("fsm", "msg", "region request", "op", p.Op, "region", p.Region, "state", p.State)
}

// applyRegionOp dispatches one primitive region operation
func (cs *ClockScheduler) applyRegionOp(p *event.FSMRegionPayload) error {
	if p.Op == event.RegionList {
		return cs.reportRegions()
	}
	if p.Region == "" {
		return fmt.Errorf("%s requires a region name", p.Op)
	}
	if cs.fsm.GetRegionConfig(p.Region) == nil {
		return fmt.Errorf("undeclared region %q", p.Region)
	}

	switch p.Op {
	case event.RegionSpawn:
		id, ok := cs.fsm.GetStateID(p.State)
		if !ok {
			return fmt.Errorf("unknown state %q", p.State)
		}
		if err := cs.fsm.SpawnRegion(cs.world, p.Region, id); err != nil {
			return err
		}
	case event.RegionPause:
		if !cs.fsm.HasRegion(p.Region) {
			return fmt.Errorf("region %q is not active", p.Region)
		}
		cs.fsm.PauseRegion(p.Region)
	case event.RegionResume:
		if !cs.fsm.HasRegion(p.Region) {
			return fmt.Errorf("region %q is not active", p.Region)
		}
		cs.fsm.ResumeRegion(p.Region)
	case event.RegionTerminate:
		if err := cs.fsm.TerminateRegion(cs.world, p.Region); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown op %q", p.Op)
	}

	// Every op changes the active set, so every op reconciles
	if !cs.fsm.ExecuteAction(cs.world, "ApplyRegionSystemConfigs", nil) {
		return fmt.Errorf("fsm: action ApplyRegionSystemConfigs is not registered")
	}
	return nil
}

// report surfaces a scheduler-side message in the status bar
func (cs *ClockScheduler) report(msg string) {
	cs.world.PushEvent(event.EventMetaStatusMessageRequest, &event.MetaStatusMessagePayload{
		Message:          msg,
		DurationOverride: true,
	})
}

// reportRegions publishes declared regions for :region list.
// An inactive region shows its declared initial state, which is what spawn expects;
// an active one shows its current state, suffixed '~' while paused.
func (cs *ClockScheduler) reportRegions() error {
	var b strings.Builder
	for i, r := range cs.fsm.DeclaredRegions() {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(r)

		t := cs.fsm.RegionTelemetry(r)
		if !t.Active {
			if cfg := cs.fsm.GetRegionConfig(r); cfg != nil && cfg.Initial != "" {
				b.WriteString("[" + cfg.Initial + "]")
			}
			continue
		}
		b.WriteString("(" + t.State + ")")
		if t.Paused {
			b.WriteByte('~')
		}
	}
	cs.report("regions: " + b.String())
	return nil
}

// RegisterEventHandler adds an event handler to router, must be called before Start()
func (cs *ClockScheduler) RegisterEventHandler(handler event.Handler) {
	cs.eventRouter.Register(handler)
}

// LoadFSMFromFS initializes HFSM from a filesystem (embed.FS or os.DirFS)
func (cs *ClockScheduler) LoadFSMFromFS(fsys fs.FS, entry string, registerComponents func(*fsm.Machine[*World])) error {
	registerComponents(cs.fsm)
	if err := fsm.LoadConfigFromFS(cs.fsm, fsys, entry); err != nil {
		return fmt.Errorf("failed to load FSM: %w", err)
	}
	return cs.initLoadedFSM()
}

// LoadFSMFromPath initializes HFSM from an external entry config
// Region file includes resolve relative to the file's directory
func (cs *ClockScheduler) LoadFSMFromPath(configPath string, registerComponents func(*fsm.Machine[*World])) error {
	registerComponents(cs.fsm)

	if err := fsm.LoadConfigFromPath(cs.fsm, configPath); err != nil {
		return fmt.Errorf("failed to load FSM: %w", err)
	}
	return cs.initLoadedFSM()
}

// initLoadedFSM is common post-load initialization
func (cs *ClockScheduler) initLoadedFSM() error {
	// Before Init: the initial region entries must reach the observer
	cs.bindFSMTelemetry()

	if err := cs.fsm.Init(cs.world); err != nil {
		return fmt.Errorf("failed to init FSM: %w", err)
	}
	return cs.applySystemConfig()
}

// applySystemConfig reconciles global then per-region toggles, so a region
// declaration wins over the root list. A missing action is a wiring
// regression, not a runtime condition.
func (cs *ClockScheduler) applySystemConfig() error {
	for _, name := range [...]string{"ApplyGlobalSystemConfig", "ApplyRegionSystemConfigs"} {
		if !cs.fsm.ExecuteAction(cs.world, name, nil) {
			return fmt.Errorf("fsm: action %s is not registered", name)
		}
	}
	return nil
}

// bindFSMTelemetry installs the transition taps and pre-registers one metric
// set per declared region, so no status key appears after the first tick.
func (cs *ClockScheduler) bindFSMTelemetry() {
	reg := cs.world.Resources.Status
	names := cs.fsm.DeclaredRegions()
	cs.regionStats = make([]regionStat, 0, len(names))
	for _, n := range names {
		k := "fsm." + n + "."
		cs.regionStats = append(cs.regionStats, regionStat{
			name:    n,
			state:   reg.Strings.Get(k + "state"),
			index:   reg.Ints.Get(k + "index"),
			elapsed: reg.Ints.Get(k + "elapsed"),
			maxDur:  reg.Ints.Get(k + "max_duration"),
			paused:  reg.Bools.Get(k + "paused"),
		})
	}

	// region is the first string field on every record, so vif-log's follow
	// key (f/F) walks one region's path
	cs.fsm.OnTransition = func(region string, from, to fsm.StateID, trigger event.EventType, internal bool) {
		if internal {
			if !vlog.On("fsm", vlog.LevelDebug) {
				return
			}
			vlog.Debug("fsm", "msg", "internal",
				"region", region,
				"state", cs.fsm.StateName(from),
				"via", event.GetEventName(trigger))
			return
		}
		status.TriggerFSM(region)
		if vlog.On("fsm", vlog.LevelInfo) {
			vlog.Info("fsm", "msg", "transition",
				"region", region,
				"from", cs.fsm.StateName(from),
				"to", cs.fsm.StateName(to),
				"via", event.GetEventName(trigger),
				"index", cs.fsm.StateIndices[to],
				"max_ms", cs.fsm.StateDurations[to].Milliseconds())
		}
		// After the transition record, so the breakpoint reads as its consequence
		if bs := cs.ctl.Trip(StepFSM, region, event.EventNone); bs != nil {
			cs.breakHit(bs, region+" "+cs.fsm.StateName(from)+" -> "+cs.fsm.StateName(to))
		}
	}

	cs.fsm.OnRegion = func(op, region string, state fsm.StateID) {
		if !vlog.On("fsm", vlog.LevelInfo) {
			return
		}
		vlog.Info("fsm", "msg", "region",
			"region", region,
			"op", op,
			"state", cs.fsm.StateName(state))
	}
}

// publishRegionStats mirrors every declared region into the status registry.
// Caller MUST hold updateMutex: reads live FSM state.
func (cs *ClockScheduler) publishRegionStats() {
	for i := range cs.regionStats {
		rs := &cs.regionStats[i]
		t := cs.fsm.RegionTelemetry(rs.name)
		if !t.Active {
			rs.state.StoreIfChanged("-")
			rs.index.Store(-1)
			rs.elapsed.Store(0)
			rs.maxDur.Store(0)
			rs.paused.Store(false)
			continue
		}
		rs.state.StoreIfChanged(t.State)
		rs.index.Store(int64(t.Index))
		rs.elapsed.Store(int64(t.TimeInState))
		rs.maxDur.Store(int64(t.MaxDuration))
		rs.paused.Store(t.Paused)
	}
}

// Start begins the scheduler loop
func (cs *ClockScheduler) Start() {
	if cs.running.CompareAndSwap(false, true) {
		cs.Prepare()
		cs.wg.Add(2) // 2 Goroutines
		// Use core.Go for safe execution with centralized crash handling
		core.Go(cs.schedulerLoop)
		core.Go(cs.eventLoop)
	}
}

// Prepare closes the system and metric sets from a harness that drives ticks
// or settles events before the first RunTicks call. Idempotent.
func (cs *ClockScheduler) Prepare() {
	cs.world.Seal() // no system registration once the goroutines are live
	cs.world.Resources.Status.Freeze()
}

// RunTicks advances the simulation by n ticks as fast as the caller's goroutine
// allows. Requires a manual clock: Step is a no-op on the interactive clock
// while it is running. The caller owns the loop, so Start must not be running —
// a concurrent scheduler or event goroutine reintroduces the nondeterminism
// this exists to avoid. A reset requested during a tick is serviced before the
// next one, matching the scheduler loop.
func (cs *ClockScheduler) RunTicks(n int) {
	cs.Prepare()
	for range n {
		cs.drainReset()
		cs.stepTick()
	}
	cs.drainReset()
}

// drainReset services a pending reset request without blocking
func (cs *ClockScheduler) drainReset() {
	select {
	case <-cs.resetChan:
		cs.executeReset()
	default:
	}
}

// Settle dispatches queued events without advancing time. Use after injecting
// input so its effects land before the next tick. Must not be called from a
// path already holding the world lock.
func (cs *ClockScheduler) Settle() {
	cs.world.RunSafe(func() {
		cs.dispatchAndProcessEvents("settle")
	})
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

// schedulerLoop runs the main scheduling loop with pause awareness.
// Deadlines live in game time; sleeps live in wall time, so every wait
// converts through the clock's current rate.
func (cs *ClockScheduler) schedulerLoop() {
	defer cs.wg.Done()

	cs.nextTickDeadline = cs.ctl.Now().Add(cs.tickInterval)

	timer := stoppedTimer()
	defer timer.Stop()
	frameTimer := stoppedTimer()
	defer frameTimer.Stop()

	wasPaused := false

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

		var sleepDuration time.Duration // wall clock

		if cs.ctl.IsPaused() {
			if cs.ctl.TakeStep() {
				cs.stepTick()
				continue // drain the allowance without sleeping
			}
			wasPaused = true
			// Game time is frozen, so no game duration converts; poll on wall time
			sleepDuration = parameter.PausedPollInterval
		} else {
			if wasPaused {
				// Game time stood still or was stepped; re-anchor so the first
				// live tick is not owed a burst
				cs.nextTickDeadline = cs.ctl.Now().Add(cs.tickInterval)
				wasPaused = false
			}
			gameNow := cs.ctl.Now()
			deadline := cs.nextTickDeadline

			if !gameNow.Before(deadline) {
				if !cs.awaitFrame(frameTimer) {
					return
				}

				cs.processTick()

				cs.nextTickDeadline = cs.nextTickDeadline.Add(cs.tickInterval)

				// Re-read after the tick: the debt is what the tick consumed,
				// not what was owed when it started
				gameNow = cs.ctl.Now()
				if gameNow.Sub(cs.nextTickDeadline) > cs.tickInterval*2 {
					// Systems cannot sustain this rate; drop the debt and count it
					cs.nextTickDeadline = gameNow.Add(cs.tickInterval)
					cs.statTickSlips.Add(1)
				}
				deadline = cs.nextTickDeadline

				select {
				case cs.updateDone <- struct{}{}:
				default:
				}

			}
			sleepDuration = max(cs.ctl.ToReal(deadline.Sub(gameNow)), 0)
		}

		if sleepDuration > 0 {
			timer.Reset(sleepDuration)
			select {
			case <-timer.C:
			case <-cs.ctl.Wake():
				drainTimer(timer) // rate changed; recompute against the new one
			case <-cs.resetChan:
				drainTimer(timer)
				cs.executeReset()
			case <-cs.stopChan:
				return
			}
		}
	}
}

// stepTick advances frozen game time by one interval and runs the tick past the
// pause gate. Render backpressure is skipped: the render loop grants no frame
// while paused, and a step is an inspection request, not a paced one.
func (cs *ClockScheduler) stepTick() {
	cs.ctl.Step(cs.tickInterval)
	cs.stepping = true
	cs.processTick()
	cs.stepping = false

	select {
	case cs.updateDone <- struct{}{}:
	default:
	}
}

// Reset rebuilds world state on the caller's goroutine, for harnesses that drive
// ticks directly rather than through the reset channel
func (cs *ClockScheduler) Reset() { cs.executeReset() }

// breakHit applies a tripped request: flush the recorder window, report the
// cause, and pause when asked
func (cs *ClockScheduler) breakHit(bs *BreakState, cause string) {
	status.Trigger(status.TrigBreak)
	vlog.Info("app", "msg", "breakpoint",
		"on", bs.Label, "cause", cause, "scale", bs.Restore.String(), "pause", bs.Pause)

	if bs.Pause {
		cs.world.PushEvent(event.EventGamePauseRequest, &event.GamePausePayload{Paused: true})
	}
	cs.world.PushEvent(event.EventMetaStatusMessageRequest, &event.MetaStatusMessagePayload{
		Message: "Break: " + cause, DurationOverride: true,
	})
}

// awaitFrame applies render backpressure: at real time and slower a tick waits
// for the render loop, bounded by a timeout so a stalled terminal cannot freeze
// the simulation. Above real time the operator has asked the world to outrun the
// display, so the handshake is skipped and the tick deadline is the only pacing.
// Returns false on shutdown.
func (cs *ClockScheduler) awaitFrame(t *time.Timer) bool {
	if cs.ctl.Scale().Faster() {
		return true
	}
	timeout := cs.ctl.ToReal(cs.tickInterval * 2)
	if timeout <= 0 {
		timeout = cs.tickInterval * 2
	}
	t.Reset(timeout)
	defer drainTimer(t)

	select {
	case <-cs.frameReady:
	case <-t.C:
	case <-cs.ctl.Wake(): // rate or pause changed; recompute rather than wait it out
	case <-cs.stopChan:
		return false
	}
	return true
}

// stoppedTimer returns an armed-but-drained timer ready for Reset
func stoppedTimer() *time.Timer {
	t := time.NewTimer(0)
	drainTimer(t)
	return t
}

// drainTimer stops a timer and clears a fire that may already be queued
func drainTimer(t *time.Timer) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
}

// eventLoop settles queued events between ticks so a frame renders a settled
// world. Runs regardless of pause: pause freezes the simulation (processTick),
// not delivery.
//
// The world lock is mandatory here, not merely for component safety:
// EventQueue.Consume is single-consumer, and updateMutex is what serializes
// this goroutine against processTick, DispatchEventsImmediately, and
// executeReset. Never Consume without holding it.
//
// TryLock first — short holds (frame snapshot, router RunSafe) are cheaper to
// skip and retry than to queue behind. Escalate to a blocking acquire after
// EventLoopBackoffMax misses: a hold that long means a tick is in progress,
// and its post-UpdateLocked events need settling before the next frame.
// Without the escalation the only guaranteed consumer is processTick, i.e.
// one tick of latency on exactly the ticks that need it least.
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
			// Skip if queue empty (prevents busy-wait contention)
			if cs.world.Resources.Event.Queue.Len() == 0 {
				backoffCount = 0
				continue
			}

			// Attempt non-blocking lock
			if cs.world.TryLock() {
				cs.dispatchOnePass("loop")
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
				cs.dispatchOnePass("loop")
				cs.world.Unlock()
				backoffCount = 0
			}
		}
	}
}

// dispatchOnePass consumes and dispatches pending events exactly once.
// src labels the caller so a pass record identifies what produced the batch.
// Returns number of events processed.
func (cs *ClockScheduler) dispatchOnePass(src string) int {
	eventsList := cs.world.Resources.Event.Queue.Consume()
	if len(eventsList) == 0 {
		return 0
	}

	// Gates hoisted out of the loop: one atomic load each per pass.
	// Payloads are pooled and released by their handlers, so only the type
	// is logged — retaining ev.Payload would race the next Acquire.
	perEvent := vlog.On("dispatch", vlog.LevelTrace)
	summary := vlog.On("event", vlog.LevelDebug)

	// Breakpoint probe: one pointer load per pass, one compare per event
	var brkEv event.EventType
	if bs := cs.ctl.Armed(); bs != nil && bs.Mode == StepEvent {
		brkEv = bs.Event
	}

	var nFSM, nSys, nDead int
	for _, ev := range eventsList {
		handlers, _ := cs.eventRouter.GetHandlers(ev.Type)

		// FSM first, and its result is what makes sys=0 meaningful
		took := cs.fsm.HandleEvent(cs.world, ev)

		if took {
			nFSM++
		}
		if len(handlers) > 0 {
			nSys++
		}
		if !took && len(handlers) == 0 {
			nDead++
		}

		// Emitted after HandleEvent so the fsm verdict is known; any transition
		// record it produced carries via=<this event> and reads as the cause
		if perEvent {
			vlog.Detail("dispatch", "msg", "ev",
				"ev", event.GetEventName(ev.Type),
				"sys", len(handlers),
				"fsm", took)
		}

		for _, h := range handlers {
			h.HandleEvent(ev)
		}

		if brkEv != 0 && ev.Type == brkEv {
			if bs := cs.ctl.Trip(StepEvent, "", ev.Type); bs != nil {
				cs.breakHit(bs, event.GetEventName(ev.Type))
			}
			brkEv = 0
		}
	}

	if summary {
		vlog.Debug("event", "msg", "pass",
			"src", src,
			"n", len(eventsList),
			"fsm", nFSM,
			"sys", nSys,
			"dead", nDead)
	}

	cs.statEvDispatches.Add(int64(len(eventsList)))
	cs.statEvDead.Add(int64(nDead))
	return len(eventsList)
}

// dispatchAndProcessEvents settles pending events with iteration cap
// Used by reset path where immediate settling is required
func (cs *ClockScheduler) dispatchAndProcessEvents(src string) {
	for range parameter.EventLoopIterations {
		if cs.dispatchOnePass(src) == 0 {
			return
		}
	}
}

// executeReset performs FSM reset while scheduler mutex is held
func (cs *ClockScheduler) executeReset() {
	// New session: correlation stamp for every record below
	vlog.NextRun()
	vlog.Info("fsm", "msg", "session reset")

	// 1. Synchronize with world lock
	// Acquire lock, wait till MetaSystem finishes synchronous cleanup and releases the lock
	// NOTE: Do not use RunSafe if called from a blocking systems
	cs.world.Lock()
	defer cs.world.Unlock()

	// 2. Drain and discard stale events from the previous game session
	_ = cs.world.Resources.Event.Queue.Consume()

	// 3. Reset Scheduler internal timing
	now := cs.ctl.Now()
	cs.nextTickDeadline = now.Add(cs.tickInterval)
	cs.gameStartTime = now

	// 4. Reset FSM state - This will trigger OnEnter actions
	if err := cs.fsm.Reset(cs.world); err != nil {
		panic(fmt.Errorf("FSM reset failed: %v", err))
	}

	// 5. Re-apply global system configuration (mirrors LoadFSM behavior)
	if err := cs.applySystemConfig(); err != nil {
		vlog.Error("fsm", "msg", "system config", "error", err.Error())
	}

	// 6. Unpause via the single owner so clock, context, and audio move
	//    together; settled below while the world lock is still held.
	cs.world.PushEvent(event.EventGamePauseRequest, &event.GamePausePayload{Paused: false})

	// 7. Settle FSM-reset and unpause events before releasing the lock
	cs.dispatchAndProcessEvents("reset")

	// 8. Systems re-Init on the reset dispatch that preceded this call, so the
	//    next game's streams differ while staying a function of the root seed
	vlog.Info("app", "msg", "rng session", "session", cs.world.Resources.Rand.NextSession())
}

// DispatchEventsImmediately processes all pending events synchronously
func (cs *ClockScheduler) DispatchEventsImmediately() {
	cs.world.RunSafe(func() {
		cs.dispatchAndProcessEvents("input")
	})
}

// processTick executes one clock cycle
func (cs *ClockScheduler) processTick() {
	if cs.ctl.IsPaused() && !cs.stepping {
		return
	}

	// Stamp the tick being executed; the counter is committed at the end of
	// this function, so records inside would otherwise carry the previous one
	vlog.SetTick(cs.world.Resources.Game.State.GetGameTicks() + 1)

	// Lock sampling is a per-tick decision, not a per-acquire probe
	SetLockSampling(vlog.On("lock", vlog.LevelDebug) || status.RecorderActive())

	var (
		entityCount int
		tickTime    time.Time // this tick's game instant, read once under the lock
	)

	cs.world.RunSafe(func() {
		tickTime = cs.ctl.Now()

		// 1. Sync Time
		cs.world.Resources.Time.Update(
			tickTime,
			cs.ctl.RealTime(),
			cs.tickInterval,
		)

		// 2. Update game elapsed time status
		cs.statGameElapsedMs.Store(tickTime.Sub(cs.gameStartTime).Milliseconds())

		// 3. Initial Settling: Resolve everything accumulated during game tick

		// Ensures FSM and Systems start with a consistent, settled world
		cs.dispatchAndProcessEvents("pre")

		// 4. FSM Update: Advance state machine (may emit new events via Actions)
		cs.fsm.Update(cs.world, cs.tickInterval)

		// 5. FSM telemetry (after update, before post-settling)
		// Transitions are reported by the OnTransition tap, not sampled here:
		// sampling collapses intra-tick chains and cannot see background regions
		stateName, stateID, timeInState := cs.fsm.GetActiveRegionTelemetry()
		cs.statFSMName.StoreIfChanged(stateName)
		cs.statFSMElapsed.Store(int64(timeInState))
		cs.statFSMMaxDur.Store(int64(cs.fsm.StateDurations[stateID]))
		cs.statFSMIndex.Store(int64(cs.fsm.StateIndices[stateID]))
		cs.statFSMTotal.Store(int64(cs.fsm.StateCount))
		cs.publishRegionStats()

		// 6. Post-FSM Settling: Resolve events emitted by FSM state transitions
		cs.dispatchAndProcessEvents("post")

		// 7. System Execution: Systems run on the final, settled state for this tick
		cs.world.UpdateLocked()

		// 8. Snapshot store-derived stats while the lock is held
		// Position has no internal locking; CountEntities outside this
		// closure races removeAt on the event-loop/main goroutines
		entityCount = cs.world.Positions.CountEntities()
	})

	// Lock-free / internally synchronized paths only below this line
	gs := cs.world.Resources.Game.State
	ticks := gs.IncrementGameTicks()
	if bs := cs.ctl.Expire(ticks); bs != nil {
		cs.breakHit(bs, "expired")
	}

	// APM rolls on game time; publish alongside the other tick counters
	gs.UpdateAPM(tickTime)

	cs.statTicks.Store(int64(ticks))
	cs.statAPM.Store(int64(gs.GetAPM()))
	cs.statMusicAPM.Store(int64(gs.GetMusicAPM()))
	cs.statEntityCount.Store(int64(entityCount))
	cs.statEntityCreated.Store(cs.world.CreatedCount())
	cs.statEntityDestroyed.Store(cs.world.DestroyedCount())
	qlen := int64(cs.world.Resources.Event.Queue.Len())
	cs.statQueueLen.Store(qlen)
	if qlen > cs.statQueueMax.Load() {
		cs.statQueueMax.Store(qlen) // high-water mark; sizing input for EventQueueSize
	}

	// Queue overflow is silent state loss; report every increase.
	// The counter is monotonic across sessions — the queue outlives reset
	dropped := cs.world.Resources.Event.Queue.Dropped()
	cs.statEvDropped.Store(int64(dropped))
	if dropped > cs.lastEvDropped {
		vlog.Warn("event", "msg", "queue overflow",
			"dropped", dropped,
			"delta", dropped-cs.lastEvDropped)
		cs.lastEvDropped = dropped
		status.Trigger(status.TrigDrop)
	}

	// Status snapshot: world lock released and every stat above committed,
	// so the reading belongs to exactly this tick. Lock-free by construction.
	cs.world.Resources.Status.Tick(ticks)
}
