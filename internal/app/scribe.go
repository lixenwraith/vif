package app

import (
	"sync/atomic"

	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/service"
	"github.com/lixenwraith/vif/internal/vlog"
)

// scribe keeps the run's journal and reads one back: it opens and closes the journal,
// writes the world a run stands in and the anchor that names it, verifies an anchor
// against this run, installs a written world as a replay, and reads and restores
// the checkpoints a rewind moves between. It depends on the journal package and the
// world, not on a session, and reads the run through scribeRun alone.
type scribe struct {
	run scribeRun

	world     *engine.World
	ctx       *engine.GameContext
	cfg       *Config
	log       *vlog.Log
	scheduler *engine.Scheduler // wired when the run builds its own, after the world
	scenario  *resource.Scenario
	glue      *corrector
	hub       *service.Hub

	// recorder is the journal this run keeps, nil when it keeps none; atomic, as a
	// holder reads its seats' from its own goroutine. journalAsk is a start or stop
	// the command line asked for that the run's loop has not served yet. closedAtExit
	// is the journal Close shut and why its tail may be lost, which a seat's holder
	// reports on the same goroutine.
	recorder       atomic.Pointer[journal.Recorder]
	journalAsk     atomic.Pointer[journalAsk]
	closedAtExit   string
	closedAtExitAs error
}

// scribeRun is what the scribe reads of the run beyond its world: where the run
// stands and how it settles, whether its barrier owns its crossings, the twin a
// journal opens against, the seats whose journals it starts, stops and names, and
// whether a run to replace this one is latched.
type scribeRun interface {
	Position() event.Stamp
	Settle()
	barrierLocked() bool
	journalTwin(an event.JournalAnchor) (*App, error)
	askSeatJournal(slot int, start bool) error
	seatJournalSummaries() []string
	replacing() bool
}

// open wires the scribe over the run's configuration, scenario and services before
// the run builds anything; the world, the context and the scheduler follow.
func (sc *scribe) open(run scribeRun, cfg *Config, scenario *resource.Scenario, log *vlog.Log, hub *service.Hub, glue *corrector) {
	sc.run, sc.cfg, sc.scenario, sc.log, sc.hub, sc.glue = run, cfg, scenario, log, hub, glue
}

// The anchor and the lost journal stay the run's for the glue and the membership,
// which read the run; the seats' journals are the membership's.
func (a *App) VerifyAnchor(an event.JournalAnchor) error { return a.scribe.VerifyAnchor(an) }
func (a *App) buildAnchor() event.JournalAnchor          { return a.scribe.buildAnchor() }
func (a *App) journalLostLocked(j *event.Journal, err error) {
	a.scribe.journalLostLocked(j, err)
}
func (a *App) sessionAnchorFields(an event.JournalAnchor) []anchorField {
	return a.scribe.sessionAnchorFields(an)
}
func (a *App) askSeatJournal(slot int, start bool) error {
	return a.membership.askSeatJournal(slot, start)
}
func (a *App) seatJournalSummaries() []string { return a.membership.seatJournalSummaries() }
