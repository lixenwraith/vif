package app

import (
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/lixenwraith/vif/internal/content"
	"github.com/lixenwraith/vif/internal/core"
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

// queueJournalAsk leaves ask for this run's loop; a start asked while a stop waits
// follows it, so a journal stopped and started before the loop came round closes and
// another opens. It reports false when an ask already waits that ask cannot follow.
func (a *App) queueJournalAsk(ask *journalAsk) bool {
	for {
		cur, next := a.journalAsk.Load(), ask
		switch {
		case cur == nil:
		case ask.start && cur.opening() == nil:
			next = &journalAsk{report: cur.report, then: ask}
		default:
			return false
		}
		if a.journalAsk.CompareAndSwap(cur, next) {
			return true
		}
	}
}

// joinNotice adds one line to what the next run's status line opens with.
func joinNotice(notice, line string) string {
	if notice == "" {
		return line
	}
	return notice + "; " + line
}

// lineHolds refuses a capture whose payloads one journal line could not hold, as no
// reader would then take the journal.
func lineHolds(payloads ...[]byte) error {
	n := 0
	for _, p := range payloads {
		n += base64.StdEncoding.EncodedLen(len(p))
	}
	if n > parameter.JournalLineBytes-parameter.JournalLineHeadroom {
		return fmt.Errorf("the world is %d MiB, more than a journal line holds", n>>20)
	}
	return nil
}

// journalLostLocked closes j where it is still true once a world this run wrote
// could not be recorded in it: past that write, nothing it holds would verify. A
// journal opened since holds that world already. Caller MUST hold updateMutex.
func (a *App) journalLostLocked(j *event.Journal, err error) {
	if j == nil || a.world.Resources.Event.Queue.Journal() != j {
		return
	}
	path, _ := a.closeJournal()
	at := a.Position().Tick
	a.log.Error("journal", "msg", "journal closed: a written world was not recorded", "path", path, "tick", at, "error", err.Error())
	a.ctx.SetStatusMessage(fmt.Sprintf("Journal closed at tick %d: a written world could not be recorded (%s)", at, err), parameter.StatusAlert)
}

// errRunReplaced refuses a journal start in a run about to be replaced, which the
// replacing run serves instead: it opens a journal from its start.
var errRunReplaced = errors.New("this run is being replaced")

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

// initJournal opens the replay journal a run starts with and installs it on the
// event queue: for the life of the run it records every event of a journaled origin
// (all but system and device) and every own crossing.
func (a *App) initJournal() error {
	if !a.cfg.Journal {
		return nil
	}

	r, err := journal.Start(a.world.Resources.Event.Queue, a.buildAnchor(), a.cfg.JournalSink, a.log)
	switch {
	case err != nil && a.cfg.journalCarried:
		a.log.Warn("journal", "msg", "journal not reopened", "error", err.Error())
		a.cfg.notice = joinNotice(a.cfg.notice, "Journal not opened in this run: "+err.Error())
		return nil
	case err != nil:
		return err
	case a.cfg.journalCarried:
		a.cfg.notice = joinNotice(a.cfg.notice, "Journaling on to "+r.Path())
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
	case a.journalAsk.Load().opening() != nil:
		return errors.New("a journal is already opening")
	case a.recorder.Load() != nil && a.journalAsk.Load() == nil:
		return errors.New("already journaling to " + a.recorder.Load().Path())
	case a.restart.Load() != nil:
		return errRunReplaced
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
	if err := a.journalErrorLocked(); err != nil && !errors.Is(err, errRunReplaced) {
		return err
	}
	if !a.queueJournalAsk(&journalAsk{start: true, report: a.reportJournal}) {
		return errors.New("a journal is already opening")
	}
	return nil
}

// stopJournalLocked asks for this instance's journal to close, or the bot seat's on
// slot; a later start opens another file. The loop owning the run closes it once the
// run is back in play, so the journal ends there rather than on the command line or
// a menu. Caller MUST hold updateMutex.
func (a *App) stopJournalLocked(slot int) error {
	switch {
	case slot >= 0:
		return a.askSeatJournal(slot, false)
	case a.cancelJournalStart():
		return nil
	case a.recorder.Load() == nil:
		return errors.New("no journal is open")
	case !a.queueJournalAsk(&journalAsk{report: a.reportJournalClosed}):
		return errors.New("the journal is already closing")
	}
	return nil
}

// journaling reports whether this instance records once its asks are served: a stop
// not yet served wins over the journal it would close, and a start over its absence.
func (a *App) journaling() bool {
	ask := a.journalAsk.Load()
	return ask.opening() != nil || ask == nil && a.recorder.Load() != nil
}

// cancelJournalStart lets a stop win over a start not yet served and reports whether
// there was one. A start alone is reported cancelled; one that followed a stop leaves
// that stop, whose close is then all there is to tell.
func (a *App) cancelJournalStart() bool {
	ask := a.journalAsk.Load()
	start := ask.opening()
	if start == nil {
		return false
	}
	var left *journalAsk
	if start != ask {
		left = &journalAsk{report: ask.report}
	}
	if !a.journalAsk.CompareAndSwap(ask, left) {
		return false
	}
	if left == nil {
		start.report("", errors.New("cancelled by :journal stop"))
	}
	return true
}

// closeJournal writes the end record and releases the file, returning its path, or
// "" with none open, and why its tail may not have reached it. Call it with queue
// producers quiescent: under updateMutex, or once the scheduler has stopped.
func (a *App) closeJournal() (string, error) {
	r := a.recorder.Swap(nil)
	if r == nil {
		return "", nil
	}
	stats, err := r.Close()
	a.log.Info("journal", "msg", "journal closed",
		"path", stats.Path, "records", stats.Emitted, "encode_failed", stats.EncodeFailed)
	if err != nil {
		a.log.Error("journal", "msg", "journal not closed", "error", err.Error())
		return stats.Path, fmt.Errorf("%s closed incomplete: %w", stats.Path, err)
	}
	return stats.Path, nil
}

// serveJournal serves the journal start or stop asked of this run, on the loop that
// owns it and outside the world lock. A stop waits while the run is paused on an
// overlay or the command line, whose frames a replay would end on, and stays asked
// until it has closed, so a start asked meanwhile, maybe from another goroutine,
// follows it rather than finding the journal still open.
func (a *App) serveJournal() {
	ask := a.journalAsk.Load()
	if ask == nil || !ask.start && a.pausedForInput() {
		return
	}
	if !ask.start {
		var (
			path string
			err  error
		)
		a.world.RunSafe(func() {
			if a.recorder.Load() != nil {
				path, err = a.closeJournal()
				return
			}
			err = errors.New("no journal is open")
		})
		ask.report(path, err)
		// Whatever waits now is this stop, a start having joined it or left it since
		for ask = a.journalAsk.Load(); !a.journalAsk.CompareAndSwap(ask, nil); ask = a.journalAsk.Load() {
		}
		if ask = ask.then; ask == nil {
			return
		}
	} else if !a.journalAsk.CompareAndSwap(ask, nil) {
		return
	}
	path, err := a.openJournal()
	if errors.Is(err, errRunReplaced) {
		a.journalAsk.CompareAndSwap(nil, ask) // left for the replacing run
		return
	}
	ask.report(path, err)
}

// pausedForInput reports whether the run stands paused on an overlay or the command line
func (a *App) pausedForInput() bool {
	m := a.ctx.GetMode()
	return a.ctx.TimeCtl.OwnPause() && (m == core.ModeOverlay || m == core.ModeCommand)
}

// reportJournalClosed tells this run's player how its own journal stop went.
func (a *App) reportJournalClosed(path string, err error) {
	if err != nil {
		a.ctx.SetStatusMessage("Journal: "+err.Error(), parameter.StatusAlert)
		return
	}
	a.ctx.SetStatusMessage("Journal closed: "+path, parameter.StatusNotice)
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

// openJournal starts a journal where this run stands, without replacing the run. A
// replay of it boots afresh and writes the opening's world and own side over that
// boot, and the run takes on what the replay then holds, which is the run as it
// stood. Call it outside the world lock, from the goroutine that owns the run's loop.
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
	if err := lineHolds(opening.Body, opening.Own); err != nil {
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
		// Past its first write the run holds part of the twin's world: a defect to find
		a.log.Error("journal", "msg", "run may hold part of an opening it does not match", "error", err.Error())
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
// in, written against twin's settled boot, with the operator state and the run's own
// side beside it. Caller MUST hold updateMutex.
func (a *App) openingLocked(twin *App) (event.JournalCapture, error) {
	cap, err := a.captureSharedLocked()
	if err != nil {
		return event.JournalCapture{}, err
	}
	own, err := a.ownStateLocked()
	if err != nil {
		return event.JournalCapture{}, err
	}
	ownBody, err := snapshot.EncodeJSON(own)
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
		Participant: a.world.LocalParticipant(), Authority: cap.Header.Authority,
		Barrier: a.barrierLocked(), Body: body,
		Opens: true, Mode: a.ctx.GetMode(), Paused: a.ctx.TimeCtl.OwnPause(), Held: a.ctx.TimeCtl.Held(), Off: off,
		Slot: a.world.Resources.Player.LocalSlot(), Own: ownBody,
	}, nil
}

// ownState is the run's own side, which a journal's opening carries beside the shared
// world so its replay plays on as this participant rather than as a fresh boot of
// one: the player world, the player's resources, each system's own state, where the
// corpus walk stands, the event tallies, every status cell but this instance's own
// (ownedStatusKey), and the order the run holds both domains' entities in.
type ownState struct {
	World   engine.PlayerWorldState      `json:"world"`
	Side    engine.PlayerSide            `json:"side"`
	Systems []snapshot.SystemStateRecord `json:"systems,omitempty"`
	Content content.CursorState          `json:"content"`
	Events  event.QueueTelemetry         `json:"events"`
	Status  snapshot.StatusState         `json:"status"`
	Order   engine.WorldOrder            `json:"order"`
}

// ownStateLocked reads the run's own side. Caller MUST hold updateMutex.
func (a *App) ownStateLocked() (ownState, error) {
	own := ownState{
		World:   a.world.CapturePlayerWorld(),
		Side:    a.world.CapturePlayerSide(),
		Content: service.MustGet[*service.ContentService](a.hub, "content").CursorState(),
		Events:  a.world.Resources.Event.Queue.Telemetry(),
		Status:  a.statusCellsLocked(a.statusKeysLocked(func(k string) bool { return !ownedStatusKey(k) })),
		Order:   a.world.CaptureOrder(),
	}
	own.Side.Streams = slices.DeleteFunc(own.Side.Streams, ownedStream)
	for _, sys := range a.world.Systems() {
		s, ok := sys.(engine.LocalStateSaver)
		if !ok || slices.Contains(ownedSystems, sys.Name()) {
			continue
		}
		data, err := s.SaveLocal()
		if err != nil {
			return ownState{}, fmt.Errorf("%s: %w", sys.Name(), err)
		}
		own.Systems = append(own.Systems, snapshot.SystemStateRecord{System: sys.Name(), Data: data})
	}
	return own, nil
}

// installOwnLocked writes the run's own side over a replay's opening world, after
// the settle that bound its input, so nothing that settle derived for the boot's
// player stands in for it. Caller MUST hold updateMutex.
func (a *App) installOwnLocked(own ownState) error {
	a.world.InstallPlayerWorld(own.World)
	if unknown := a.world.InstallPlayerSide(own.Side); len(unknown) > 0 {
		return fmt.Errorf("RNG streams this replay does not issue: %v", unknown)
	}
	savers := make(map[string]engine.LocalStateSaver, len(own.Systems))
	for _, sys := range a.world.Systems() {
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
	if err := service.MustGet[*service.ContentService](a.hub, "content").SetCursorState(own.Content); err != nil {
		return err
	}
	a.world.Resources.Event.Queue.SetTelemetry(own.Events)
	a.installStatusLocked(own.Status)
	// Last, once both domains' rows stand: the shared write and the settle placed theirs
	if err := a.world.InstallOrder(own.Order); err != nil {
		return fmt.Errorf("the run's order: %w", err)
	}
	return nil
}

// adoptLocked takes on twin's simulated state where its driver d stands, as a
// replay copy restored from a checkpoint does, and refuses a run that does not then
// hold twin's world. What stays is this instance's own: the terminal's colour, the
// wall clock and pacing, the pointer placement it crosses next tick, and the
// ownedSystems with their streams and status. Caller MUST hold updateMutex.
func (a *App) adoptLocked(twin *App, d *journal.ReplayDriver) error {
	var (
		c   *checkpoint
		err error
	)
	twin.world.RunSafe(func() { c, err = twin.checkpointLocked(d, nil) })
	if err != nil {
		return err
	}
	if err := service.MustGet[*service.ContentService](a.hub, "content").SetCursorState(c.content); err != nil {
		return err
	}
	cfg := a.world.Resources.Config
	color, palette, wall := cfg.ColorMode, cfg.ConsolePalette, a.world.Resources.Time.RealTime
	held := *a.world.Resources.Player
	kept := slices.DeleteFunc(a.world.Resources.Rand.SaveStreams(core.DomainPlayer), func(s engine.StreamState) bool { return !ownedStream(s) })
	a.world.CopyIn(c.world)
	cfg.ColorMode, cfg.ConsolePalette, a.world.Resources.Time.RealTime = color, palette, wall
	a.world.Resources.Player.KeepPointer(&held)
	if unknown := c.world.LoadStreams(a.world.Resources.Rand); len(unknown) > 0 {
		return fmt.Errorf("RNG streams this run does not issue: %v", unknown)
	}
	a.world.Resources.Rand.LoadStreams(core.DomainPlayer, kept)
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
		if err := loadLocalState(sys, c.local[sys.Name()]); err != nil {
			return fmt.Errorf("adopt %s: %w", sys.Name(), err)
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
