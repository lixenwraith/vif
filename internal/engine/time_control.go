package engine

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/status"
)

// Clock is the time backend behind TimeControl. PausableClock drives interactive
// runs, ManualClock drives headless and replay. Not reached outside the engine:
// every consumer goes through TimeControl.
type Clock interface {
	Now() time.Time
	RealTime() time.Time
	Pause()
	Resume()
	Scale() TimeScale
	SetScale(TimeScale)
	ToReal(time.Duration) time.Duration
	Step(time.Duration)
}

var _ Clock = (*PausableClock)(nil)

// StepMode selects what disarms a run-until request
type StepMode uint8

const (
	StepNone  StepMode = iota
	StepFSM            // next FSM region transition
	StepEvent          // next dispatch of a named event
)

// BreakState is an armed run-until request; immutable once published
type BreakState struct {
	Mode    StepMode
	Region  string          // StepFSM: "" matches any region
	Event   event.EventType // StepEvent: the awaited type
	Restore TimeScale       // rate reinstated when it trips
	Pause   bool            // pause the world when it trips
	Expiry  uint64          // game tick after which the request self-disarms
	Label   string          // operator-facing description
}

// TimeControl is the single time surface: reads, rate, pause, step and
// run-until policy, telemetry, and the wake signal that preempts a sleep armed
// at the previous rate. Owned by GameContext.
type TimeControl struct {
	clock Clock
	wake  chan struct{}

	paused atomic.Bool                // pause authority; the clock mirrors it
	brk    atomic.Pointer[BreakState] // armed run-until request, nil when disarmed
	budget atomic.Int64               // remaining stepped ticks while paused

	statPct    *atomic.Int64
	statScale  *status.AtomicString
	statStep   *atomic.Int64
	statBreak  *status.AtomicString
	statPaused *atomic.Bool
}

// NewTimeControl binds a clock to the status registry; call before Freeze
func NewTimeControl(clock Clock, reg *status.Registry) *TimeControl {
	tc := &TimeControl{
		clock:      clock,
		wake:       make(chan struct{}, 1),
		statPct:    reg.Ints.Get("engine.speed_pct"),
		statScale:  reg.Strings.Get("engine.speed"),
		statStep:   reg.Ints.Get("engine.step"),
		statBreak:  reg.Strings.Get("engine.breakpoint"),
		statPaused: reg.Bools.Get("engine.paused"),
	}
	tc.statBreak.Store("-")
	tc.publish()
	return tc
}

// Now returns the current game instant
func (tc *TimeControl) Now() time.Time { return tc.clock.Now() }

// RealTime returns wall-clock time; a manual clock returns its virtual instant
func (tc *TimeControl) RealTime() time.Time { return tc.clock.RealTime() }

// ToReal converts a game duration to the wall interval it occupies at this rate
func (tc *TimeControl) ToReal(d time.Duration) time.Duration { return tc.clock.ToReal(d) }

// Step advances frozen game time; the paused step path
func (tc *TimeControl) Step(d time.Duration) { tc.clock.Step(d) }

// SetPaused freezes or restarts game time and reports whether the state changed.
// Sole pause authority: input, render and the scheduler all read IsPaused.
func (tc *TimeControl) SetPaused(paused bool) bool {
	if !tc.paused.CompareAndSwap(!paused, paused) {
		return false
	}
	if paused {
		tc.clock.Pause()
	} else {
		tc.clock.Resume()
	}
	tc.statPaused.Store(paused)
	tc.signal() // a sleep armed at the old rate must be recomputed
	return true
}

// IsPaused reports whether game time is frozen
func (tc *TimeControl) IsPaused() bool { return tc.paused.Load() }

// Wake fires when the rate changes and an armed sleep must be recomputed
func (tc *TimeControl) Wake() <-chan struct{} { return tc.wake }

// SetScale applies a rate; an explicit change overrides any pending request
func (tc *TimeControl) SetScale(s TimeScale) {
	if !s.Valid() {
		return
	}
	tc.disarm()
	tc.applyScale(s)
}

// applyScale changes the rate, publishes it, and wakes the scheduler
func (tc *TimeControl) applyScale(s TimeScale) {
	tc.clock.SetScale(s)
	tc.publish()
	tc.signal()
}

// StepTicks grants a paused tick allowance and wakes the scheduler
func (tc *TimeControl) StepTicks(n int64) {
	tc.disarm()
	if n < 1 {
		n = 1
	}
	tc.budget.Store(n)
	tc.statStep.Store(n)
	tc.signal()
}

// TakeStep consumes one tick from the paused allowance
func (tc *TimeControl) TakeStep() bool {
	for {
		n := tc.budget.Load()
		if n <= 0 {
			return false
		}
		if tc.budget.CompareAndSwap(n, n-1) {
			tc.statStep.Store(n - 1)
			return true
		}
	}
}

// Arm installs a run-until request and switches to its run rate
func (tc *TimeControl) Arm(bs *BreakState, run TimeScale) {
	tc.disarm()
	tc.brk.Store(bs)
	tc.statBreak.StoreIfChanged(bs.Label)
	tc.applyScale(run)
}

// Armed returns the active run-until request, nil when disarmed
func (tc *TimeControl) Armed() *BreakState { return tc.brk.Load() }

// Trip disarms and returns the request when the condition matches, restoring
// its rate. The caller applies the terminal action; nil means no match.
func (tc *TimeControl) Trip(mode StepMode, region string, et event.EventType) *BreakState {
	bs := tc.brk.Load()
	if bs == nil || bs.Mode != mode {
		return nil
	}
	if mode == StepFSM && bs.Region != "" && bs.Region != region {
		return nil
	}
	if mode == StepEvent && bs.Event != et {
		return nil
	}
	return tc.take(bs)
}

// Expire disarms a request whose tick deadline has passed, restoring its rate
func (tc *TimeControl) Expire(tick uint64) *BreakState {
	bs := tc.brk.Load()
	if bs == nil || bs.Expiry == 0 || tick < bs.Expiry {
		return nil
	}
	return tc.take(bs)
}

// take claims a request exactly once and reinstates its rate
func (tc *TimeControl) take(bs *BreakState) *BreakState {
	if !tc.brk.CompareAndSwap(bs, nil) {
		return nil
	}
	tc.statBreak.StoreIfChanged("-")
	tc.applyScale(bs.Restore)
	return bs
}

// Disarm clears any pending step allowance or run-until request
func (tc *TimeControl) Disarm() { tc.disarm() }

func (tc *TimeControl) disarm() {
	tc.budget.Store(0)
	tc.statStep.Store(0)
	if tc.brk.Swap(nil) != nil {
		tc.statBreak.StoreIfChanged("-")
	}
}

// CancelBreak drops a pending step allowance and disarms any run-until request,
// reinstating the rate it was armed from. Reset uses this because the FSM region
// or event stream the request named no longer exists after a restart.
func (tc *TimeControl) CancelBreak() {
	tc.budget.Store(0)
	tc.statStep.Store(0)
	if bs := tc.brk.Load(); bs != nil {
		tc.take(bs)
	}
}

// Scale returns the active rate
func (tc *TimeControl) Scale() TimeScale { return tc.clock.Scale() }

// signal is a non-blocking wake; a pending signal absorbs repeats
func (tc *TimeControl) signal() {
	select {
	case tc.wake <- struct{}{}:
	default:
	}
}

// publish mirrors the rate into the status registry
func (tc *TimeControl) publish() {
	s := tc.clock.Scale()
	tc.statPct.Store(s.Percent())
	tc.statScale.StoreIfChanged(s.String())
}
