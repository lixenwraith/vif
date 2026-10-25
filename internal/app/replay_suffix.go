package app

import (
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/snapshot"
	link "github.com/lixenwraith/vif/pkg/network"
)

// replaySource is the seam the barrier offers the correction path. An interface
// rather than a concrete type for the same reason AdoptSnapshot is: the system set
// is assembled from the manifest, and a run without a network system has nothing
// to project rather than being broken.
type replaySource interface {
	LocalReplaySuffix(fence uint64) ([]event.ScheduledWireFrame, []event.Origin, bool)
	ReplaySuffixSize() (int, int64)
	RetainedAfter(tick uint64, fences link.CrossingFences) ([]event.ScheduledWireFrame, []uint32, []event.Origin)
}

// replaySink is the projection world's half of the same seam.
type replaySink interface {
	ScheduleReplay([]event.ScheduledWireFrame, []uint32, []event.Origin)
}

// feedProjectionLocked schedules on the projection world what this instance applied
// after the capture and the capture lacks: its own crossings past the authority's fence
// for its source and the agreed artifacts due after its tick, each at its own tick, so
// the projection is the world this instance would have reached (multi-player.md §3.3);
// a suffix missing a record is unavailable, not shorter. Caller MUST hold updateMutex.
func (c *corrector) feedProjectionLocked(staging *App, header snapshot.CaptureHeader) {
	src, local := c.replaySourceLocked()
	if src == nil {
		return // no session barrier: nothing was ever retained
	}
	sink, _ := staging.glue.replaySource()
	dst, ok := sink.(replaySink)
	if !ok {
		return
	}
	tick := header.Tick
	// A capture that names no fence for this source claims nothing about its
	// stream, so the whole retained suffix is fed — the conservative direction,
	// because a duplicate is repaired by the next correction and a discarded
	// action is not.
	fence := header.Crossings.Seq(link.PeerID(local))
	frames, origins, available := src.LocalReplaySuffix(fence)
	retained, dropped := src.ReplaySuffixSize()

	m := c.telemetry
	m.ReplaySuffix.Store(int64(retained))
	m.ReplayOverflow.Store(dropped)
	m.ReplayUnusable.Store(!available)
	if !available {
		m.ReplaySkipped.Add(1)
		c.log.Warn("converge", "msg", "local replay skipped",
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
		c.log.Debug("converge", "msg", "local crossings projected",
			"tick", tick, "records", len(frames), "agreed", len(agreed), "retained", retained)
	}
}

// replaySourceLocked finds the barrier that retains the suffix, and this instance's
// own participant identity. Caller MUST hold updateMutex.
func (c *corrector) replaySourceLocked() (replaySource, uint32) {
	local := c.world.LocalParticipant()
	for _, sys := range c.world.Systems() {
		if r, ok := sys.(replaySource); ok {
			return r, local
		}
	}
	return nil, local
}

// replaySource is replaySourceLocked for a caller that does not hold the lock.
func (c *corrector) replaySource() (out replaySource, local uint32) {
	c.world.RunSafe(func() { out, local = c.replaySourceLocked() })
	return out, local
}
