package engine

import (
	"sync/atomic"
	"time"
)

// ManualEpoch is the fixed origin of manual time. A constant rather than time.Now
// so a replay produces identical timestamps across runs and machines.
var ManualEpoch = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// ManualClock is a discrete time source advanced only by Step: game time is a pure
// function of accumulated steps, with no wall clock input at all. Drives headless
// runs, replays and tests. Scale is recorded for telemetry but never applied, since
// pacing belongs to whoever calls Step.
type ManualClock struct {
	elapsed atomic.Int64 // nanoseconds advanced by Step
	num     atomic.Int64
	den     atomic.Int64
}

var _ Clock = (*ManualClock)(nil)

// NewManualClock creates a clock sitting at ManualEpoch
func NewManualClock() *ManualClock {
	mc := &ManualClock{}
	mc.num.Store(ScaleNormal.Num)
	mc.den.Store(ScaleNormal.Den)
	return mc
}

// Now returns epoch plus everything Step has advanced
func (mc *ManualClock) Now() time.Time {
	return ManualEpoch.Add(time.Duration(mc.elapsed.Load()))
}

// RealTime returns the same virtual instant as Now. A manual run has no wall clock:
// admitting one would let scheduling jitter reach the simulation.
func (mc *ManualClock) RealTime() time.Time { return mc.Now() }

// Step advances game time; the only way a manual clock moves
func (mc *ManualClock) Step(d time.Duration) {
	if d > 0 {
		mc.elapsed.Add(int64(d))
	}
}

// ToReal returns 0: no wall interval corresponds to manual time, which the
// scheduler reads as "do not sleep"
func (mc *ManualClock) ToReal(time.Duration) time.Duration { return 0 }

// Scale returns the recorded rate
func (mc *ManualClock) Scale() TimeScale {
	return TimeScale{Num: mc.num.Load(), Den: mc.den.Load()}
}

// SetScale records a rate for telemetry; manual pacing ignores it
func (mc *ManualClock) SetScale(s TimeScale) {
	if !s.Valid() {
		return
	}
	mc.num.Store(s.Num)
	mc.den.Store(s.Den)
}

// Elapsed returns total game time advanced since epoch
func (mc *ManualClock) Elapsed() time.Duration { return time.Duration(mc.elapsed.Load()) }

// Pause is a no-op: manual time advances only on Step, so TimeControl's flag is
// the whole of the pause state
func (mc *ManualClock) Pause() {}

// Resume is a no-op; manual time has no wall clock to resume against
func (mc *ManualClock) Resume() {}
