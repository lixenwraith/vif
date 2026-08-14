package engine

import (
	"sync/atomic"
	"time"

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

// TimeControl is the operator-facing time surface shared by the command path
// and the scheduler: rate changes, telemetry, and the wake signal that
// preempts a sleep armed at the previous rate. Owned by GameContext.
type TimeControl struct {
	clock *PausableClock
	wake  chan struct{}

	statPct   *atomic.Int64
	statScale *status.AtomicString
}

// NewTimeControl binds a clock to the status registry; call before Freeze
func NewTimeControl(clock *PausableClock, reg *status.Registry) *TimeControl {
	tc := &TimeControl{
		clock:     clock,
		wake:      make(chan struct{}, 1),
		statPct:   reg.Ints.Get("engine.speed_pct"),
		statScale: reg.Strings.Get("engine.speed"),
	}
	tc.publish()
	return tc
}

// Clock returns the bound time source
func (tc *TimeControl) Clock() Clock { return tc.clock }

// Wake fires when the rate changes and an armed sleep must be recomputed
func (tc *TimeControl) Wake() <-chan struct{} { return tc.wake }

// SetScale applies a rate, publishes it, and wakes the scheduler.
// MetaSystem is the sole caller on the game path, mirroring pause ownership.
func (tc *TimeControl) SetScale(s TimeScale) {
	if !s.Valid() {
		return
	}
	tc.clock.SetScale(s)
	tc.publish()
	tc.signal()
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
