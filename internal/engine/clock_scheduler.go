package engine

import (
	"fmt"
	"io/fs"
	"strconv"
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

	// tap observes every event before dispatch; harness-only, set before Start
	tap func(event.GameEvent)

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
	statEvInvalid       *atomic.Int64
	statSettleExhausted *atomic.Int64
	statEvDispatchTypes *status.AtomicString
	statEvDeadTypes     *status.AtomicString
	statSettlePass      [settleSourceCount]*atomic.Int64

	// Scheduler-owned accumulators are mutated and published under the world lock.
	evBackoffs      int64
	tickSlips       int64
	tickSlipPending bool

	eventDispatch [event.EventTypeCount]int64
	eventDead     [event.EventTypeCount]int64
	eventTypeBuf  []byte
	eventDeadBuf  []byte

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

const (
	settleSourcePre = iota
	settleSourcePost
	settleSourceLoop
	settleSourceReset
	settleSourceInput
	settleSourceManual
	settleSourceWire
	settleSourceCount
)

var settleSourceNames = [settleSourceCount]string{"pre", "post", "loop", "reset", "input", "settle", "wire"}

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

		// SimEpoch, not the pacing clock: elapsed game time is tick * interval on
		// every instance, which is what makes time.game_elapsed_ms comparable.
		gameStartTime: SimEpoch,

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
		statEvInvalid:       statusReg.Ints.Get("event.invalid"),
		statSettleExhausted: statusReg.Ints.Get("event.settle_exhausted"),
		statEvDispatchTypes: statusReg.Strings.Get("event.dispatch_by_type"),
		statEvDeadTypes:     statusReg.Strings.Get("event.dead_by_type"),

		statFSMName:    statusReg.Strings.Get("fsm.state"),
		statFSMElapsed: statusReg.Ints.Get("fsm.elapsed"),
		statFSMMaxDur:  statusReg.Ints.Get("fsm.max_duration"),
		statFSMIndex:   statusReg.Ints.Get("fsm.state_index"),
		statFSMTotal:   statusReg.Ints.Get("fsm.state_count"),
	}
	for i, name := range settleSourceNames {
		cs.statSettlePass[i] = statusReg.Ints.Get("event.settle_" + name)
	}
	cs.resetTelemetry()

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

// SetDispatchTap installs an observer called for every event before the FSM and
// system handlers see it, so a pooled payload is still the producer's. Harness-only:
// set before Start, or any time on a driven App, never on a running scheduler.
func (cs *ClockScheduler) SetDispatchTap(fn func(event.GameEvent)) { cs.tap = fn }

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
		cs.world.Resources.Status.TriggerFSM(region)
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
	if cs.world.Resources.Status.Frozen() {
		return
	}
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
	cs.world.RunSafe(func() { cs.settleLocked("settle") })
}

// settleLocked dispatches pending events and closes the settle group when it
// consumed any. Caller MUST hold the world lock.
func (cs *ClockScheduler) settleLocked(src string) {
	if cs.dispatchAndProcessEvents(src) > 0 {
		cs.world.Resources.Event.Queue.NextBoundary()
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
					cs.tickSlipPending = true
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
	cs.world.Resources.Status.Trigger(status.TrigBreak)
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
	var pendingBackoffs int64
	var pendingRun uint64

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
				if pendingBackoffs != 0 && pendingRun == cs.world.Resources.Event.Queue.Stamp().Run {
					cs.evBackoffs += pendingBackoffs
				}
				pendingBackoffs = 0
				if cs.dispatchOnePass("loop") > 0 {
					cs.world.Resources.Event.Queue.NextBoundary()
				}
				cs.world.Unlock()
				backoffCount = 0
				continue
			}

			// Backoff tracking
			backoffCount++
			run := cs.world.Resources.Event.Queue.Stamp().Run
			if pendingBackoffs == 0 || pendingRun == run {
				pendingBackoffs++
			} else {
				pendingBackoffs = 1
			}
			pendingRun = run

			// Force progress after threshold
			if backoffCount >= cs.eventLoopBackoffMax {
				// Check shutdown before blocking lock to prevent Stop() hang
				if !cs.running.Load() {
					return
				}
				cs.world.Lock()
				if pendingRun == cs.world.Resources.Event.Queue.Stamp().Run {
					cs.evBackoffs += pendingBackoffs
				}
				pendingBackoffs = 0
				if cs.dispatchOnePass("loop") > 0 {
					cs.world.Resources.Event.Queue.NextBoundary()
				}
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

	// APM admission; input while paused is not gameplay
	apmOpen := !cs.ctl.IsPaused()
	var apmWeight uint64

	// Harness observer, hoisted with the other gates
	tap := cs.tap

	var nFSM, nSys, nDead int
	for _, ev := range eventsList {
		// Before any handler runs: a pooled payload is still the producer's
		if tap != nil {
			tap(ev)
		}

		handlers, _ := cs.eventRouter.GetHandlers(ev.Type)

		// FSM first, and its result is what makes sys=0 meaningful
		took := cs.fsm.HandleEvent(cs.world, ev)

		if took {
			nFSM++
		}
		if len(handlers) > 0 {
			nSys++
		}
		dead := !took && len(handlers) == 0
		if dead {
			nDead++
		}
		if ev.Type <= event.EventNone || int(ev.Type) >= event.EventTypeCount {
			cs.statEvInvalid.Add(1)
		} else {
			cs.world.Resources.Event.Queue.RecordDispatch(ev.Type, dead)
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

		if apmOpen && ev.Origin == event.OriginInput && apmAdmits(ev.Type) {
			apmWeight += parameter.APMWeightFull
		}

		if brkEv != 0 && ev.Type == brkEv {
			if bs := cs.ctl.Trip(StepEvent, "", ev.Type); bs != nil {
				cs.breakHit(bs, event.GetEventName(ev.Type))
			}
			brkEv = 0
		}
	}

	if apmWeight > 0 {
		cs.world.Resources.Game.State.RecordActionWeight(apmWeight)
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
	cs.recordSettlePass(src)
	return len(eventsList)
}

// apmAdmits reports whether an input-origin event counts as player effort.
// A screen resize carries OriginInput so a replay reflows, not because the player acted.
func apmAdmits(t event.EventType) bool {
	return t != event.EventScreenResize
}

// dispatchAndProcessEvents settles pending events with an iteration cap and
// returns how many were dispatched
func (cs *ClockScheduler) dispatchAndProcessEvents(src string) int {
	total := 0
	last := 0
	for range parameter.EventLoopIterations {
		last = cs.dispatchOnePass(src)
		total += last
		if last == 0 {
			break
		}
	}
	if last != 0 && cs.world.Resources.Event.Queue.Len() != 0 {
		cs.statSettleExhausted.Add(1)
	}
	return total
}

// recordSettlePass increments the fixed source bucket for one non-empty pass.
func (cs *ClockScheduler) recordSettlePass(src string) {
	var index int
	switch src {
	case "pre":
		index = settleSourcePre
	case "post":
		index = settleSourcePost
	case "loop":
		index = settleSourceLoop
	case "reset":
		index = settleSourceReset
	case "input":
		index = settleSourceInput
	case "settle":
		index = settleSourceManual
	case "wire":
		index = settleSourceWire
	default:
		return
	}
	cs.statSettlePass[index].Add(1)
}

// resetTelemetry clears scheduler and queue-facing session diagnostics.
// Construction and executeReset are the only callers.
func (cs *ClockScheduler) resetTelemetry() {
	cs.evBackoffs = 0
	cs.tickSlips = 0
	cs.tickSlipPending = false
	cs.lastEvDropped = 0

	for _, stat := range []*atomic.Int64{
		cs.statTicks,
		cs.statAPM,
		cs.statMusicAPM,
		cs.statEvBackoffs,
		cs.statEvDispatches,
		cs.statEvDead,
		cs.statEntityCount,
		cs.statEntityCreated,
		cs.statEntityDestroyed,
		cs.statQueueLen,
		cs.statQueueMax,
		cs.statGameElapsedMs,
		cs.statEvDropped,
		cs.statTickSlips,
		cs.statEvInvalid,
		cs.statSettleExhausted,
	} {
		stat.Store(0)
	}
	for _, stat := range cs.statSettlePass {
		stat.Store(0)
	}
	cs.statEvDispatchTypes.StoreIfChanged("-")
	cs.statEvDeadTypes.StoreIfChanged("-")
	cs.statFSMName.StoreIfChanged("-")
	cs.statFSMElapsed.Store(0)
	cs.statFSMMaxDur.Store(0)
	cs.statFSMIndex.Store(0)
	cs.statFSMTotal.Store(0)
	for i := range cs.regionStats {
		rs := &cs.regionStats[i]
		rs.state.StoreIfChanged("-")
		rs.index.Store(0)
		rs.elapsed.Store(0)
		rs.maxDur.Store(0)
		rs.paused.Store(false)
	}
}

// publishEventTelemetry formats the sparse per-type arrays on snapshot cadence.
func (cs *ClockScheduler) publishEventTelemetry() {
	cs.world.Resources.Event.Queue.SnapshotTelemetry(&cs.eventDispatch, &cs.eventDead)
	cs.eventTypeBuf = appendEventTypeCounts(cs.eventTypeBuf[:0], &cs.eventDispatch)
	cs.eventDeadBuf = appendEventTypeCounts(cs.eventDeadBuf[:0], &cs.eventDead)
	cs.statEvDispatchTypes.Store(string(cs.eventTypeBuf))
	cs.statEvDeadTypes.Store(string(cs.eventDeadBuf))
}

func appendEventTypeCounts(dst []byte, counts *[event.EventTypeCount]int64) []byte {
	for i := 1; i < event.EventTypeCount; i++ {
		if counts[i] == 0 {
			continue
		}
		if len(dst) != 0 {
			dst = append(dst, ' ')
		}
		name := event.GetEventName(event.EventType(i))
		if name == "" {
			dst = append(dst, '#')
			dst = strconv.AppendInt(dst, int64(i), 10)
		} else {
			dst = append(dst, name...)
		}
		dst = append(dst, '=')
		dst = strconv.AppendInt(dst, counts[i], 10)
	}
	if len(dst) == 0 {
		return append(dst, '-')
	}
	return dst
}

// executeReset performs FSM reset while scheduler mutex is held
func (cs *ClockScheduler) executeReset() {
	vlog.Info("fsm", "msg", "session reset")

	// 1. Synchronize with world lock
	// Acquire lock, wait till MetaSystem finishes synchronous cleanup and releases the lock
	// NOTE: Do not use RunSafe if called from a blocking systems
	cs.world.Lock()
	defer cs.world.Unlock()

	// 2. Drain and discard stale events from the previous game session
	_ = cs.world.Resources.Event.Queue.Consume()
	cs.world.Resources.Event.Queue.ResetTelemetry()
	cs.resetTelemetry()

	// 3. Reset Scheduler internal timing. The deadline is a pacing value and stays
	// on the wall clock; the game-time origin is SimEpoch, because the tick counter
	// the elapsed figure is derived from restarts with the run.
	cs.nextTickDeadline = cs.ctl.Now().Add(cs.tickInterval)
	cs.gameStartTime = SimEpoch

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

	// 7. Settle FSM-reset and unpause events before releasing the lock. No boundary
	//    bump: this is a phase of the reset, reached identically by a run and its replay.
	cs.dispatchAndProcessEvents("reset")

	// 8. Systems re-Init on the reset dispatch that preceded this call, so the
	//    next game's streams differ while staying a function of the root seed
	session := cs.world.Resources.Rand.NextSession()
	vlog.Info("app", "msg", "rng session", "session", session)
	cs.world.Resources.Event.Queue.AnchorJournal(cs.anchorLive(session))
}

// anchorLive reads the per-emission anchor fields: this instance's terminal, the
// D-14 map latch, the followed slot and the time scale.
// Caller MUST hold updateMutex — reads Config and the roster.
func (cs *ClockScheduler) anchorLive(session uint64) event.AnchorLive {
	cfg := cs.world.Resources.Config
	w, h := ScreenSize(cfg)
	return event.AnchorLive{
		Speed:         cs.ctl.Scale().String(),
		Session:       session,
		Width:         w,
		Height:        h,
		MapWidth:      cfg.MapWidth,
		MapHeight:     cfg.MapHeight,
		CropOnResize:  cfg.CropOnResize,
		SessionShared: cs.world.SessionShared(),
		Slot:          cs.world.Resources.Player.LocalSlot(),
	}
}

// DispatchEventsImmediately processes all pending events synchronously
func (cs *ClockScheduler) DispatchEventsImmediately() {
	cs.world.RunSafe(func() { cs.settleLocked("input") })
}

// processTick executes one clock cycle
func (cs *ClockScheduler) processTick() {
	if cs.ctl.IsPaused() && !cs.stepping {
		return
	}

	// Lock sampling is a per-tick decision, not a per-acquire probe
	cs.world.SetLockSampling(vlog.On("lock", vlog.LevelDebug) || cs.world.Resources.Status.RecorderActive())
	SetDomainAudit(vlog.On("domain", vlog.LevelDebug))

	var (
		tickTime         time.Time // this tick's game instant, read once under the lock
		screenW, screenH int       // terminal dims for the anchor, derived under the lock
		mapW, mapH       int       // D-14 map latch for the anchor, read under the lock
		cropOnResize     bool
		sessionShared    bool  // D-14 crop admissibility, which a reproduction adopts
		slot             uint8 // local roster slot for the anchor, read under the lock
		ticks            uint64
		dropped          uint64
		droppedDelta     uint64
	)

	cs.world.RunSafe(func() {
		if cs.tickSlipPending {
			cs.tickSlips++
			cs.tickSlipPending = false
		}
		// The barrier applies against the completed tick's stamp. Its settle group
		// therefore replays between ticks, before the next BeginTick resets Boundary.
		tick := cs.world.Resources.Game.State.GetGameTicks() + 1

		// The simulation instant is derived from the tick, never from the pacing
		// clock: it is shared state, and every participant must read the same value
		// at the same tick (SimEpoch). The pacing clock still decides *when* this
		// tick runs, and RealTime below still reports the wall.
		tickTime = SimTime(tick, cs.tickInterval)
		if cs.world.Resources.Event.Queue.ReceiveWire(tick) > 0 {
			cs.settleLocked("wire")
		}

		// Stamp under the lock: a producer must not observe the new tick before
		// the tick body it belongs to has started.
		cs.world.Resources.Status.Correlation().SetTick(tick)
		cs.world.Resources.Event.Queue.BeginTick(tick)

		// 1. Sync Time
		cs.world.Resources.Time.Update(
			tickTime,
			cs.ctl.RealTime(),
			cs.tickInterval,
		)

		// 2. Update game elapsed time status
		cs.statGameElapsedMs.Store(tickTime.Sub(cs.gameStartTime).Milliseconds())

		// 3. Initial Settling: Resolve everything accumulated during game tick.

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

		// 8. Commit the tick and every registry write while the world is stable.
		gs := cs.world.Resources.Game.State
		ticks = gs.IncrementGameTicks()
		gs.UpdateAPM(tickTime)

		cs.statTicks.Store(int64(ticks))
		cs.statAPM.Store(int64(gs.GetAPM()))
		cs.statMusicAPM.Store(int64(gs.GetMusicAPM()))
		cs.statEntityCount.Store(int64(cs.world.Positions.CountEntities()))
		cs.statEntityCreated.Store(cs.world.CreatedCount())
		cs.statEntityDestroyed.Store(cs.world.DestroyedCount())
		cs.statEvBackoffs.Store(cs.evBackoffs)
		cs.statTickSlips.Store(cs.tickSlips)

		qlen := int64(cs.world.Resources.Event.Queue.Len())
		cs.statQueueLen.Store(qlen)
		if qlen > cs.statQueueMax.Load() {
			cs.statQueueMax.Store(qlen)
		}

		dropped = cs.world.Resources.Event.Queue.Dropped()
		cs.statEvDropped.Store(int64(dropped))
		if dropped > cs.lastEvDropped {
			droppedDelta = dropped - cs.lastEvDropped
			cs.lastEvDropped = dropped
		}

		if parameter.StatSnapshotTicks != 0 && ticks%parameter.StatSnapshotTicks == 0 {
			cs.world.Positions.PublishTelemetry()
			cs.publishEventTelemetry()
		}
		// Outbound transport closes the tick: everything this tick produced has
		// settled, so a peer receives one tick's artifacts as one tick's worth
		cs.world.Resources.Event.Queue.FlushWire(ticks)

		cfg := cs.world.Resources.Config
		screenW, screenH = ScreenSize(cfg)
		mapW, mapH, cropOnResize = cfg.MapWidth, cfg.MapHeight, cfg.CropOnResize
		sessionShared = cs.world.SessionShared()
		slot = cs.world.Resources.Player.LocalSlot()
	})

	if bs := cs.ctl.Expire(ticks); bs != nil {
		cs.breakHit(bs, "expired")
	}

	// Queue overflow is silent state loss; report every increase.
	if droppedDelta != 0 {
		vlog.Warn("event", "msg", "queue overflow",
			"dropped", dropped,
			"delta", droppedDelta)
		cs.world.Resources.Status.Trigger(status.TrigDrop)
	}

	// Status snapshot: world lock released and every stat above committed,
	// so the reading belongs to exactly this tick. Lock-free by construction.
	cs.world.Resources.Status.Tick(ticks)

	// Anchor cadence: a rotated journal file must be interpretable on its own
	if event.AnchorDue(ticks) {
		cs.world.Resources.Event.Queue.AnchorJournal(event.AnchorLive{
			Speed:         cs.ctl.Scale().String(),
			Session:       cs.world.Resources.Rand.Session(),
			Width:         screenW,
			Height:        screenH,
			MapWidth:      mapW,
			MapHeight:     mapH,
			CropOnResize:  cropOnResize,
			SessionShared: sessionShared,
			Slot:          slot,
		})
	}
}
