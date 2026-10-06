package app

import (
	"fmt"
	"slices"
	"time"

	"github.com/lixenwraith/vif/internal/content"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/service"
	"github.com/lixenwraith/vif/internal/snapshot"
)

// checkpoint is a replay copy's whole state where its driver stood: what a fresh
// copy restores to continue from there instead of replaying the stream before it.
type checkpoint struct {
	steps     int                 // the driver steps it was read after
	at        event.Stamp         // where the copy stood
	digest    event.JournalDigest // its world, which a restore must reproduce
	world     *engine.WorldCopy
	queue     event.QueueCopy
	scheduler engine.SchedulerCopy
	systems   []snapshot.SystemStateRecord
	local     map[string]any
	toggles   map[string]bool
	status    snapshot.StatusState
	content   content.CursorState
	clock     time.Duration
	paused    bool
	scale     engine.TimeScale
	width     int
	height    int
	driver    journal.Cursor
}

// checkpointLocked reads a replay copy and where its driver stands. Caller MUST
// hold updateMutex.
func (a *App) checkpointLocked(d *journal.ReplayDriver) (*checkpoint, error) {
	systems, err := a.captureSystemStatesLocked()
	if err != nil {
		return nil, err
	}
	local, toggles := map[string]any{}, map[string]bool{}
	for _, sys := range a.world.Systems() {
		if cp, ok := sys.(engine.StateCopier); ok {
			local[sys.Name()] = cp.CopyState()
		}
		if t, ok := sys.(engine.Toggled); ok {
			toggles[sys.Name()] = t.Enabled()
		}
	}
	// A copier carries the whole of its system; LoadShared would re-derive it as a joiner.
	systems = slices.DeleteFunc(systems, func(r snapshot.SystemStateRecord) bool {
		_, copied := local[r.System]
		return copied
	})
	c := &checkpoint{
		world:     a.world.CopyOut(),
		queue:     a.world.Resources.Event.Queue.CopyOut(),
		scheduler: a.scheduler.CopyOut(),
		systems:   systems,
		status:    a.statusCellsLocked(a.statusKeysLocked(func(string) bool { return true })),
		paused:    a.ctx.TimeCtl.IsPaused(),
		scale:     a.ctx.TimeCtl.Scale(),
		width:     a.ctx.Width,
		height:    a.ctx.Height,
		local:     local,
		toggles:   toggles,
		content:   service.MustGet[*service.ContentService](a.hub, "content").CursorState(),
		driver:    d.Cursor(),
		at:        a.Position(),
		digest:    a.journalDigestLocked(),
	}
	if mc, ok := a.ctx.TimeCtl.Clock().(*engine.ManualClock); ok {
		c.clock = mc.Elapsed()
	}
	return c, nil
}

// restore settles a fresh replay copy's boot and places it where c was read. A copy
// that does not then stand where c's did, on the digested classes of its world, is
// refused: a checkpoint that carried the run short is caught before anything plays.
func (a *App) restore(c *checkpoint, d *journal.ReplayDriver) (err error) {
	a.Settle()
	a.world.RunSafe(func() {
		if err = a.restoreLocked(c, d); err != nil {
			return
		}
		if at, got := a.Position(), a.journalDigestLocked(); at != c.at || got != c.digest {
			err = fmt.Errorf("restore at step %d: stands at %+v with digest %+v, the checkpoint at %+v with %+v",
				c.steps, at, got, c.at, c.digest)
		}
	})
	return err
}

// restoreLocked places a fresh replay copy, built from the same journal, where c
// was read, and its driver with it. Caller MUST hold updateMutex.
func (a *App) restoreLocked(c *checkpoint, d *journal.ReplayDriver) error {
	a.world.CopyIn(c.world)
	if unknown := c.world.LoadStreams(a.world.Resources.Rand); len(unknown) > 0 {
		return fmt.Errorf("restore: RNG streams this copy does not issue: %v", unknown)
	}
	savers := a.sharedStateSaversLocked()
	for _, rec := range c.systems {
		if err := savers[rec.System].LoadShared(rec.Data); err != nil {
			return fmt.Errorf("restore %s: %w", rec.System, err)
		}
	}
	for _, sys := range a.world.Systems() {
		if cp, ok := sys.(engine.StateCopier); ok {
			if err := cp.RestoreState(c.local[sys.Name()]); err != nil {
				return fmt.Errorf("restore %s: %w", sys.Name(), err)
			}
		}
	}
	if err := a.scheduler.CopyIn(c.scheduler); err != nil {
		return err
	}
	if c.scheduler.ResetPending() {
		select {
		case a.ctx.ResetChan <- struct{}{}:
		default:
		}
	}
	// After the FSM import, whose re-derived toggles are queued and dropped here
	a.world.Resources.Event.Queue.CopyIn(c.queue)
	for _, sys := range a.world.Systems() {
		if t, ok := sys.(engine.Toggled); ok {
			t.SetEnabled(c.toggles[sys.Name()])
		}
	}
	if mc, ok := a.ctx.TimeCtl.Clock().(*engine.ManualClock); ok {
		mc.SetElapsed(c.clock)
	}
	a.ctx.TimeCtl.SetPaused(c.paused)
	a.ctx.TimeCtl.SetScale(c.scale)
	a.ctx.Width, a.ctx.Height = c.width, c.height
	service.MustGet[*service.ContentService](a.hub, "content").SetCursorState(c.content)
	a.installStatusLocked(c.status)
	d.Resume(c.driver)
	return nil
}
