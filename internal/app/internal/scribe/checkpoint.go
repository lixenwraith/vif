package scribe

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/lixenwraith/vif/internal/app/internal/corrector"
	"github.com/lixenwraith/vif/internal/content"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/service"
	"github.com/lixenwraith/vif/internal/snapshot"
)

// Checkpoint is a replay copy's whole state where its driver stood: what a fresh
// copy restores to continue from there instead of replaying the stream before it.
type Checkpoint struct {
	Steps     int                 // the driver steps it was read after
	at        event.Stamp         // where the copy stood
	Digest    event.JournalDigest // its world, which a restore must reproduce
	World     *engine.WorldCopy
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

// CheckpointLocked reads a replay copy and where its driver stands, into reuse's
// storage when one is given. Caller MUST hold updateMutex.
func (sc *Scribe) CheckpointLocked(d *journal.ReplayDriver, reuse *engine.WorldCopy) (*Checkpoint, error) {
	systems, err := corrector.CaptureSystemStatesLocked(sc.world)
	if err != nil {
		return nil, err
	}
	local, toggles := map[string]any{}, map[string]bool{}
	for _, sys := range sc.world.Systems() {
		switch s := sys.(type) {
		case engine.LocalStateSaver:
			data, err := s.SaveLocal()
			if err != nil {
				return nil, fmt.Errorf("checkpoint %s: %w", sys.Name(), err)
			}
			local[sys.Name()] = data
		case engine.StateCopier:
			local[sys.Name()] = s.CopyState()
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
	c := &Checkpoint{
		World:     sc.world.CopyOut(reuse),
		queue:     sc.world.Resources.Event.Queue.CopyOut(),
		scheduler: sc.scheduler.CopyOut(),
		systems:   systems,
		status:    corrector.StatusCellsLocked(sc.world, corrector.StatusKeysLocked(sc.world, func(string) bool { return true })),
		paused:    sc.ctx.TimeCtl.IsPaused(),
		scale:     sc.ctx.TimeCtl.Scale(),
		width:     sc.ctx.Width,
		height:    sc.ctx.Height,
		local:     local,
		toggles:   toggles,
		content:   service.MustGet[*service.ContentService](sc.hub, "content").CursorState(),
		driver:    d.Cursor(),
		at:        sc.run.Position(),
		Digest:    sc.JournalDigestLocked(),
	}
	if mc, ok := sc.ctx.TimeCtl.Clock().(*engine.ManualClock); ok {
		c.clock = mc.Elapsed()
	}
	return c, nil
}

// Restore settles a fresh replay copy's boot and places it where c was read. A copy
// that does not then stand where c's did, on the digested classes of its world, is
// refused: a checkpoint that carried the run short is caught before anything plays.
func (sc *Scribe) Restore(c *Checkpoint, d *journal.ReplayDriver) (err error) {
	sc.run.Settle()
	sc.world.RunSafe(func() {
		if err = sc.restoreLocked(c, d); err != nil {
			return
		}
		if at, got := sc.run.Position(), sc.JournalDigestLocked(); at != c.at || got != c.Digest {
			err = fmt.Errorf("restore at step %d: stands at %+v with digest %+v, the checkpoint at %+v with %+v",
				c.Steps, at, got, c.at, c.Digest)
		}
	})
	return err
}

// restoreLocked places a fresh replay copy, built from the same journal, where c
// was read, and its driver with it. Caller MUST hold updateMutex.
func (sc *Scribe) restoreLocked(c *Checkpoint, d *journal.ReplayDriver) error {
	sc.world.CopyIn(c.World)
	if unknown := c.World.LoadStreams(sc.world.Resources.Rand); len(unknown) > 0 {
		return fmt.Errorf("restore: RNG streams this copy does not issue: %v", unknown)
	}
	savers := corrector.SharedStateSaversLocked(sc.world)
	for _, rec := range c.systems {
		if err := savers[rec.System].LoadShared(rec.Data); err != nil {
			return fmt.Errorf("restore %s: %w", rec.System, err)
		}
	}
	for _, sys := range sc.world.Systems() {
		if err := loadLocalState(sys, c.local[sys.Name()]); err != nil {
			return fmt.Errorf("restore %s: %w", sys.Name(), err)
		}
	}
	if err := sc.scheduler.CopyIn(c.scheduler); err != nil {
		return err
	}
	if c.scheduler.ResetPending() {
		select {
		case sc.ctx.ResetChan <- struct{}{}:
		default:
		}
	}
	// After the FSM import, whose re-derived toggles are queued and dropped here
	sc.world.Resources.Event.Queue.CopyIn(c.queue)
	for _, sys := range sc.world.Systems() {
		if t, ok := sys.(engine.Toggled); ok {
			t.SetEnabled(c.toggles[sys.Name()])
		}
	}
	if mc, ok := sc.ctx.TimeCtl.Clock().(*engine.ManualClock); ok {
		mc.SetElapsed(c.clock)
	}
	sc.ctx.TimeCtl.SetPaused(c.paused)
	sc.ctx.TimeCtl.SetScale(c.scale)
	sc.ctx.Width, sc.ctx.Height = c.width, c.height
	if err := service.MustGet[*service.ContentService](sc.hub, "content").SetCursorState(c.content); err != nil {
		return err
	}
	corrector.InstallStatusLocked(sc.world, c.status)
	d.Resume(c.driver)
	return nil
}

// loadLocalState hands a system what a checkpoint carried of it whole: the bytes of
// a LocalStateSaver, the value of a StateCopier.
func loadLocalState(sys engine.System, v any) error {
	switch s := sys.(type) {
	case engine.LocalStateSaver:
		data, ok := v.([]byte)
		if !ok {
			return errors.New("no state carried")
		}
		return s.LoadLocal(data)
	case engine.StateCopier:
		if v == nil {
			return errors.New("no state carried")
		}
		return s.RestoreState(v)
	}
	return nil
}
