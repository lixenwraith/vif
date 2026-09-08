// Replaying this participant's own actions after a correction.
//
// A correction describes the host's world at tick T. A guest applying one stands
// past T — it has been predicting — and what it produced in between is real:
// keystrokes that typed a gold sequence, shots fired, cursor motion the player
// watched. Discarding that suffix lets a correction undo a fast sequence and the
// receive schedule re-do it later.
//
// Three rules make the repair exact rather than approximate:
//
//   - One canonical suffix. What is retained is the artifact the transport already
//     encoded: event.ScheduledWireFrame, the same value the host will apply and the
//     same payload text the journal writes. Retention lives in NetworkSystem beside
//     the barrier, because the barrier decides an artifact's apply tick and the apply
//     tick decides whether the correction already contains it.
//
//   - One membership test, and it is a sequence rather than a tick. A capture
//     describes a world; what that world holds of this participant's stream is the
//     sequence its fence names, because a producer applies its own crossing
//     immediately and everyone else waits for the agreed tick. A copy whose apply
//     tick is already past can therefore be missing from a capture taken before it
//     arrived, and judging by tick discarded exactly those — which is how a guest's
//     own action came to disappear for one cadence whenever its link missed the
//     playout lead. AdoptSnapshot and scheduleCrossings apply the same fence to the
//     schedule, for every source rather than only the authority.
//
//   - No partial answer. Retention is bounded by tick span, record count and bytes,
//     and dropping a record the suffix would need makes the suffix unavailable rather
//     than shorter: a shorter suffix is a different history, not a smaller one.
//
// Not replayed: anything a peer produced, anything a shared system re-derives (D-5,
// it would apply twice), and the three barrier-bound artifacts that decide what the
// world is rather than what happens in it — arrival, departure and reset.

package app

import (
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// replaySource is the seam the barrier offers the correction path. An interface
// rather than a concrete type for the same reason AdoptSnapshot is: the system set
// is assembled from the manifest, and a run without a network system has nothing
// to replay rather than being broken.
type replaySource interface {
	LocalReplaySuffix(fence uint64) ([]event.ScheduledWireFrame, []event.Origin, bool)
	ReplaySuffixSize() (int, int64)
}

// replayLocalSuffix re-applies this instance's own accepted crossings that the
// correction it just installed does not contain.
//
// It runs after the commit and between two ticks, where the install left the
// world: shared state is the authority's as of tick T, and these are the artifacts
// the session agreed will apply after T. Pushing them here is the same publication
// the producing tick made, in the same order.
//
// The origin is the artifact's own rather than OriginNetwork: it is this
// participant's action either way, and the journal records what the run did. The
// queue does not cross it a second time — the crossing was flushed in the epoch
// that produced it, and AdoptSnapshot rebases the barrier past that epoch before
// this runs.
func (a *App) replayLocalSuffix(header snapshot.CaptureHeader) (replayed int, ok bool) {
	src, local := a.replaySource()
	if src == nil {
		return 0, true // no session barrier: nothing was ever retained
	}
	// This instance's own boundary in the world that was just installed. A capture
	// that names no fence for this source claims nothing about its stream, so the
	// whole retained suffix is replayed — the conservative direction, because a
	// duplicate is repaired by the next correction and a discarded action is not.
	tick := header.Tick
	fence := header.Crossings.Seq(network.PeerID(local))
	frames, origins, available := src.LocalReplaySuffix(fence)
	retained, dropped := src.ReplaySuffixSize()

	m := a.telemetry
	m.ReplaySuffix.Store(int64(retained))
	m.ReplayOverflow.Store(dropped)
	m.ReplayUnusable.Store(!available)
	if !available {
		m.ReplaySkipped.Add(1)
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
		m.ReplaySkipped.Add(1)
		m.ReplayUnusable.Store(true)
		return 0, false
	}
	a.scheduler.Settle()
	m.ReplayReplayed.Add(int64(pushed))
	vlog.Debug("app", "msg", "local crossings replayed",
		"tick", tick, "records", pushed, "retained", retained)
	return pushed, true
}

// replaySource finds the barrier that retains the suffix, and this instance's own
// participant identity, under one world lock: the caller needs both and reading them
// apart would let a departure land between them.
func (a *App) replaySource() (replaySource, uint32) {
	var (
		out   replaySource
		local uint32
	)
	a.world.RunSafe(func() {
		local = a.world.LocalParticipant()
		for _, sys := range a.world.Systems() {
			if r, ok := sys.(replaySource); ok {
				out = r
				return
			}
		}
	})
	return out, local
}
