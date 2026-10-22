package engine

import (
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/status"
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

// BreakKind selects what a standing breakpoint watches
type BreakKind uint8

const (
	BreakOnEvent  BreakKind = iota // a dispatch of the named event
	BreakOnRegion                  // the named region spawning
	BreakOnState                   // any region entering the named state
	BreakOnStat                    // a status cell's comparison becoming true
)

// BreakKindNames is the command word for each kind
var BreakKindNames = [...]string{"event", "region", "state", "stat"}

// Breakpoint is one standing condition: it trips each time it matches until it
// is cleared, across resets. Region and state names match without case.
type Breakpoint struct {
	Kind  BreakKind
	Name  string          // event, region or state name, or stat key
	Event event.EventType // BreakOnEvent
	Label string          // the condition as the operator reads it back
	Test  func() bool     // BreakOnStat: the comparison over the live cell
	held  atomic.Bool     // BreakOnStat: whether Test held at the last reading
}

// Arm records whether the comparison holds now, so a condition already met
// trips the next time it becomes true rather than at once.
func (bp *Breakpoint) Arm() *Breakpoint {
	if bp.Test != nil {
		bp.held.Store(bp.Test())
	}
	return bp
}

// BreakSet is the standing breakpoints, replaced whole on every change
type BreakSet struct {
	List   []*Breakpoint
	events map[event.EventType]*Breakpoint
	stats  []*Breakpoint
}

func newBreakSet(list []*Breakpoint) *BreakSet {
	if len(list) == 0 {
		return nil
	}
	bs := &BreakSet{List: list, events: map[event.EventType]*Breakpoint{}}
	for _, bp := range list {
		switch bp.Kind {
		case BreakOnEvent:
			bs.events[bp.Event] = bp
		case BreakOnStat:
			bs.stats = append(bs.stats, bp)
		}
	}
	return bs
}

// OnEvent is the breakpoint a dispatch of et trips; nil when none
func (bs *BreakSet) OnEvent(et event.EventType) *Breakpoint {
	if bs == nil || len(bs.events) == 0 {
		return nil
	}
	return bs.events[et]
}

// Named is the region or state breakpoint name trips; nil when none
func (bs *BreakSet) Named(kind BreakKind, name string) *Breakpoint {
	if bs == nil {
		return nil
	}
	for _, bp := range bs.List {
		if bp.Kind == kind && strings.EqualFold(bp.Name, name) {
			return bp
		}
	}
	return nil
}

// RisenStats is the stat breakpoints whose comparison became true since the
// last reading. Read once per completed tick, by one goroutine.
func (bs *BreakSet) RisenStats() []*Breakpoint {
	if bs == nil {
		return nil
	}
	var out []*Breakpoint
	for _, bp := range bs.stats {
		now := bp.Test()
		if now && !bp.held.Load() {
			out = append(out, bp)
		}
		bp.held.Store(now)
	}
	return out
}

// TimeControl is the single time surface: reads, rate, pause, step and
// run-until policy, standing breakpoints, telemetry, and the wake signal that
// preempts a sleep armed at the previous rate. Owned by GameContext.
type TimeControl struct {
	clock Clock
	wake  chan struct{}

	// The two reasons the clock stands still; it runs only while neither holds.
	// stopMu orders their changes, so the clock follows the pair rather than
	// whichever writer came last.
	stopMu sync.Mutex
	paused atomic.Bool                // this instance's pause: input, away, a breakpoint
	held   atomic.Bool                // a hold, which only :pause or the host's resume ends
	frozen atomic.Bool                // the hold standing is a freeze; see Freeze
	brk    atomic.Pointer[BreakState] // armed run-until request, nil when disarmed
	budget atomic.Int64               // remaining stepped ticks while paused
	breaks atomic.Pointer[BreakSet]   // standing breakpoints, nil when none
	hit    atomic.Bool                // a breakpoint paused the run; the command line opens

	statPct    *atomic.Int64
	statScale  *status.AtomicString
	statStep   *atomic.Int64
	statBreak  *status.AtomicString
	statPaused *atomic.Bool
}

// NewTimeControl binds a clock to the status registry; call before Registry.Freeze
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

// SetPaused freezes or restarts game time for this instance's own reasons and
// reports whether that changed. Sole pause authority: input, render and the
// scheduler all read IsPaused.
func (tc *TimeControl) SetPaused(paused bool) bool { return tc.stop(&tc.paused, paused) }

// SetHeld starts or ends a hold and reports whether that changed. A hold stops the
// clock as a pause does, but ending a pause leaves it standing, and no step passes
// it: in a session every participant stands at the tick it applied in. Either way,
// the hold is no longer a freeze.
func (tc *TimeControl) SetHeld(held bool) bool {
	tc.stopMu.Lock()
	defer tc.stopMu.Unlock()
	tc.frozen.Store(false)
	return tc.stopLocked(&tc.held, held)
}

// Freeze holds the clock while the session's host changes and reports whether that
// stopped it; a hold already standing stays the hold it was. No host set a freeze,
// so the one taking the session over ends it, where a host's hold waits for :pause.
func (tc *TimeControl) Freeze() bool {
	tc.stopMu.Lock()
	defer tc.stopMu.Unlock()
	if !tc.stopLocked(&tc.held, true) {
		return false
	}
	tc.frozen.Store(true)
	return true
}

// stop sets one reason the clock stands still and moves the clock when the pair's
// answer changes.
func (tc *TimeControl) stop(reason *atomic.Bool, on bool) bool {
	tc.stopMu.Lock()
	defer tc.stopMu.Unlock()
	return tc.stopLocked(reason, on)
}

// stopLocked is stop for a caller holding stopMu.
func (tc *TimeControl) stopLocked(reason *atomic.Bool, on bool) bool {
	was := tc.IsPaused()
	if !reason.CompareAndSwap(!on, on) {
		return false
	}
	if now := tc.IsPaused(); now != was {
		if now {
			tc.clock.Pause()
		} else {
			tc.clock.Resume()
		}
		tc.statPaused.Store(now)
		tc.signal() // a sleep armed at the old rate must be recomputed
	}
	return true
}

// Clock returns the clock time control drives.
func (tc *TimeControl) Clock() Clock { return tc.clock }

// IsPaused reports whether game time is frozen, by a pause or by a hold
func (tc *TimeControl) IsPaused() bool { return tc.paused.Load() || tc.held.Load() }

// Held reports whether a hold stands
func (tc *TimeControl) Held() bool { return tc.held.Load() }

// Frozen reports whether the hold standing is a freeze rather than a host's
func (tc *TimeControl) Frozen() bool { return tc.frozen.Load() }

// OwnPause reports this instance's own pause, a hold aside
func (tc *TimeControl) OwnPause() bool { return tc.paused.Load() }

// Wake fires when the rate changes and an armed sleep must be recomputed
func (tc *TimeControl) Wake() <-chan struct{} { return tc.wake }

// SetScale applies a rate; an explicit change overrides any pending request
func (tc *TimeControl) SetScale(s TimeScale) {
	if !s.Valid() {
		return
	}
	tc.disarm(false)
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
	tc.disarm(true)
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
	tc.disarm(true)
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

// disarm clears any armed break and returns it, reinstating the rate it was
// armed from unless the caller is setting a rate itself.
func (tc *TimeControl) disarm(restore bool) *BreakState {
	bs := tc.brk.Swap(nil)
	if bs == nil {
		return nil
	}
	if restore {
		tc.applyScale(bs.Restore)
	}
	tc.statBreak.StoreIfChanged("-")
	return bs
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

// SetBreak adds a standing breakpoint, replacing one with the same label
func (tc *TimeControl) SetBreak(bp *Breakpoint) {
	tc.editBreaks(func(list []*Breakpoint) []*Breakpoint {
		list = slices.DeleteFunc(list, func(o *Breakpoint) bool { return o.Label == bp.Label })
		return append(list, bp.Arm())
	})
}

// ClearBreaks removes the breakpoints match accepts, every one for a nil match,
// and reports how many went
func (tc *TimeControl) ClearBreaks(match func(*Breakpoint) bool) int {
	n := 0
	tc.editBreaks(func(list []*Breakpoint) []*Breakpoint {
		before := len(list)
		list = slices.DeleteFunc(list, func(bp *Breakpoint) bool { return match == nil || match(bp) })
		n = before - len(list)
		return list
	})
	return n
}

// editBreaks replaces the standing set with edit's rewrite of a copy of it
func (tc *TimeControl) editBreaks(edit func([]*Breakpoint) []*Breakpoint) {
	for {
		old := tc.breaks.Load()
		var list []*Breakpoint
		if old != nil {
			list = slices.Clone(old.List)
		}
		if tc.breaks.CompareAndSwap(old, newBreakSet(edit(list))) {
			return
		}
	}
}

// Breaks returns the standing breakpoints; nil when there are none
func (tc *TimeControl) Breaks() *BreakSet { return tc.breaks.Load() }

// MarkHit records that a breakpoint paused the run
func (tc *TimeControl) MarkHit() { tc.hit.Store(true) }

// TakeHit reports, once per hit, that a breakpoint paused the run
func (tc *TimeControl) TakeHit() bool { return tc.hit.Swap(false) }

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
