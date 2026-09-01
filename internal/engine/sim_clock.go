package engine

import "time"

// SimEpoch is the origin every instance measures simulation time from. A constant
// rather than a per-process time.Now, because game time is shared simulation state:
// two participants must read the same instant at the same tick, and a replay must
// read the same instant as the run it reproduces.
//
// Before this existed the simulation instant came from the pacing clock, so it
// carried each process's own start offset and its own scheduler jitter. Every
// shared reader takes a difference against a stored instant — a quasar's speed
// step, the gold timer, adaptation drain ages, genotype ages — and a difference
// against a wall-paced clock crosses its threshold on a different *tick* on each
// instance. The 2026-08-31 kinetics divergence was exactly that: one instance
// stepped a quasar's SpeedMultiplier a tick before the other, and the two velocity
// streams never re-converged.
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
