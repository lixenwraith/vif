// Package app: putting this participant's own actions back after the authority
// moves it.
//
// A correction describes the host's world at tick T. A guest applying one is
// standing somewhere past T — it has been predicting — and what it produced in
// between is real: the keystrokes that typed a gold sequence, the shots that were
// fired, the cursor motion a player watched happen. Discarding that suffix lets a
// correction undo a fast sequence and the receive schedule re-do it later.
//
// This is the repair. Three rules make it exact rather than approximate:
//
//   - **One canonical suffix.** What is retained is the artifact the transport
//     already encoded — event.ScheduledWireFrame, the same value the host will
//     apply and the same payload text the journal writes — so a replay cannot
//     differ from what the session was told happened. The retention itself lives
//     in NetworkSystem beside the barrier, because the barrier is what decides an
//     artifact's apply tick and the apply tick is what decides whether the
//     correction already contains it.
//
//   - **One local-suffix membership test.** A guest replays its own artifacts
//     whose agreed apply tick is after the installed world's tick. Authority-local
//     artifacts are the asymmetric case: the host applied them immediately, so
//     AdoptSnapshot and scheduleCrossings classify those by the capture header's
//     completed authority sequence instead.
//
//   - **No partial answer.** Retention is bounded by tick span, record count and
//     bytes, and dropping a record the suffix would need makes the suffix
//     unavailable rather than shorter. A guest then keeps the authority alone and
//     says so. A shorter suffix is not a smaller version of this participant's
//     history; it is a different one.
//
// What is deliberately not replayed: anything a peer produced (it is the host's to
// order, and it reaches this instance through the barrier), anything a shared
// system re-derives (D-5 — it would apply twice), and the three barrier-bound
// artifacts that decide what the world is rather than what happens in it — an
// arrival, a departure and a reset. Those are never retained in the first place.
package app

import (
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// replaySource is the seam the barrier offers the correction path. It is an
// interface rather than a concrete type for the same reason AdoptSnapshot is: the
// system set is assembled from the manifest, and a run without a network system is
// a run with nothing to replay rather than a broken one.
type replaySource interface {
	LocalReplaySuffix(tick uint64) ([]event.ScheduledWireFrame, []event.Origin, bool)
	ReplaySuffixSize() (int, int64)
}

// replayLocalSuffix re-applies this instance's own accepted crossings that the
// correction it just installed does not contain.
//
// It runs after the commit and between two ticks, which is where the install left
// the world: the shared state is the authority's as of tick T, and these are the
// artifacts the session has agreed will apply after T. Pushing them here is the
// same publication the producing tick made, in the same order, so what the player
// sees is their own actions surviving a correction rather than being rolled back
// and re-delivered a playout lead later.
//
// The origin is the artifact's own rather than OriginNetwork. It is this
// participant's action either way, and the journal records what the run did — but
// the queue must not cross it a second time, which it will not: the crossing was
// already flushed in the epoch that produced it, and the barrier is rebased past
// that epoch by AdoptSnapshot before this runs.
func (a *App) replayLocalSuffix(tick uint64) (replayed int, ok bool) {
	src := a.replaySourceLocked()
	if src == nil {
		return 0, true // no session barrier: nothing was ever retained
	}
	frames, origins, available := src.LocalReplaySuffix(tick)
	retained, dropped := src.ReplaySuffixSize()

	m := a.snapshotTelemetry
	m.replaySuffix.Store(int64(retained))
	m.replayOverflow.Store(dropped)
	m.replayUnusable.Store(!available)
	if !available {
		m.replaySkipped.Add(1)
		vlog.Warn("app", "msg", "local replay skipped",
			"tick", tick, "retained", retained, "dropped", dropped)
		return 0, false
	}
	if len(frames) == 0 {
		return 0, true
	}

	pushed := 0
	a.world.RunSafe(func() {
		queue := a.world.Resources.Event.Queue
		for i, f := range frames {
			et, payload, domain, err := f.Frame.Decode()
			if err != nil {
				// A frame this build cannot decode is one it should never have
				// encoded. Counting it and going on would replay a hole; the whole
				// suffix is refused instead, on the same "never guess" rule.
				vlog.Warn("app", "msg", "local replay frame refused", "error", err.Error())
				pushed = -1
				return
			}
			origin := event.OriginNetwork
			if i < len(origins) {
				origin = origins[i]
			}
			queue.PushReady(event.GameEvent{
				Type: et, Payload: payload, Origin: origin, Domain: domain,
			})
			pushed++
		}
	})
	if pushed < 0 {
		m.replaySkipped.Add(1)
		m.replayUnusable.Store(true)
		return 0, false
	}
	a.scheduler.Settle()
	m.replayReplayed.Add(int64(pushed))
	vlog.Debug("app", "msg", "local crossings replayed",
		"tick", tick, "records", pushed, "retained", retained)
	return pushed, true
}

// replaySourceLocked finds the barrier that retains the suffix.
func (a *App) replaySourceLocked() replaySource {
	var out replaySource
	a.world.RunSafe(func() {
		for _, sys := range a.world.Systems() {
			if r, ok := sys.(replaySource); ok {
				out = r
				return
			}
		}
	})
	return out
}
