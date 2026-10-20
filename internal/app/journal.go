package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/service"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/internal/vlog"
)

// journalAsk is a journal start or stop asked for from the command line, which the
// loop owning the run serves once the command has settled: an opening takes on a
// world wholesale, under which a command's own events still queued would be lost.
// report says how it went, on the status line of whoever asked.
type journalAsk struct {
	start  bool
	report func(path string, err error)
}

// ownedSystems are this instance's own I/O, which taking on another world's state
// leaves as they are: the transport and the speakers. Their status groups stay too.
var ownedSystems = []string{"network", "audio", "music"}

// ownedStatusKey reports whether a status key is this instance's own: a process,
// pacing or telemetry figure a replay never compares, or one of ownedSystems'.
func ownedStatusKey(key string) bool {
	if snapshot.SimDeniedKey(key) {
		return true
	}
	group, _, _ := strings.Cut(key, ".")
	return slices.Contains(ownedSystems, group)
}

// initJournal opens the replay journal a run starts with and installs it on the
// event queue: for the life of the run it records every event of a journaled origin
// (all but system and device) and every own crossing.
func (a *App) initJournal() error {
	if !a.cfg.Journal {
		return nil
	}

	r, err := journal.Start(a.world.Resources.Event.Queue, a.buildAnchor(), a.cfg.JournalSink, a.log)
	if err != nil {
		return err
	}
	a.recorder.Store(r)
	a.log.Info("journal", "msg", "journal opened", "path", r.Path())
	return nil
}

// journalErrorLocked says why this instance cannot start a journal now; an opening
// takes on a world, so whatever can refuse it refuses here first.
// Caller MUST hold updateMutex.
func (a *App) journalErrorLocked() error {
	switch {
	case a.world.FollowsJournal():
		return errors.New("a replay records nothing of its own")
	case a.recorder.Load() != nil:
		return errors.New("already journaling to " + a.recorder.Load().Path())
	case a.journalAsk.Load() != nil:
		return errors.New("a journal is already opening")
	case a.restart.Load() != nil:
		return errors.New("this run is being replaced")
	case a.scheduler.ResetPending():
		// The reset's cleanup has run and its boot spawn has not: the roster the
		// spawn restores is in no world a twin can take on.
		return errors.New("a new game is starting")
	case a.cfg.JournalSink == nil:
		return vlog.JournalUnavailable()
	}
	return nil
}

// startJournalLocked asks for a journal from where the run stands: this instance's
// with slot -1, else the one of the bot seat this run holds on slot. The loop owning
// that run opens it. Caller MUST hold updateMutex.
func (a *App) startJournalLocked(slot int) error {
	if slot >= 0 {
		return a.askSeatJournal(slot, true)
	}
	if err := a.journalErrorLocked(); err != nil {
		return err
	}
	a.journalAsk.Store(&journalAsk{start: true, report: a.reportJournal})
	return nil
}

// stopJournalLocked closes this instance's journal where the run stands, or asks
// the bot seat on slot to close its own; a later start opens another file.
// Caller MUST hold updateMutex.
func (a *App) stopJournalLocked(slot int) error {
	if slot >= 0 {
		return a.askSeatJournal(slot, false)
	}
	if a.recorder.Load() == nil {
		return errors.New("no journal is open")
	}
	a.closeJournal()
	return nil
}

// closeJournal writes the end record and releases the file. Call it with queue
// producers quiescent: under updateMutex, or once the scheduler has stopped.
func (a *App) closeJournal() {
	r := a.recorder.Swap(nil)
	if r == nil {
		return
	}
	stats, err := r.Close()
	a.log.Info("journal", "msg", "journal closed",
		"path", stats.Path, "records", stats.Emitted, "encode_failed", stats.EncodeFailed)
	if err != nil {
		a.log.Error("journal", "msg", "journal not closed", "error", err.Error())
	}
}

// serveJournal serves the journal start or stop asked of this run, on the loop that
// owns it and outside the world lock.
func (a *App) serveJournal() {
	ask := a.journalAsk.Swap(nil)
	if ask == nil {
		return
	}
	var (
		path string
		err  error
	)
	if ask.start {
		path, err = a.openJournal()
	} else {
		a.world.RunSafe(func() {
			if r := a.recorder.Load(); r != nil {
				path = r.Path()
				a.closeJournal()
				return
			}
			err = errors.New("no journal is open")
		})
	}
	ask.report(path, err)
}

// reportJournal tells this run's player how its own journal start went.
func (a *App) reportJournal(path string, err error) {
	if err != nil {
		a.log.Warn("journal", "msg", "journal not opened", "error", err.Error())
		a.ctx.SetStatusMessage("Journal not opened: "+err.Error(), parameter.StatusAlert)
		return
	}
	a.ctx.SetStatusMessage("Journaling to "+path+"; :journal stop closes it", parameter.StatusNotice)
}

// openJournal starts a journal where this run stands, without replacing the run.
// A replay of a journal opened mid-run boots afresh and writes the world it opened
// on over that boot, so the run takes on what the replay will hold there: the
// shared world is the run's own, and the player domain and everything else a boot
// decides are the boot's, as they are after a join. Call it outside the world lock,
// from the goroutine that owns the run's loop.
func (a *App) openJournal() (string, error) {
	var (
		an  event.JournalAnchor
		err error
	)
	a.world.RunSafe(func() {
		if err = a.journalErrorLocked(); err == nil {
			an = a.buildAnchor()
		}
	})
	if err != nil {
		return "", err
	}
	twin, err := a.journalTwin(an)
	if err != nil {
		return "", err
	}
	defer twin.Close()
	var r *journal.Recorder
	a.world.RunSafe(func() { r, err = a.openJournalLocked(an, twin) })
	if err != nil {
		return "", err
	}
	a.log.Info("journal", "msg", "journal opened", "path", r.Path(), "run", an.Run, "tick", an.Tick)
	return r.Path(), nil
}

// journalTwin builds a replay of a journal opening on anchor, its boot settled: the
// world such a replay holds before it writes the one the journal opened on.
func (a *App) journalTwin(an event.JournalAnchor) (*App, error) {
	an.Schema = event.JournalSchema // as the journal writes it
	cfg, err := ConfigFromAnchor(an)
	if err != nil {
		return nil, err
	}
	// Handed over rather than resolved by name, as a staging world's scenario is: it
	// may have come from the coordinator and exist nowhere on this host.
	scenario := a.scenario
	cfg.Resources = resource.Options{Dir: a.cfg.Resources.Dir, Content: a.cfg.Resources.Content,
		Embedded: a.cfg.Resources.Embedded, Provided: &scenario}
	cfg.RecTicks, cfg.StatTicks = -1, -1
	cfg.log = vlog.NewLog("journal")
	cfg.log.Mute(true)
	twin, err := NewHeadless(cfg)
	if err != nil {
		return nil, err
	}
	if err := twin.VerifyAnchor(an); err != nil {
		twin.Close()
		return nil, err
	}
	twin.Settle()
	return twin, nil
}

// openJournalLocked writes the journal's first capture, the world the run stands in
// against twin's boot, which twin then writes as a replay does; the run takes on
// what that left, and only then does the journal attach, so it records from a state
// its replay reproduces. Caller MUST hold updateMutex.
func (a *App) openJournalLocked(an event.JournalAnchor, twin *App) (*journal.Recorder, error) {
	// Nothing pending: the run takes on twin's queue, so an event held here is lost.
	// The checks read what that settle leaves, a reset it asked for included.
	a.scheduler.SettleLocked()
	if err := a.journalErrorLocked(); err != nil {
		return nil, err
	}
	// The twin is the boot of the identity the anchor read; a reset, a resize, a
	// rate change or a rebind since it was built is a world it is not the boot of.
	if a.buildAnchor() != an {
		return nil, errors.New("the run changed while the journal was opening; start it again")
	}
	opening, err := a.openingLocked(twin)
	if err != nil {
		return nil, err
	}
	d, err := newReplayDriver(twin, journal.Set{Captures: []event.JournalCapture{opening}}.Stream())
	if err != nil {
		return nil, err
	}
	if _, err := d.Step(); err != nil {
		return nil, err
	}
	if err := a.adoptLocked(twin, d); err != nil {
		return nil, err
	}
	r, err := journal.Start(a.world.Resources.Event.Queue, an, a.cfg.JournalSink, a.log)
	if err != nil {
		return nil, err
	}
	r.Capture(opening)
	a.recorder.Store(r)
	// The session the twin wrote as is identity alone; the one this run is in is
	// noted now, as a record, which is how its replay learns it.
	a.world.LatchSession()
	return r, nil
}

// openingLocked is the capture a journal opens with: the shared world the run stands
// in, written against twin's settled boot, with the operator state beside it.
// Caller MUST hold updateMutex.
func (a *App) openingLocked(twin *App) (event.JournalCapture, error) {
	cap, err := a.captureSharedLocked()
	if err != nil {
		return event.JournalCapture{}, err
	}
	var boot snapshot.SharedCapture
	twin.world.RunSafe(func() { boot, err = twin.captureSharedLocked() })
	if err != nil {
		return event.JournalCapture{}, err
	}
	d, err := a.encoder.DiffWritten(boot, cap)
	if err != nil {
		return event.JournalCapture{}, err
	}
	body, err := snapshot.EncodeJSON(d)
	if err != nil {
		return event.JournalCapture{}, err
	}
	var off []string
	for _, sys := range a.world.Systems() {
		if t, ok := sys.(engine.Toggled); ok && !t.Enabled() {
			off = append(off, sys.Name())
		}
	}
	at := a.Position()
	return event.JournalCapture{
		Run: at.Run, Tick: at.Tick, Boundary: at.Boundary,
		Participant: a.world.LocalParticipant(), Authority: cap.Header.Authority, Body: body,
		Opens: true, Mode: a.ctx.GetMode(), Paused: a.ctx.TimeCtl.IsPaused(), Off: off,
		Slot: a.world.Resources.Player.LocalSlot(),
	}, nil
}

// adoptLocked takes on twin's simulated state where its driver d stands, as a
// replay copy restored from a checkpoint does, and refuses a run that does not then
// hold twin's world. What stays is this instance's own: the terminal's colour, the
// wall clock and pacing, the ownedSystems with their status, and every counter a
// replay does not compare. Caller MUST hold updateMutex; twin's is taken here.
func (a *App) adoptLocked(twin *App, d *journal.ReplayDriver) error {
	var (
		c   *checkpoint
		err error
	)
	twin.world.RunSafe(func() { c, err = twin.checkpointLocked(d, nil) })
	if err != nil {
		return err
	}
	cfg := a.world.Resources.Config
	color, palette, wall := cfg.ColorMode, cfg.ConsolePalette, a.world.Resources.Time.RealTime
	a.world.CopyIn(c.world)
	cfg.ColorMode, cfg.ConsolePalette, a.world.Resources.Time.RealTime = color, palette, wall
	if unknown := c.world.LoadStreams(a.world.Resources.Rand); len(unknown) > 0 {
		return fmt.Errorf("RNG streams this run does not issue: %v", unknown)
	}
	savers := a.sharedStateSaversLocked()
	for _, rec := range c.systems {
		if slices.Contains(ownedSystems, rec.System) {
			continue
		}
		if err := savers[rec.System].LoadShared(rec.Data); err != nil {
			return fmt.Errorf("adopt %s: %w", rec.System, err)
		}
	}
	for _, sys := range a.world.Systems() {
		if slices.Contains(ownedSystems, sys.Name()) {
			continue
		}
		if cp, ok := sys.(engine.StateCopier); ok {
			if err := cp.RestoreState(c.local[sys.Name()]); err != nil {
				return fmt.Errorf("adopt %s: %w", sys.Name(), err)
			}
		}
	}
	if err := a.scheduler.ImportFSM(c.scheduler.FSM(), false); err != nil {
		return err
	}
	// After the FSM import, whose re-derived toggles are queued and dropped here
	a.world.Resources.Event.Queue.CopyIn(c.queue)
	for _, sys := range a.world.Systems() {
		if t, ok := sys.(engine.Toggled); ok && !slices.Contains(ownedSystems, sys.Name()) {
			t.SetEnabled(c.toggles[sys.Name()])
		}
	}
	service.MustGet[*service.ContentService](a.hub, "content").SetCursorState(c.content)
	c.status = snapshot.StatusState{
		Ints:    slices.DeleteFunc(c.status.Ints, func(s snapshot.IntCell) bool { return ownedStatusKey(s.Key) }),
		Bools:   slices.DeleteFunc(c.status.Bools, func(s snapshot.BoolCell) bool { return ownedStatusKey(s.Key) }),
		Floats:  slices.DeleteFunc(c.status.Floats, func(s snapshot.FloatCell) bool { return ownedStatusKey(s.Key) }),
		Strings: slices.DeleteFunc(c.status.Strings, func(s snapshot.StringCell) bool { return ownedStatusKey(s.Key) }),
	}
	a.installStatusLocked(c.status)
	if at, got := a.Position(), a.journalDigestLocked(); at != c.at || got != c.digest {
		return fmt.Errorf("the run stands at %+v with digest %+v after taking on the journal's opening, which stands at %+v with %+v",
			at, got, c.at, c.digest)
	}
	return nil
}

// buildAnchor describes what a replay must reproduce. Reads the telemetry
// ContentService published during initWorld, so anchor and verification share a source.
func (a *App) buildAnchor() event.JournalAnchor {
	reg := a.world.Resources.Status
	cfg := a.world.Resources.Config
	return event.JournalAnchor{
		Speed:          a.ctx.TimeCtl.Scale().String(),
		ScenarioID:     a.scenario.Name,
		ScenarioDigest: a.scenario.Digest(),
		ContentID:      reg.Strings.Get("content.source").Load(),
		ContentPin:     service.MustGet[*service.ContentService](a.hub, "content").Pin(),
		ContentFiles:   uint64(reg.Ints.Get("content.files").Load()),
		ContentBlocks:  uint64(reg.Ints.Get("content.blocks").Load()),
		ContentLines:   uint64(reg.Ints.Get("content.lines").Load()),
		Seed:           a.world.Resources.Rand.Root(),
		Session:        a.world.Resources.Rand.Session(),
		TickInterval:   int64(parameter.GameUpdateInterval),
		Width:          a.ctx.Width,
		Height:         a.ctx.Height,
		MapWidth:       cfg.MapWidth,
		MapHeight:      cfg.MapHeight,
		CropOnResize:   cfg.CropOnResize,
		CellAspect:     float64(cfg.CellAspect),
		SessionShared:  a.world.SessionShared(),
		Slot:           uint64(a.world.Resources.Player.LocalSlot()),
	}
}

// journalSummary names this instance's journal and every one its seats keep.
func (a *App) journalSummary() string {
	var parts []string
	if p := a.recorder.Load().Path(); p != "" {
		parts = append(parts, "Journaling to "+p)
	}
	a.seatsMu.Lock()
	for _, s := range a.seats {
		if b := s.app.Load(); b != nil {
			if p := b.recorder.Load().Path(); p != "" {
				parts = append(parts, fmt.Sprintf("slot %X to %s", s.slot.Load(), p))
			}
		}
	}
	a.seatsMu.Unlock()
	if len(parts) == 0 {
		return "No journal; :journal start [slot] records from here"
	}
	return strings.Join(parts, "; ")
}
