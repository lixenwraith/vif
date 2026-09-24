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

// Scheduler manages game logic on a fixed tick
// Provides infrastructure for phase transitions and state ownership
// Handles pause-aware scheduling without busy-wait
type Scheduler struct {
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

	// paceTrim and paceStep are the guest pacing read off NetworkResource under the
	// tick's lock; whoever paces the ticks takes them with TakePace.
	paceTrim int32
	paceStep int64

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

// NewScheduler creates a scheduler on the given tick interval
// Receives frameReady sync (receive) channel and returns game updateDone (send) and resetRequest (send) channels
func NewScheduler(
	world *World,
	ctl *TimeControl,
	tickInterval time.Duration,
	frameReady <-chan struct{},
) (*Scheduler, <-chan struct{}, chan<- struct{}) {
	updateDone := make(chan struct{}, 1)
	resetChan := make(chan struct{}, 1)

	statusReg := world.Resources.Status

	s := &Scheduler{
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
		s.statSettlePass[i] = statusReg.Ints.Get("event.settle_" + name)
	}
	s.resetTelemetry()

	// The FSM is scheduler-owned, so region control arrives as an event rather
	// than as an API pair reaching through App
	s.eventRouter.Register(s)

	return s, updateDone, resetChan
}

// EventTypes returns the event types the scheduler handles directly
func (s *Scheduler) EventTypes() []event.EventType {
	return []event.EventType{event.EventFSMRegionRequest}
}

// HandleEvent applies a region operation to the scheduler-owned FSM.
// Runs inside dispatchOnePass with the world lock held; anything the operation
// emits settles on a later pass.
func (s *Scheduler) HandleEvent(ev event.GameEvent) {
	p, ok := ev.Payload.(*event.FSMRegionPayload)
	if !ok {
		s.report("region: missing payload")
		return
	}
	if err := s.applyRegionOp(p); err != nil {
		vlog.Error("fsm", "msg", "region request failed",
			"op", p.Op, "region", p.Region, "state", p.State, "error", err.Error())
		s.report("region: " + err.Error())
		return
	}
	vlog.Info("fsm", "msg", "region request", "op", p.Op, "region", p.Region, "state", p.State)
}

// applyRegionOp dispatches one primitive region operation
func (s *Scheduler) applyRegionOp(p *event.FSMRegionPayload) error {
	if p.Op == event.RegionList {
		return s.reportRegions()
	}
	if p.Region == "" {
		return fmt.Errorf("%s requires a region name", p.Op)
	}
	if s.fsm.GetRegionConfig(p.Region) == nil {
		return fmt.Errorf("undeclared region %q", p.Region)
	}

	switch p.Op {
	case event.RegionSpawn:
		id, ok := s.fsm.GetStateID(p.State)
		if !ok {
			return fmt.Errorf("unknown state %q", p.State)
		}
		if err := s.fsm.SpawnRegion(s.world, p.Region, id); err != nil {
			return err
		}
	case event.RegionPause:
		if !s.fsm.HasRegion(p.Region) {
			return fmt.Errorf("region %q is not active", p.Region)
		}
		s.fsm.PauseRegion(p.Region)
	case event.RegionResume:
		if !s.fsm.HasRegion(p.Region) {
			return fmt.Errorf("region %q is not active", p.Region)
		}
		s.fsm.ResumeRegion(p.Region)
	case event.RegionTerminate:
		if err := s.fsm.TerminateRegion(s.world, p.Region); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown op %q", p.Op)
	}

	// Every op changes the active set, so every op reconciles
	if !s.fsm.ExecuteAction(s.world, "ApplyRegionSystemConfigs", nil) {
		return fmt.Errorf("fsm: action ApplyRegionSystemConfigs is not registered")
	}
	return nil
}

// report surfaces a scheduler-side message in the status bar
func (s *Scheduler) report(msg string) {
	s.world.PushLocal(event.EventMetaStatusMessageRequest, &event.MetaStatusMessagePayload{
		Message:          msg,
		DurationOverride: true,
	})
}

// reportRegions publishes declared regions for :region list.
// An inactive region shows its declared initial state, which is what spawn expects;
// an active one shows its current state, suffixed '~' while paused.
func (s *Scheduler) reportRegions() error {
	var b strings.Builder
	for i, r := range s.fsm.DeclaredRegions() {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(r)

		t := s.fsm.RegionTelemetry(r)
		if !t.Active {
			if cfg := s.fsm.GetRegionConfig(r); cfg != nil && cfg.Initial != "" {
				b.WriteString("[" + cfg.Initial + "]")
			}
			continue
		}
		b.WriteString("(" + t.State + ")")
		if t.Paused {
			b.WriteByte('~')
		}
	}
	s.report("regions: " + b.String())
	return nil
}

// RegisterEventHandler adds an event handler to router, must be called before Start()
func (s *Scheduler) RegisterEventHandler(handler event.Handler) {
	s.eventRouter.Register(handler)
}

// SetDispatchTap installs an observer called for every event before the FSM and
// system handlers see it, so a pooled payload is still the producer's. Harness-only:
// set before Start, or any time on a driven App, never on a running scheduler.
func (s *Scheduler) SetDispatchTap(fn func(event.GameEvent)) { s.tap = fn }

// ExportFSM reads the FSM runtime's position for a D-19 capture: which state each
// region stands in, how long it has stood there, the variables guards read, and
// the delayed actions still pending. The state graph itself is configuration and
// travels with the build.
//
// Caller MUST hold updateMutex: the machine is tick-owned.
func (s *Scheduler) ExportFSM() fsm.MachineState { return s.fsm.Export() }

// ImportFSM places the FSM runtime where a capture found it. A staging import
// resolves the graph without side effects. A live import additionally replays the
// explicitly marked, idempotent ClassLocal lifecycle actions for state boundaries
// the imported position crossed; ordinary entry actions are never re-run.
//
// Caller MUST hold updateMutex.
func (s *Scheduler) ImportFSM(state fsm.MachineState, reconcileLocal bool) error {
	var err error
	if reconcileLocal {
		var actions int
		actions, err = s.fsm.ImportReconciled(s.world, state)
		if err == nil && actions > 0 {
			vlog.Debug("fsm", "msg", "import reconciled local lifecycle", "actions", actions)
		}
	} else {
		err = s.fsm.Import(s.world, state)
	}
	if err != nil {
		return err
	}
	// An import changes the active set, so it reconciles like every other op that
	// does. The toggles a region declares are a per-instance effect of a Shared
	// position (D-20) and are re-derived from it; a participant that arrives at the
	// position by installing a world runs no region's entry actions.
	if err := s.applySystemConfig(); err != nil {
		return err
	}
	// Region telemetry is derived from the position that just changed, and it is
	// part of the compared shared surface. Republish it here rather than waiting
	// for the next tick, so an installed world reports where it stands.
	s.publishRegionStats()
	stateName, stateID, timeInState := s.fsm.GetActiveRegionTelemetry()
	s.statFSMName.StoreIfChanged(stateName)
	s.statFSMElapsed.Store(int64(timeInState))
	s.statFSMMaxDur.Store(int64(s.fsm.StateDurations[stateID]))
	s.statFSMIndex.Store(int64(s.fsm.StateIndices[stateID]))
	s.statFSMTotal.Store(int64(s.fsm.StateCount))
	return nil
}

// LoadScenarioFromFS initializes the HFSM from a scenario on any filesystem
// (embed.FS, os.DirFS, or a scenario held in memory)
func (s *Scheduler) LoadScenarioFromFS(fsys fs.FS, entry string, registerComponents func(*fsm.Machine[*World])) error {
	registerComponents(s.fsm)
	if err := fsm.LoadScenarioFromFS(s.fsm, fsys, entry); err != nil {
		return fmt.Errorf("failed to load scenario: %w", err)
	}
	return s.initLoadedScenario()
}

// initLoadedScenario is common post-load initialization
func (s *Scheduler) initLoadedScenario() error {
	// Before Init: the initial region entries must reach the observer
	s.bindFSMTelemetry()

	if err := s.fsm.Init(s.world); err != nil {
		return fmt.Errorf("failed to init FSM: %w", err)
	}
	return s.applySystemConfig()
}

// applySystemConfig reconciles global then per-region toggles, so a region
// declaration wins over the root list. A missing action is a wiring
// regression, not a runtime condition.
func (s *Scheduler) applySystemConfig() error {
	for _, name := range [...]string{"ApplyGlobalSystemConfig", "ApplyRegionSystemConfigs"} {
		if !s.fsm.ExecuteAction(s.world, name, nil) {
			return fmt.Errorf("fsm: action %s is not registered", name)
		}
	}
	return nil
}

// bindFSMTelemetry installs the transition taps and pre-registers one metric
// set per declared region, so no status key appears after the first tick.
func (s *Scheduler) bindFSMTelemetry() {
	reg := s.world.Resources.Status
	names := s.fsm.DeclaredRegions()
	s.regionStats = make([]regionStat, 0, len(names))
	for _, n := range names {
		k := "fsm." + n + "."
		s.regionStats = append(s.regionStats, regionStat{
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
	s.fsm.OnTransition = func(region string, from, to fsm.StateID, trigger event.EventType, internal bool) {
		if internal {
			if !vlog.On("fsm", vlog.LevelDebug) {
				return
			}
			vlog.Debug("fsm", "msg", "internal",
				"region", region,
				"state", s.fsm.StateName(from),
				"via", event.GetEventName(trigger))
			return
		}
		s.world.Resources.Status.TriggerFSM(region)
		if vlog.On("fsm", vlog.LevelInfo) {
			vlog.Info("fsm", "msg", "transition",
				"region", region,
				"from", s.fsm.StateName(from),
				"to", s.fsm.StateName(to),
				"via", event.GetEventName(trigger),
				"index", s.fsm.StateIndices[to],
				"max_ms", s.fsm.StateDurations[to].Milliseconds())
		}
		// After the transition record, so the breakpoint reads as its consequence
		if bs := s.ctl.Trip(StepFSM, region, event.EventNone); bs != nil {
			s.breakHit(bs, region+" "+s.fsm.StateName(from)+" -> "+s.fsm.StateName(to))
		}
	}

	s.fsm.OnRegion = func(op, region string, state fsm.StateID) {
		if !vlog.On("fsm", vlog.LevelInfo) {
			return
		}
		vlog.Info("fsm", "msg", "region",
			"region", region,
			"op", op,
			"state", s.fsm.StateName(state))
	}
}

// publishRegionStats mirrors every declared region into the status registry.
// Caller MUST hold updateMutex: reads live FSM state.
func (s *Scheduler) publishRegionStats() {
	for i := range s.regionStats {
		rs := &s.regionStats[i]
		t := s.fsm.RegionTelemetry(rs.name)
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
func (s *Scheduler) Start() {
	if s.running.CompareAndSwap(false, true) {
		s.Prepare()
		s.wg.Add(2) // 2 Goroutines
		// Use core.Go for safe execution with centralized crash handling
		core.Go(s.schedulerLoop)
		core.Go(s.eventLoop)
	}
}

// Running reports whether the scheduler goroutines are live. A liveness probe
// reads it to tell a stalled tick loop from one that has not started: only the
// first is a fault.
func (s *Scheduler) Running() bool { return s.running.Load() }

// Prepare closes the system and metric sets from a harness that drives ticks
// or settles events before the first RunTicks call. Idempotent.
func (s *Scheduler) Prepare() {
	if s.world.Resources.Status.Frozen() {
		return
	}
	s.world.Seal() // no system registration once the goroutines are live
	s.world.Resources.Status.Freeze()
}

// RunTicks advances the simulation by n ticks as fast as the caller's goroutine
// allows. Requires a manual clock: Step is a no-op on the interactive clock
// while it is running. The caller owns the loop, so Start must not be running —
// a concurrent scheduler or event goroutine reintroduces the nondeterminism
// this exists to avoid. A reset requested during a tick is serviced before the
// next one, matching the scheduler loop.
func (s *Scheduler) RunTicks(n int) {
	s.Prepare()
	for range n {
		s.drainReset()
		s.stepTick()
	}
	s.drainReset()
}

// drainReset services a pending reset request without blocking
func (s *Scheduler) drainReset() {
	select {
	case <-s.resetChan:
		s.executeReset()
	default:
	}
}

// Settle dispatches queued events without advancing time. Use after injecting
// input so its effects land before the next tick. Must not be called from a
// path already holding the world lock.
func (s *Scheduler) Settle() {
	s.world.RunSafe(func() { s.settleLocked("settle") })
}

// settleLocked dispatches pending events and closes the settle group when it
// consumed any. Caller MUST hold the world lock.
func (s *Scheduler) settleLocked(src string) {
	if s.dispatchAndProcessEvents(src) > 0 {
		s.world.Resources.Event.Queue.NextBoundary()
	}
}

// Stop halts the scheduler loop
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() {
		if s.running.CompareAndSwap(true, false) {
			close(s.stopChan)
			s.wg.Wait()
		}
	})
}

// schedulerLoop runs the main scheduling loop with pause awareness.
// Deadlines live in game time; sleeps live in wall time, so every wait
// converts through the clock's current rate.
func (s *Scheduler) schedulerLoop() {
	defer s.wg.Done()

	s.nextTickDeadline = s.ctl.Now().Add(s.tickInterval)

	timer := stoppedTimer()
	defer timer.Stop()
	frameTimer := stoppedTimer()
	defer frameTimer.Stop()

	wasPaused := false

	for {
		select {
		case <-s.stopChan:
			return

		case <-s.resetChan:
			// Execute reset regardless of current pause state to prevent channel clogging
			s.executeReset()
			continue

		default:
		}

		var sleepDuration time.Duration // wall clock

		if s.ctl.IsPaused() {
			if s.ctl.TakeStep() {
				s.stepTick()
				continue // drain the allowance without sleeping
			}
			wasPaused = true
			// Game time is frozen, so no game duration converts; poll on wall time
			sleepDuration = parameter.PausedPollInterval
		} else {
			if wasPaused {
				// Game time stood still or was stepped; re-anchor so the first
				// live tick is not owed a burst
				s.nextTickDeadline = s.ctl.Now().Add(s.tickInterval)
				wasPaused = false
			}
			gameNow := s.ctl.Now()
			deadline := s.nextTickDeadline

			if !gameNow.Before(deadline) {
				if !s.awaitFrame(frameTimer) {
					return
				}

				s.processTick()

				trim, step := s.TakePace()
				s.nextTickDeadline = s.nextTickDeadline.Add(PacedInterval(s.tickInterval, trim, step))

				// Re-read after the tick: the debt is what the tick consumed,
				// not what was owed when it started
				gameNow = s.ctl.Now()
				if gameNow.Sub(s.nextTickDeadline) > s.tickInterval*2 {
					// Systems cannot sustain this rate; drop the debt and count it
					s.nextTickDeadline = gameNow.Add(s.tickInterval)
					s.tickSlipPending = true
				}
				deadline = s.nextTickDeadline

				select {
				case s.updateDone <- struct{}{}:
				default:
				}

			}
			sleepDuration = max(s.ctl.ToReal(deadline.Sub(gameNow)), 0)
		}

		if sleepDuration > 0 {
			timer.Reset(sleepDuration)
			select {
			case <-timer.C:
			case <-s.ctl.Wake():
				drainTimer(timer) // rate changed; recompute against the new one
			case <-s.resetChan:
				drainTimer(timer)
				s.executeReset()
			case <-s.stopChan:
				return
			}
		}
	}
}

// stepTick advances frozen game time by one interval and runs the tick past the
// pause gate. Render backpressure is skipped: the render loop grants no frame
// while paused, and a step is an inspection request, not a paced one.
func (s *Scheduler) stepTick() {
	s.ctl.Step(s.tickInterval)
	s.stepping = true
	s.processTick()
	s.stepping = false

	select {
	case s.updateDone <- struct{}{}:
	default:
	}
}

// Reset rebuilds world state on the caller's goroutine, for harnesses that drive
// ticks directly rather than through the reset channel
func (s *Scheduler) Reset() { s.executeReset() }

// breakHit applies a tripped request: flush the recorder window, report the
// cause, and pause when asked
func (s *Scheduler) breakHit(bs *BreakState, cause string) {
	s.world.Resources.Status.Trigger(status.TrigBreak)
	vlog.Info("app", "msg", "breakpoint",
		"on", bs.Label, "cause", cause, "scale", bs.Restore.String(), "pause", bs.Pause)

	if bs.Pause {
		s.world.PushLocal(event.EventGamePauseRequest, &event.GamePausePayload{Paused: true})
	}
	s.world.PushLocal(event.EventMetaStatusMessageRequest, &event.MetaStatusMessagePayload{
		Message: "Break: " + cause, DurationOverride: true,
	})
}

// awaitFrame applies render backpressure: at real time and slower a tick waits
// for the render loop, bounded by a timeout so a stalled terminal cannot freeze
// the simulation. Above real time the operator has asked the world to outrun the
// display, so the handshake is skipped and the tick deadline is the only pacing.
// Returns false on shutdown.
func (s *Scheduler) awaitFrame(t *time.Timer) bool {
	if s.ctl.Scale().Faster() {
		return true
	}
	timeout := s.ctl.ToReal(s.tickInterval * 2)
	if timeout <= 0 {
		timeout = s.tickInterval * 2
	}
	t.Reset(timeout)
	defer drainTimer(t)

	select {
	case <-s.frameReady:
	case <-t.C:
	case <-s.ctl.Wake(): // rate or pause changed; recompute rather than wait it out
	case <-s.stopChan:
		return false
	}
	return true
}

// TakePace returns the trim and the owed step the last tick read, clearing the step.
// Called by whichever goroutine paces the ticks, after a tick.
func (s *Scheduler) TakePace() (trimPermille int32, stepTicks int64) {
	step := s.paceStep
	s.paceStep = 0
	return s.paceTrim, step
}

// PacedInterval is the wall gap before the next tick: the interval trimmed by the
// guest pace, plus whole ticks owed by a step. Negative only for a step back.
func PacedInterval(interval time.Duration, trimPermille int32, stepTicks int64) time.Duration {
	return interval + interval*time.Duration(trimPermille)/1000 + interval*time.Duration(stepTicks)
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
func (s *Scheduler) eventLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.eventLoopInterval)
	defer ticker.Stop()

	backoffCount := 0
	var pendingBackoffs int64
	var pendingRun uint64

	for {
		select {
		case <-s.stopChan:
			return

		case <-ticker.C:
			// Skip if queue empty (prevents busy-wait contention)
			if s.world.Resources.Event.Queue.Len() == 0 {
				backoffCount = 0
				continue
			}

			// Attempt non-blocking lock
			if s.world.TryLock() {
				if pendingBackoffs != 0 && pendingRun == s.world.Resources.Event.Queue.Stamp().Run {
					s.evBackoffs += pendingBackoffs
				}
				pendingBackoffs = 0
				if s.dispatchOnePass("loop") > 0 {
					s.world.Resources.Event.Queue.NextBoundary()
				}
				s.world.Unlock()
				backoffCount = 0
				continue
			}

			// Backoff tracking
			backoffCount++
			run := s.world.Resources.Event.Queue.Stamp().Run
			if pendingBackoffs == 0 || pendingRun == run {
				pendingBackoffs++
			} else {
				pendingBackoffs = 1
			}
			pendingRun = run

			// Force progress after threshold
			if backoffCount >= s.eventLoopBackoffMax {
				// Check shutdown before blocking lock to prevent Stop() hang
				if !s.running.Load() {
					return
				}
				s.world.Lock()
				if pendingRun == s.world.Resources.Event.Queue.Stamp().Run {
					s.evBackoffs += pendingBackoffs
				}
				pendingBackoffs = 0
				if s.dispatchOnePass("loop") > 0 {
					s.world.Resources.Event.Queue.NextBoundary()
				}
				s.world.Unlock()
				backoffCount = 0
			}
		}
	}
}

// dispatchOnePass consumes and dispatches pending events exactly once.
// src labels the caller so a pass record identifies what produced the batch.
// Returns number of events processed.
func (s *Scheduler) dispatchOnePass(src string) int {
	eventsList := s.world.Resources.Event.Queue.Consume()
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
	if bs := s.ctl.Armed(); bs != nil && bs.Mode == StepEvent {
		brkEv = bs.Event
	}

	// APM admission; input while paused is not gameplay
	apmOpen := !s.ctl.IsPaused()
	var apmWeight uint64

	// Harness observer, hoisted with the other gates
	tap := s.tap

	var nFSM, nSys, nDead int
	for _, ev := range eventsList {
		// Before any handler runs: a pooled payload is still the producer's
		if tap != nil {
			tap(ev)
		}

		handlers, _ := s.eventRouter.GetHandlers(ev.Type)

		// FSM first, and its result is what makes sys=0 meaningful
		took := s.fsm.HandleEvent(s.world, ev)

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
			s.statEvInvalid.Add(1)
		} else {
			s.world.Resources.Event.Queue.RecordDispatch(ev.Type, dead)
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
		s.world.Resources.Event.Queue.RecordCrossingApplied(ev.CrossingSeq)

		if apmOpen && ev.Origin == event.OriginInput && apmAdmits(ev.Type) {
			apmWeight += parameter.APMWeightFull
		}

		if brkEv != 0 && ev.Type == brkEv {
			if bs := s.ctl.Trip(StepEvent, "", ev.Type); bs != nil {
				s.breakHit(bs, event.GetEventName(ev.Type))
			}
			brkEv = 0
		}
	}

	if apmWeight > 0 {
		s.world.Resources.Game.State.RecordActionWeight(apmWeight)
	}

	if summary {
		vlog.Debug("event", "msg", "pass",
			"src", src,
			"n", len(eventsList),
			"fsm", nFSM,
			"sys", nSys,
			"dead", nDead)
	}

	s.statEvDispatches.Add(int64(len(eventsList)))
	s.statEvDead.Add(int64(nDead))
	s.recordSettlePass(src)
	return len(eventsList)
}

// apmAdmits reports whether an input-origin event counts as player effort.
// A screen resize carries OriginInput so a replay reflows, not because the player acted.
func apmAdmits(t event.EventType) bool {
	return t != event.EventScreenResize
}

// dispatchAndProcessEvents settles pending events with an iteration cap and
// returns how many were dispatched
func (s *Scheduler) dispatchAndProcessEvents(src string) int {
	total := 0
	last := 0
	for range parameter.EventLoopIterations {
		last = s.dispatchOnePass(src)
		total += last
		if last == 0 {
			break
		}
	}
	if last != 0 && s.world.Resources.Event.Queue.Len() != 0 {
		s.statSettleExhausted.Add(1)
	}
	return total
}

// recordSettlePass increments the fixed source bucket for one non-empty pass.
func (s *Scheduler) recordSettlePass(src string) {
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
	s.statSettlePass[index].Add(1)
}

// resetTelemetry clears scheduler and queue-facing session diagnostics.
// Construction and executeReset are the only callers.
func (s *Scheduler) resetTelemetry() {
	s.evBackoffs = 0
	s.tickSlips = 0
	s.tickSlipPending = false
	s.lastEvDropped = 0

	for _, stat := range []*atomic.Int64{
		s.statTicks,
		s.statAPM,
		s.statMusicAPM,
		s.statEvBackoffs,
		s.statEvDispatches,
		s.statEvDead,
		s.statEntityCount,
		s.statEntityCreated,
		s.statEntityDestroyed,
		s.statQueueLen,
		s.statQueueMax,
		s.statGameElapsedMs,
		s.statEvDropped,
		s.statTickSlips,
		s.statEvInvalid,
		s.statSettleExhausted,
	} {
		stat.Store(0)
	}
	for _, stat := range s.statSettlePass {
		stat.Store(0)
	}
	s.statEvDispatchTypes.StoreIfChanged("-")
	s.statEvDeadTypes.StoreIfChanged("-")
	s.statFSMName.StoreIfChanged("-")
	s.statFSMElapsed.Store(0)
	s.statFSMMaxDur.Store(0)
	s.statFSMIndex.Store(0)
	s.statFSMTotal.Store(0)
	for i := range s.regionStats {
		rs := &s.regionStats[i]
		rs.state.StoreIfChanged("-")
		rs.index.Store(0)
		rs.elapsed.Store(0)
		rs.maxDur.Store(0)
		rs.paused.Store(false)
	}
}

// publishEventTelemetry formats the sparse per-type arrays on snapshot cadence.
func (s *Scheduler) publishEventTelemetry() {
	s.world.Resources.Event.Queue.SnapshotTelemetry(&s.eventDispatch, &s.eventDead)
	s.eventTypeBuf = appendEventTypeCounts(s.eventTypeBuf[:0], &s.eventDispatch)
	s.eventDeadBuf = appendEventTypeCounts(s.eventDeadBuf[:0], &s.eventDead)
	s.statEvDispatchTypes.Store(string(s.eventTypeBuf))
	s.statEvDeadTypes.Store(string(s.eventDeadBuf))
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
func (s *Scheduler) executeReset() {
	vlog.Info("fsm", "msg", "session reset")

	// 1. Synchronize with world lock
	// Acquire lock, wait till MetaSystem finishes synchronous cleanup and releases the lock
	// NOTE: Do not use RunSafe if called from a blocking systems
	s.world.Lock()
	defer s.world.Unlock()

	// 2. Drain and discard stale events from the previous game session
	_ = s.world.Resources.Event.Queue.Consume()
	s.world.Resources.Event.Queue.ResetTelemetry()
	s.resetTelemetry()

	// 3. Reset Scheduler internal timing. The deadline is a pacing value and stays
	// on the wall clock; the game-time origin is SimEpoch, because the tick counter
	// the elapsed figure is derived from restarts with the run.
	s.nextTickDeadline = s.ctl.Now().Add(s.tickInterval)
	s.gameStartTime = SimEpoch
	// Reset settles FSM entry actions before another tick can stamp TimeResource.
	// Rebase it first so those actions create deadlines in the new run's timeline.
	s.world.Resources.Time.Update(
		SimEpoch,
		s.ctl.RealTime(),
		s.tickInterval,
	)

	// 4. Reset FSM state - This will trigger OnEnter actions
	if err := s.fsm.Reset(s.world); err != nil {
		panic(fmt.Errorf("FSM reset failed: %v", err))
	}

	// 5. Re-apply global system configuration (mirrors the scenario load)
	if err := s.applySystemConfig(); err != nil {
		vlog.Error("fsm", "msg", "system config", "error", err.Error())
	}

	// 6. Unpause via the single owner so clock, context, and audio move
	//    together; settled below while the world lock is still held.
	s.world.PushLocal(event.EventGamePauseRequest, &event.GamePausePayload{Paused: false})

	// 7. Settle FSM-reset and unpause events before releasing the lock. No boundary
	//    bump: this is a phase of the reset, reached identically by a run and its replay.
	s.dispatchAndProcessEvents("reset")

	// 8. Systems re-Init on the reset dispatch that preceded this call, so the
	//    next game's streams differ while staying a function of the root seed
	session := s.world.Resources.Rand.NextSession()
	vlog.Info("app", "msg", "rng session", "session", session)
	s.world.Resources.Event.Queue.AnchorJournal(s.anchorLive(session))
}

// anchorLive reads the per-emission anchor fields: this instance's terminal, the
// D-14 map latch, the followed slot and the time scale.
// Caller MUST hold updateMutex — reads Config and the roster.
func (s *Scheduler) anchorLive(session uint64) event.AnchorLive {
	cfg := s.world.Resources.Config
	w, h := ScreenSize(cfg)
	return event.AnchorLive{
		Speed:         s.ctl.Scale().String(),
		Session:       session,
		Width:         w,
		Height:        h,
		MapWidth:      cfg.MapWidth,
		MapHeight:     cfg.MapHeight,
		CropOnResize:  cfg.CropOnResize,
		SessionShared: s.world.SessionShared(),
		Slot:          s.world.Resources.Player.LocalSlot(),
	}
}

// DispatchEventsImmediately processes all pending events synchronously
func (s *Scheduler) DispatchEventsImmediately() {
	s.world.RunSafe(func() { s.settleLocked("input") })
}

// processTick executes one clock cycle
func (s *Scheduler) processTick() {
	if s.ctl.IsPaused() && !s.stepping {
		return
	}

	// Lock sampling is a per-tick decision, not a per-acquire probe
	s.world.SetLockSampling(vlog.On("lock", vlog.LevelDebug) || s.world.Resources.Status.RecorderActive())
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

	s.world.RunSafe(func() {
		if r := s.world.Resources.Network; r != nil {
			s.paceTrim = r.Pace.Load()
			s.paceStep += r.PaceStep.Swap(0)
		} else {
			s.paceTrim = 0
		}
		if s.tickSlipPending {
			s.tickSlips++
			s.tickSlipPending = false
		}
		// The barrier applies against the completed tick's stamp. Its settle group
		// therefore replays between ticks, before the next BeginTick resets Boundary.
		tick := s.world.Resources.Game.State.GetGameTicks() + 1

		// The simulation instant is derived from the tick, never from the pacing
		// clock: it is shared state, and every participant must read the same value
		// at the same tick (SimEpoch). The pacing clock still decides *when* this
		// tick runs, and RealTime below still reports the wall.
		tickTime = SimTime(tick, s.tickInterval)
		if s.world.Resources.Event.Queue.ReceiveWire(tick) > 0 {
			s.settleLocked("wire")
		}

		// Stamp under the lock: a producer must not observe the new tick before
		// the tick body it belongs to has started.
		s.world.Resources.Status.Correlation().SetTick(tick)
		s.world.Resources.Event.Queue.BeginTick(tick)

		// 1. Sync Time
		s.world.Resources.Time.Update(
			tickTime,
			s.ctl.RealTime(),
			s.tickInterval,
		)

		// 2. Update game elapsed time status
		s.statGameElapsedMs.Store(tickTime.Sub(s.gameStartTime).Milliseconds())

		// 3. Initial Settling: Resolve everything accumulated during game tick.

		// Ensures FSM and Systems start with a consistent, settled world
		s.dispatchAndProcessEvents("pre")

		// 4. FSM Update: Advance state machine (may emit new events via Actions)
		s.fsm.Update(s.world, s.tickInterval)

		// 5. FSM telemetry (after update, before post-settling)
		// Transitions are reported by the OnTransition tap, not sampled here:
		// sampling collapses intra-tick chains and cannot see background regions
		stateName, stateID, timeInState := s.fsm.GetActiveRegionTelemetry()
		s.statFSMName.StoreIfChanged(stateName)
		s.statFSMElapsed.Store(int64(timeInState))
		s.statFSMMaxDur.Store(int64(s.fsm.StateDurations[stateID]))
		s.statFSMIndex.Store(int64(s.fsm.StateIndices[stateID]))
		s.statFSMTotal.Store(int64(s.fsm.StateCount))
		s.publishRegionStats()

		// 6. Post-FSM Settling: Resolve events emitted by FSM state transitions
		s.dispatchAndProcessEvents("post")

		// 7. System Execution: Systems run on the final, settled state for this tick
		s.world.UpdateLocked()

		// 8. Commit the tick and every registry write while the world is stable.
		gs := s.world.Resources.Game.State
		ticks = gs.IncrementGameTicks()
		gs.UpdateAPM(tickTime)

		s.statTicks.Store(int64(ticks))
		s.statAPM.Store(int64(gs.GetAPM()))
		s.statMusicAPM.Store(int64(gs.GetMusicAPM()))
		s.statEntityCount.Store(int64(s.world.Positions.CountEntities()))
		s.statEntityCreated.Store(s.world.CreatedCount())
		s.statEntityDestroyed.Store(s.world.DestroyedCount())
		s.statEvBackoffs.Store(s.evBackoffs)
		s.statTickSlips.Store(s.tickSlips)

		qlen := int64(s.world.Resources.Event.Queue.Len())
		s.statQueueLen.Store(qlen)
		if qlen > s.statQueueMax.Load() {
			s.statQueueMax.Store(qlen)
		}

		dropped = s.world.Resources.Event.Queue.Dropped()
		s.statEvDropped.Store(int64(dropped))
		if dropped > s.lastEvDropped {
			droppedDelta = dropped - s.lastEvDropped
			s.lastEvDropped = dropped
		}

		if parameter.StatSnapshotTicks != 0 && ticks%parameter.StatSnapshotTicks == 0 {
			s.world.Positions.PublishTelemetry()
			s.publishEventTelemetry()
		}
		// Outbound transport closes the tick: everything this tick produced has
		// settled, so a peer receives one tick's artifacts as one tick's worth
		s.world.Resources.Event.Queue.FlushWire(ticks)

		cfg := s.world.Resources.Config
		screenW, screenH = ScreenSize(cfg)
		mapW, mapH, cropOnResize = cfg.MapWidth, cfg.MapHeight, cfg.CropOnResize
		sessionShared = s.world.SessionShared()
		slot = s.world.Resources.Player.LocalSlot()
	})

	if bs := s.ctl.Expire(ticks); bs != nil {
		s.breakHit(bs, "expired")
	}

	// Queue overflow is silent state loss; report every increase.
	if droppedDelta != 0 {
		vlog.Warn("event", "msg", "queue overflow",
			"dropped", dropped,
			"delta", droppedDelta)
		s.world.Resources.Status.Trigger(status.TrigDrop)
	}

	// Status snapshot: world lock released and every stat above committed,
	// so the reading belongs to exactly this tick. Lock-free by construction.
	s.world.Resources.Status.Tick(ticks)

	// Anchor cadence: a rotated journal file must be interpretable on its own
	if event.AnchorDue(ticks) {
		s.world.Resources.Event.Queue.AnchorJournal(event.AnchorLive{
			Speed:         s.ctl.Scale().String(),
			Session:       s.world.Resources.Rand.Session(),
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
