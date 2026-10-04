package engine

import "time"

// SimEpoch is the origin every instance measures simulation time from: a constant,
// not a per-process time.Now, so participants and a replay read one instant per tick.
// Shared readers difference against stored instants (quasar speed step, gold timer,
// adaptation and genotype ages); against a wall-paced clock those cross their
// thresholds on different ticks per instance and never re-converge.
var SimEpoch = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// SimTime is the simulation instant of one tick: a pure function of the tick
// number and the fixed tick interval, identical on every instance and every
// reproduction. It is the only value a system may treat as "now".
//
// This is also what makes DeltaTime honest. A tick has always advanced the
// simulation by exactly tickInterval; only the instant stamped beside it drifted
// with the wall clock. Deriving both from the tick puts them back in agreement.
func SimTime(tick uint64, tickInterval time.Duration) time.Time {
	return SimEpoch.Add(time.Duration(tick) * tickInterval)
}
