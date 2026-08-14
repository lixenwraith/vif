package engine

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/status"
)

// Clock is the scheduler's time source. PausableClock drives interactive runs;
// a manual implementation will drive headless and replay.
type Clock interface {
	Now() time.Time
	RealTime() time.Time
	IsPaused() bool
	Scale() TimeScale
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

// TimeControl is the operator-facing time surface shared by the command path
// and the scheduler: rate changes, telemetry, and the wake signal that
// preempts a sleep armed at the previous rate. Owned by GameContext.

type TimeControl struct {
	clock *PausableClock
	wake  chan struct{}

	brk    atomic.Pointer[BreakState] // armed run-until request, nil when disarmed
	budget atomic.Int64               // remaining stepped ticks while paused

	statPct   *atomic.Int64
	statScale *status.AtomicString
	statStep  *atomic.Int64
	statBreak *status.AtomicString
}

// NewTimeControl binds a clock to the status registry; call before Freeze
func NewTimeControl(clock *PausableClock, reg *status.Registry) *TimeControl {
	tc := &TimeControl{
		clock:     clock,
		wake:      make(chan struct{}, 1),
		statPct:   reg.Ints.Get("engine.speed_pct"),
		statScale: reg.Strings.Get("engine.speed"),
		statStep:  reg.Ints.Get("engine.step"),
		statBreak: reg.Strings.Get("engine.breakpoint"),
	}
	tc.statBreak.Store("-")
	tc.publish()
	return tc
}

// Clock returns the bound time source
func (tc *TimeControl) Clock() Clock { return tc.clock }

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
