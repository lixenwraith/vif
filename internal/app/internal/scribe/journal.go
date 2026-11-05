package scribe

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/lixenwraith/vif/internal/app/internal/corrector"
	"github.com/lixenwraith/vif/internal/content"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/service"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/internal/vlog"
	"github.com/lixenwraith/vif/pkg/position"
)

// journalAsk is a journal start or stop asked for from the command line, which the
// loop owning the run serves once the command has settled: an opening takes on a
// world wholesale, under which a command's own events still queued would be lost.
// report says how it went, on the status line of whoever asked; then is a start
// asked while this stop waited, which opens once it has closed.
type journalAsk struct {
	start  bool
	report func(path string, err error)
	then   *journalAsk
}

// opening is the start asked, alone or after a stop, or nil
func (k *journalAsk) opening() *journalAsk {
	if k != nil && !k.start {
		return k.then
	}
	return k
}

// QueueJournalAsk leaves a start or a stop for this run's loop, which tells report
// how it went; a start asked while a stop waits follows it, so a journal stopped and
// started before the loop came round closes and another opens. It reports false
// when an ask already waits that this one cannot follow.
func (sc *Scribe) QueueJournalAsk(start bool, report func(path string, err error)) bool {
	ask := &journalAsk{start: start, report: report}
	for {
		cur, next := sc.journalAsk.Load(), ask
		switch {
		case cur == nil:
		case ask.start && cur.opening() == nil:
			next = &journalAsk{report: cur.report, then: ask}
		default:
			return false
		}
		if sc.journalAsk.CompareAndSwap(cur, next) {
			return true
		}
	}
}

// AskPending reports a start or a stop this run's loop has not served yet.
func (sc *Scribe) AskPending() bool { return sc.journalAsk.Load() != nil }

// AbandonAsks drops what this run's loop never served, as its run closes, and tells
// the start among them why.
func (sc *Scribe) AbandonAsks(err error) {
	if ask := sc.journalAsk.Swap(nil).opening(); ask != nil {
		ask.report("", err)
	}
}

// JournalLostLocked closes j where it is still true once a world this run wrote
// could not be recorded in it: past that write, nothing it holds would verify. A
// journal opened since holds that world already. Caller MUST hold updateMutex.
func (sc *Scribe) JournalLostLocked(j *event.Journal, err error) {
	if j == nil || sc.world.Resources.Event.Queue.Journal() != j {
		return
	}
	path, _ := sc.CloseJournal()
	at := sc.run.Position().Tick
	sc.log.Error("journal", "msg", "journal closed: a written world was not recorded", "path", path, "tick", at, "error", err.Error())
	sc.ctx.SetStatusMessage(fmt.Sprintf("Journal closed at tick %d: a written world could not be recorded (%s)", at, err), parameter.StatusAlert)
}

// ErrRunReplaced refuses what a run about to be replaced cannot do. A journal start
// refused with it is the replacing run's to serve: it opens a journal from its start.
var ErrRunReplaced = errors.New("this run is being replaced")

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

// ownedStream reports a stream one of ownedSystems draws, which stays this
// instance's own and which a build without audio never issues.
func ownedStream(s engine.StreamState) bool { return slices.Contains(ownedSystems, s.Label) }

// InitJournal opens the replay journal a run starts with and installs it on the
// event queue: for the life of the run it records every event of a journaled origin
// (all but system and device) and every own crossing. One carried from the run this
// one replaces only notes a failure; notice is the line the run's status opens with.
func (sc *Scribe) InitJournal(carried bool) (notice string, err error) {
	r, err := journal.Start(sc.world.Resources.Event.Queue, sc.BuildAnchor(), sc.run.JournalSink(), sc.log)
	switch {
	case err != nil && carried:
		sc.log.Warn("journal", "msg", "journal not reopened", "error", err.Error())
		return "Journal not opened in this run: " + err.Error(), nil
	case err != nil:
		return "", err
	case carried:
		notice = "Journaling on to " + r.Path()
	}
	sc.recorder.Store(r)
	sc.log.Info("journal", "msg", "journal opened", "path", r.Path())
	return notice, nil
}

// JournalErrorLocked says why this instance cannot start a journal now; an opening
// takes on a world, so whatever can refuse it refuses here first.
// Caller MUST hold updateMutex.
func (sc *Scribe) JournalErrorLocked() error {
	switch {
	case sc.world.FollowsJournal():
		return errors.New("a replay records nothing of its own")
	case sc.journalAsk.Load().opening() != nil:
		return errors.New("a journal is already opening")
	case sc.recorder.Load() != nil && sc.journalAsk.Load() == nil:
		return errors.New("already journaling to " + sc.recorder.Load().Path())
	case sc.run.Replacing():
		return ErrRunReplaced
	case sc.scheduler.ResetPending():
		// The reset's cleanup has run and its boot spawn has not: the roster the
		// spawn restores is in no world a twin can take on.
		return errors.New("a new game is starting")
	case sc.run.JournalSink() == nil:
		return vlog.JournalUnavailable()
	}
	return nil
}

// StartJournalLocked asks for a journal from where the run stands: this instance's
// with slot -1, else the one of the bot seat this run holds on slot. The loop owning
// that run opens it. Caller MUST hold updateMutex.
func (sc *Scribe) StartJournalLocked(slot int) error {
	if slot >= 0 {
		return sc.run.AskSeatJournal(slot, true)
	}
	if err := sc.JournalErrorLocked(); err != nil && !errors.Is(err, ErrRunReplaced) {
		return err
	}
	if !sc.QueueJournalAsk(true, sc.reportJournal) {
		return errors.New("a journal is already opening")
	}
	return nil
}

// StopJournalLocked asks for this instance's journal to close, or the bot seat's on
// slot; a later start opens another file. The loop owning the run closes it once the
// run is back in play, so the journal ends there rather than on the command line or
// a menu. Caller MUST hold updateMutex.
func (sc *Scribe) StopJournalLocked(slot int) error {
	switch {
	case slot >= 0:
		return sc.run.AskSeatJournal(slot, false)
	case sc.CancelJournalStart():
		return nil
	case sc.recorder.Load() == nil:
		return errors.New("no journal is open")
	case !sc.QueueJournalAsk(false, sc.reportJournalClosed):
		return errors.New("the journal is already closing")
	}
	return nil
}

// Journaling reports whether this instance records once its asks are served: a stop
// not yet served wins over the journal it would close, and a start over its absence.
func (sc *Scribe) Journaling() bool {
	ask := sc.journalAsk.Load()
	return ask.opening() != nil || ask == nil && sc.recorder.Load() != nil
}

// CancelJournalStart lets a stop win over a start not yet served and reports whether
// there was one. A start alone is reported cancelled; one that followed a stop leaves
// that stop, whose close is then all there is to tell.
func (sc *Scribe) CancelJournalStart() bool {
	ask := sc.journalAsk.Load()
	start := ask.opening()
	if start == nil {
		return false
	}
	var left *journalAsk
	if start != ask {
		left = &journalAsk{report: ask.report}
	}
	if !sc.journalAsk.CompareAndSwap(ask, left) {
		return false
	}
	if left == nil {
		start.report("", errors.New("cancelled by :journal stop"))
	}
	return true
}

// CloseJournal writes the end record and releases the file, returning its path, or
// "" with none open, and why its tail may not have reached it. Call it with queue
// producers quiescent: under updateMutex, or once the scheduler has stopped.
func (sc *Scribe) CloseJournal() (string, error) {
	r := sc.recorder.Swap(nil)
	if r == nil {
		return "", nil
	}
	stats, err := r.Close()
	sc.log.Info("journal", "msg", "journal closed",
		"path", stats.Path, "records", stats.Emitted, "encode_failed", stats.EncodeFailed)
	if err != nil {
		sc.log.Error("journal", "msg", "journal not closed", "error", err.Error())
		return stats.Path, fmt.Errorf("%s closed incomplete: %w", stats.Path, err)
	}
	return stats.Path, nil
}

// ServeJournal serves the journal start or stop asked of this run, on the loop that
// owns it and outside the world lock. A stop waits while the run is paused on an
// overlay or the command line, whose frames a replay would end on, and stays asked
// until it has closed, so a start asked meanwhile, maybe from another goroutine,
// follows it rather than finding the journal still open.
func (sc *Scribe) ServeJournal() {
	ask := sc.journalAsk.Load()
	if ask == nil || !ask.start && sc.pausedForInput() {
		return
	}
	if !ask.start {
		var (
			path string
			err  error
		)
		sc.world.RunSafe(func() {
			if sc.recorder.Load() != nil {
				path, err = sc.CloseJournal()
				return
			}
			err = errors.New("no journal is open")
		})
		ask.report(path, err)
		// Whatever waits now is this stop, a start having joined it or left it since
		for ask = sc.journalAsk.Load(); !sc.journalAsk.CompareAndSwap(ask, nil); ask = sc.journalAsk.Load() {
		}
		if ask = ask.then; ask == nil {
			return
		}
	} else if !sc.journalAsk.CompareAndSwap(ask, nil) {
		return
	}
	path, err := sc.OpenJournal()
	if errors.Is(err, ErrRunReplaced) {
		sc.journalAsk.CompareAndSwap(nil, ask) // left for the replacing run
		return
	}
	ask.report(path, err)
}

// pausedForInput reports whether the run stands paused on an overlay or the command line
func (sc *Scribe) pausedForInput() bool {
	m := sc.ctx.GetMode()
	return sc.ctx.TimeCtl.OwnPause() && (m == core.ModeOverlay || m == core.ModeCommand)
}

// reportJournalClosed tells this run's player how its own journal stop went.
func (sc *Scribe) reportJournalClosed(path string, err error) {
	if err != nil {
		sc.ctx.SetStatusMessage("Journal: "+err.Error(), parameter.StatusAlert)
		return
	}
	sc.ctx.SetStatusMessage("Journal closed: "+path, parameter.StatusNotice)
}

// reportJournal tells this run's player how its own journal start went.
func (sc *Scribe) reportJournal(path string, err error) {
	if err != nil {
		sc.log.Warn("journal", "msg", "journal not opened", "error", err.Error())
		sc.ctx.SetStatusMessage("Journal not opened: "+err.Error(), parameter.StatusAlert)
		return
	}
	sc.ctx.SetStatusMessage("Journaling to "+path+"; :journal stop closes it", parameter.StatusNotice)
}

// OpenJournal starts a journal where this run stands, without replacing the run. A
// replay of it boots afresh and writes the opening's world and own side over that
// boot, and the run takes on what the replay then holds, which is the run as it
// stood. Call it outside the world lock, from the goroutine that owns the run's loop.
func (sc *Scribe) OpenJournal() (string, error) {
	var (
		an  event.JournalAnchor
		err error
	)
	sc.world.RunSafe(func() {
		if err = sc.JournalErrorLocked(); err == nil {
			an = sc.BuildAnchor()
		}
	})
	if err != nil {
		return "", err
	}
	twin, err := sc.run.JournalTwin(an)
	if err != nil {
		return "", err
	}
	defer twin.Close()
	var r *journal.Recorder
	sc.world.RunSafe(func() { r, err = sc.OpenJournalLocked(an, twin) })
	if err != nil {
		return "", err
	}
	sc.log.Info("journal", "msg", "journal opened", "path", r.Path(), "run", an.Run, "tick", an.Tick)
	return r.Path(), nil
}

// OpenJournalLocked writes the journal's first capture, the world the run stands in
// against twin's boot, which twin then writes as a replay does; the run takes on
// what that left, and only then does the journal attach, so it records from a state
// its replay reproduces. Caller MUST hold updateMutex.
func (sc *Scribe) OpenJournalLocked(an event.JournalAnchor, twin Twin) (*journal.Recorder, error) {
	// Nothing pending: the run takes on twin's queue, so an event held here is lost.
	// The checks read what that settle leaves, a reset it asked for included.
	sc.scheduler.SettleLocked()
	if err := sc.JournalErrorLocked(); err != nil {
		return nil, err
	}
	// The twin is the boot of the identity the anchor read; a reset, a resize, a
	// rate change or a rebind since it was built is a world it is not the boot of.
	if sc.BuildAnchor() != an {
		return nil, errors.New("the run changed while the journal was opening; start it again")
	}
	opening, err := sc.openingLocked(twin.Scribe())
	if err != nil {
		return nil, err
	}
	if err := journal.LineHolds(opening.Body, opening.Own); err != nil {
		return nil, err
	}
	d, err := twin.ReplayDriver(journal.Set{Captures: []event.JournalCapture{opening}}.Stream())
	if err != nil {
		return nil, err
	}
	if _, err := d.Step(); err != nil {
		return nil, err
	}
	if err := sc.adoptLocked(twin.Scribe(), d); err != nil {
		// Past its first write the run holds part of the twin's world: a defect to find
		sc.log.Error("journal", "msg", "run may hold part of an opening it does not match", "error", err.Error())
		return nil, err
	}
	r, err := journal.Start(sc.world.Resources.Event.Queue, an, sc.run.JournalSink(), sc.log)
	if err != nil {
		return nil, err
	}
	r.Capture(opening)
	sc.recorder.Store(r)
	// The session the twin wrote as is identity alone; the one this run is in is
	// noted now, as a record, which is how its replay learns it.
	sc.world.LatchSession()
	return r, nil
}

// openingLocked is the capture a journal opens with: the shared world the run stands
// in, written against twin's settled boot, with the operator state and the run's own
// side beside it. Caller MUST hold updateMutex.
func (sc *Scribe) openingLocked(twin *Scribe) (event.JournalCapture, error) {
	cap, err := sc.glue.CaptureSharedLocked()
	if err != nil {
		return event.JournalCapture{}, err
	}
	own, err := sc.ownStateLocked()
	if err != nil {
		return event.JournalCapture{}, err
	}
	ownBody, err := snapshot.EncodeJSON(own)
	if err != nil {
		return event.JournalCapture{}, err
	}
	var boot snapshot.SharedCapture
	twin.world.RunSafe(func() { boot, err = twin.glue.CaptureSharedLocked() })
	if err != nil {
		return event.JournalCapture{}, err
	}
	d, err := sc.glue.Encoder().DiffWritten(boot, cap)
	if err != nil {
		return event.JournalCapture{}, err
	}
	body, err := snapshot.EncodeJSON(d)
	if err != nil {
		return event.JournalCapture{}, err
	}
	var off []string
	for _, sys := range sc.world.Systems() {
		if t, ok := sys.(engine.Toggled); ok && !t.Enabled() {
			off = append(off, sys.Name())
		}
	}
	at := sc.run.Position()
	return event.JournalCapture{
		Run: at.Run, Tick: at.Tick, Boundary: at.Boundary,
		Participant: sc.world.LocalParticipant(), Authority: cap.Header.Authority,
		Barrier: sc.run.BarrierLocked(), Body: body,
		Opens: true, Mode: sc.world.Resources.Game.State.GetMode(), Held: sc.ctx.TimeCtl.Held(), Start: sc.ctx.TimeCtl.Holds(engine.HoldStart), Off: off,
		Slot: sc.world.Resources.Player.LocalSlot(), Own: ownBody,
	}, nil
}

// OwnState is the run's own side, which a journal's opening carries beside the shared
// world so its replay plays on as this participant rather than as a fresh boot of
// one: the player world, the player's resources, each system's own state, where the
// corpus walk stands, the event tallies, every status cell but this instance's own
// (ownedStatusKey), and the order the run holds both domains' entities in.
type OwnState struct {
	World   engine.PlayerWorldState      `json:"world"`
	Side    engine.PlayerSide            `json:"side"`
	Systems []snapshot.SystemStateRecord `json:"systems,omitempty"`
	Content content.CursorState          `json:"content"`
	Events  event.QueueTelemetry         `json:"events"`
	Status  snapshot.StatusState         `json:"status"`
	Order   engine.WorldOrder            `json:"order"`
}

// ownStateLocked reads the run's own side. Caller MUST hold updateMutex.
func (sc *Scribe) ownStateLocked() (OwnState, error) {
	own := OwnState{
		World:   sc.world.CapturePlayerWorld(),
		Side:    sc.world.CapturePlayerSide(),
		Content: service.MustGet[*service.ContentService](sc.hub, "content").CursorState(),
		Events:  sc.world.Resources.Event.Queue.Telemetry(),
		Status:  corrector.StatusCellsLocked(sc.world, corrector.StatusKeysLocked(sc.world, func(k string) bool { return !ownedStatusKey(k) })),
		Order:   sc.world.CaptureOrder(),
	}
	own.Side.Streams = slices.DeleteFunc(own.Side.Streams, ownedStream)
	for _, sys := range sc.world.Systems() {
		s, ok := sys.(engine.LocalStateSaver)
		if !ok || slices.Contains(ownedSystems, sys.Name()) {
			continue
		}
		data, err := s.SaveLocal()
		if err != nil {
			return OwnState{}, fmt.Errorf("%s: %w", sys.Name(), err)
		}
		own.Systems = append(own.Systems, snapshot.SystemStateRecord{System: sys.Name(), Data: data})
	}
	return own, nil
}

// installOwnLocked writes the run's own side over a replay's opening world, after
// the settle that bound its input, so nothing that settle derived for the boot's
// player stands in for it. Caller MUST hold updateMutex.
func (sc *Scribe) installOwnLocked(own OwnState) error {
	sc.world.InstallPlayerWorld(own.World)
	if unknown := sc.world.InstallPlayerSide(own.Side); len(unknown) > 0 {
		return fmt.Errorf("RNG streams this replay does not issue: %v", unknown)
	}
	savers := make(map[string]engine.LocalStateSaver, len(own.Systems))
	for _, sys := range sc.world.Systems() {
		if s, ok := sys.(engine.LocalStateSaver); ok && !slices.Contains(ownedSystems, sys.Name()) {
			savers[sys.Name()] = s
		}
	}
	for _, rec := range own.Systems {
		s, ok := savers[rec.System]
		if !ok {
			return fmt.Errorf("no system %q carries its own state", rec.System)
		}
		if err := s.LoadLocal(rec.Data); err != nil {
			return err
		}
		delete(savers, rec.System)
	}
	// One left without a record would play on from the boot's state, unseen
	if len(savers) > 0 {
		return fmt.Errorf("the opening carries no state for %v", slices.Sorted(maps.Keys(savers)))
	}
	if err := service.MustGet[*service.ContentService](sc.hub, "content").SetCursorState(own.Content); err != nil {
		return err
	}
	sc.world.Resources.Event.Queue.SetTelemetry(own.Events)
	corrector.InstallStatusLocked(sc.world, own.Status)
	// Last, once both domains' rows stand: the shared write and the settle placed theirs
	if err := sc.world.InstallOrder(own.Order); err != nil {
		return fmt.Errorf("the run's order: %w", err)
	}
	return nil
}

// OpenedLocked settles what writing a journal's opening world queued, with input
// bound to the slot the run followed, writes the run's own side over what that
// derived, then takes up the play mode, the hold and the systems the run stood in.
// Caller MUST hold updateMutex.
func (sc *Scribe) OpenedLocked(c event.JournalCapture, own OwnState) error {
	sc.world.PushLocalOrigin(event.EventCursorSetLocalRequest, &event.CursorSetLocalPayload{Slot: c.Slot}, event.OriginSystem)
	sc.scheduler.SettleLocked()
	if err := sc.installOwnLocked(own); err != nil {
		return err
	}
	sc.ctx.SetPlayMode(c.Mode)
	sc.world.UpdateBoundsRadius()
	if c.Start {
		sc.ctx.TimeCtl.HoldAs(engine.HoldStart)
	} else {
		sc.ctx.TimeCtl.SetHeld(c.Held)
	}
	for _, sys := range sc.world.Systems() {
		if t, ok := sys.(engine.Toggled); ok {
			t.SetEnabled(!slices.Contains(c.Off, sys.Name()))
		}
	}
	return nil
}

// adoptLocked takes on twin's simulated state where its driver d stands, as a
// replay copy restored from a checkpoint does, and refuses a run that does not then
// hold twin's world. What stays is this instance's own: the terminal's colour, the
// wall clock and pacing, the pointer placement it crosses next tick, and the
// ownedSystems with their streams and status. Caller MUST hold updateMutex.
func (sc *Scribe) adoptLocked(twin *Scribe, d *journal.ReplayDriver) error {
	var (
		c   *Checkpoint
		err error
	)
	twin.world.RunSafe(func() { c, err = twin.CheckpointLocked(d, nil) })
	if err != nil {
		return err
	}
	if err := service.MustGet[*service.ContentService](sc.hub, "content").SetCursorState(c.content); err != nil {
		return err
	}
	cfg := sc.world.Resources.Config
	color, palette, wall := cfg.ColorMode, cfg.ConsolePalette, sc.world.Resources.Time.RealTime
	held := *sc.world.Resources.Player
	kept := slices.DeleteFunc(sc.world.Resources.Rand.SaveStreams(core.DomainPlayer), func(s engine.StreamState) bool { return !ownedStream(s) })
	sc.world.CopyIn(c.World)
	cfg.ColorMode, cfg.ConsolePalette, sc.world.Resources.Time.RealTime = color, palette, wall
	sc.world.Resources.Player.KeepPointer(&held)
	if unknown := c.World.LoadStreams(sc.world.Resources.Rand); len(unknown) > 0 {
		return fmt.Errorf("RNG streams this run does not issue: %v", unknown)
	}
	sc.world.Resources.Rand.LoadStreams(core.DomainPlayer, kept)
	savers := corrector.SharedStateSaversLocked(sc.world)
	for _, rec := range c.systems {
		if slices.Contains(ownedSystems, rec.System) {
			continue
		}
		if err := savers[rec.System].LoadShared(rec.Data); err != nil {
			return fmt.Errorf("adopt %s: %w", rec.System, err)
		}
	}
	for _, sys := range sc.world.Systems() {
		if slices.Contains(ownedSystems, sys.Name()) {
			continue
		}
		if err := loadLocalState(sys, c.local[sys.Name()]); err != nil {
			return fmt.Errorf("adopt %s: %w", sys.Name(), err)
		}
	}
	if err := sc.scheduler.ImportFSM(c.scheduler.FSM(), false); err != nil {
		return err
	}
	// After the FSM import, whose re-derived toggles are queued and dropped here
	sc.world.Resources.Event.Queue.CopyIn(c.queue)
	for _, sys := range sc.world.Systems() {
		if t, ok := sys.(engine.Toggled); ok && !slices.Contains(ownedSystems, sys.Name()) {
			t.SetEnabled(c.toggles[sys.Name()])
		}
	}
	c.status = snapshot.StatusState{
		Ints:    slices.DeleteFunc(c.status.Ints, func(s snapshot.IntCell) bool { return ownedStatusKey(s.Key) }),
		Bools:   slices.DeleteFunc(c.status.Bools, func(s snapshot.BoolCell) bool { return ownedStatusKey(s.Key) }),
		Floats:  slices.DeleteFunc(c.status.Floats, func(s snapshot.FloatCell) bool { return ownedStatusKey(s.Key) }),
		Strings: slices.DeleteFunc(c.status.Strings, func(s snapshot.StringCell) bool { return ownedStatusKey(s.Key) }),
	}
	corrector.InstallStatusLocked(sc.world, c.status)
	if at, got := sc.run.Position(), sc.JournalDigestLocked(); at != c.at || got != c.Digest {
		return fmt.Errorf("the run stands at %+v with digest %+v after taking on the journal's opening, which stands at %+v with %+v",
			at, got, c.at, c.Digest)
	}
	return nil
}

// BuildAnchor describes what a replay must reproduce. Reads the telemetry
// ContentService published during initWorld, so anchor and verification share a source.
func (sc *Scribe) BuildAnchor() event.JournalAnchor {
	reg := sc.world.Resources.Status
	cfg := sc.world.Resources.Config
	return event.JournalAnchor{
		Speed:          sc.ctx.TimeCtl.Scale().String(),
		ScenarioID:     sc.scenario.Name,
		ScenarioDigest: sc.scenario.Digest(),
		ContentID:      reg.Strings.Get("content.source").Load(),
		ContentPin:     service.MustGet[*service.ContentService](sc.hub, "content").Pin(),
		ContentFiles:   uint64(reg.Ints.Get("content.files").Load()),
		ContentBlocks:  uint64(reg.Ints.Get("content.blocks").Load()),
		ContentLines:   uint64(reg.Ints.Get("content.lines").Load()),
		Seed:           sc.world.Resources.Rand.Root(),
		Session:        sc.world.Resources.Rand.Session(),
		TickInterval:   int64(sc.world.TickInterval()),
		Width:          sc.ctx.Width,
		Height:         sc.ctx.Height,
		MapWidth:       cfg.MapWidth,
		MapHeight:      cfg.MapHeight,
		CropOnResize:   cfg.CropOnResize,
		CellAspect:     float64(cfg.CellAspect),
		SessionShared:  sc.world.SessionShared(),
		Slot:           uint64(sc.world.Resources.Player.LocalSlot()),
	}
}

// SessionAnchorFields are what two participants in one session must agree on:
// record layout and tick rate, the seed and session counter every RNG stream
// derives from, and the scenario's bytes, whose name is only a lookup hint.
// Geometry is per-instance, and the corpus is player domain, read from each
// machine's own roots and never reconciled (D-11, doc/multi-player.md).
func (sc *Scribe) SessionAnchorFields(an event.JournalAnchor) []corrector.AnchorField {
	return []corrector.AnchorField{
		{Name: "schema", Want: an.Schema, Got: uint64(event.JournalSchema)},
		{Name: "seed", Want: an.Seed, Got: sc.world.Resources.Rand.Root()},
		{Name: "session", Want: an.Session, Got: sc.world.Resources.Rand.Session()},
		{Name: "scenario_digest", Want: an.ScenarioDigest, Got: sc.scenario.Digest()},
		{Name: "tick_ns", Want: an.TickInterval, Got: int64(sc.world.TickInterval())},
	}
}

// AnchorIdentity adds what a replay must reproduce and a join must not require.
// A recorded run typed the blocks of one corpus, so replaying its input against
// another yields different glyphs; a peer joining live only has to agree on the
// world, and brings its own text into it.
func (sc *Scribe) AnchorIdentity(an event.JournalAnchor) []corrector.AnchorField {
	reg := sc.world.Resources.Status
	svc := service.MustGet[*service.ContentService](sc.hub, "content")
	return append(sc.SessionAnchorFields(an),
		corrector.AnchorField{Name: "content_id", Want: an.ContentID, Got: reg.Strings.Get("content.source").Load()},
		corrector.AnchorField{Name: "content_pin", Want: an.ContentPin, Got: svc.Pin()},
		corrector.AnchorField{Name: "content_files", Want: an.ContentFiles, Got: uint64(reg.Ints.Get("content.files").Load())},
		corrector.AnchorField{Name: "content_blocks", Want: an.ContentBlocks, Got: uint64(reg.Ints.Get("content.blocks").Load())},
		corrector.AnchorField{Name: "content_lines", Want: an.ContentLines, Got: uint64(reg.Ints.Get("content.lines").Load())},
	)
}

// VerifyAnchor reports whether this run reproduces what the anchor recorded.
// A resolved path proves which corpus was asked for, not which one loaded, so the
// fingerprint is compared after construction: a discovered file or a changed corpus
// becomes a startup error instead of an unexplained snapshot diff many ticks later.
// Call after NewHeadless, before Replay.
func (sc *Scribe) VerifyAnchor(an event.JournalAnchor) error {
	fields := append(sc.AnchorIdentity(an),
		corrector.AnchorField{Name: "width", Want: an.Width, Got: sc.ctx.Width},
		corrector.AnchorField{Name: "height", Want: an.Height, Got: sc.ctx.Height})
	return corrector.FirstAnchorMismatch("anchor", fields)
}

// JournalDigestLocked is the world a journal's digest carries: both domains, since
// a replay reproduces the player's own as well. Caller MUST hold updateMutex.
func (sc *Scribe) JournalDigestLocked() event.JournalDigest {
	return snapshot.DigestWorld(sc.world, position.ScopeAll).Journal()
}

// JournalSummary names this instance's journal and every one its seats keep.
func (sc *Scribe) JournalSummary() string {
	var parts []string
	if p := sc.recorder.Load().Path(); p != "" {
		parts = append(parts, "Journaling to "+p)
	}
	parts = append(parts, sc.run.SeatJournalSummaries()...)
	if len(parts) == 0 {
		return "No journal; :journal start [slot] records from here"
	}
	return strings.Join(parts, "; ")
}
