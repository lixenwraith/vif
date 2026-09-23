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
// to project rather than being broken.
type replaySource interface {
	LocalReplaySuffix(fence uint64) ([]event.ScheduledWireFrame, []event.Origin, bool)
	ReplaySuffixSize() (int, int64)
	RetainedAfter(tick uint64, fences network.CrossingFences) ([]event.ScheduledWireFrame, []uint32, []event.Origin)
}

// replaySink is the projection world's half of the same seam.
type replaySink interface {
	ScheduleReplay([]event.ScheduledWireFrame, []uint32, []event.Origin)
}

// feedProjectionLocked puts on the projection world's schedule everything this instance
// applied after the capture that the capture does not contain: its own crossings
// past the authority's fence for its source, and the agreed artifacts due after the
// capture's tick. Each applies at its own tick as the projection simulates forward,
// which is what makes the projection the world this instance would have reached.
// Retention is bounded, and a suffix missing a record is unavailable rather than
// shorter — a shorter suffix is a different history. See doc/multi-player.md §3.3.
// Caller MUST hold the live world's updateMutex.
func (a *App) feedProjectionLocked(staging *App, header snapshot.CaptureHeader) {
	src, local := a.replaySourceLocked()
	if src == nil {
		return // no session barrier: nothing was ever retained
	}
	sink, _ := staging.replaySource()
	dst, ok := sink.(replaySink)
	if !ok {
		return
	}
	tick := header.Tick
	// A capture that names no fence for this source claims nothing about its
	// stream, so the whole retained suffix is fed — the conservative direction,
	// because a duplicate is repaired by the next correction and a discarded
	// action is not.
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
		frames, origins = nil, nil
	}
	sources := make([]uint32, len(frames))
	for i := range sources {
		sources[i] = local
	}
	agreed, agreedSources, agreedOrigins := src.RetainedAfter(tick, header.Crossings)
	dst.ScheduleReplay(append(frames, agreed...), append(sources, agreedSources...),
		append(origins, agreedOrigins...))
	m.ReplayReplayed.Add(int64(len(frames)))
	if len(frames)+len(agreed) > 0 {
		vlog.Debug("app", "msg", "local crossings projected",
			"tick", tick, "records", len(frames), "agreed", len(agreed), "retained", retained)
	}
}

// replaySourceLocked finds the barrier that retains the suffix, and this instance's
// own participant identity. Caller MUST hold updateMutex.
func (a *App) replaySourceLocked() (replaySource, uint32) {
	local := a.world.LocalParticipant()
	for _, sys := range a.world.Systems() {
		if r, ok := sys.(replaySource); ok {
			return r, local
		}
	}
	return nil, local
}

// replaySource is replaySourceLocked for a caller that does not hold the lock.
func (a *App) replaySource() (out replaySource, local uint32) {
	a.world.RunSafe(func() { out, local = a.replaySourceLocked() })
	return out, local
}
