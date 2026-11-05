// Package scribe keeps a run's journal and reads one back: it opens and closes the
// journal, writes the world the run stands in and the anchor that names it, verifies
// an anchor against the run, writes a journal's opening over a replay, and reads and
// restores the checkpoints a rewind moves between. It reads the run through Run
// alone, and opens a journal against a Twin, a replay only the run can build.
package scribe

import (
	"sync/atomic"

	"github.com/lixenwraith/vif/internal/app/internal/corrector"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/service"
	"github.com/lixenwraith/vif/internal/vlog"
)

// Scribe is the journal of one run, over its world and the journal package rather
// than a session.
type Scribe struct {
	run Run

	world     *engine.World
	ctx       *engine.GameContext
	log       *vlog.Log
	scheduler *engine.Scheduler // wired when the run builds its own, after the world
	scenario  *resource.Scenario
	glue      *corrector.Corrector
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

// Run is what the scribe reads of the run beyond its world: where the run stands
// and how it settles, whether its barrier owns its crossings, where a journal is
// written, the twin a journal opens against, the seats whose journals it starts,
// stops and names, and whether a run to replace this one is latched.
type Run interface {
	Position() event.Stamp
	Settle()
	BarrierLocked() bool
	JournalSink() event.JournalSink
	JournalTwin(an event.JournalAnchor) (Twin, error)
	AskSeatJournal(slot int, start bool) error
	SeatJournalSummaries() []string
	Replacing() bool
}

// Twin is a replay of a journal opening on this run's anchor, its boot settled: the
// world the opening is written against, read through its own scribe and driven by
// the replay driver the run builds over it.
type Twin interface {
	Scribe() *Scribe
	ReplayDriver(*journal.Stream) (*journal.ReplayDriver, error)
	Close()
}

// Open wires the scribe over the run's scenario and services before the run builds
// anything; the world, the context and the scheduler follow.
func (sc *Scribe) Open(run Run, scenario *resource.Scenario, log *vlog.Log, hub *service.Hub, glue *corrector.Corrector) {
	sc.run, sc.scenario, sc.log, sc.hub, sc.glue = run, scenario, log, hub, glue
}

// Bind wires the run's world and its game context once both exist.
func (sc *Scribe) Bind(w *engine.World, ctx *engine.GameContext) { sc.world, sc.ctx = w, ctx }

// SetScheduler wires the run's scheduler, which it builds after the world.
func (sc *Scribe) SetScheduler(s *engine.Scheduler) { sc.scheduler = s }

// Recorder is the journal this run keeps, nil when it keeps none.
func (sc *Scribe) Recorder() *journal.Recorder { return sc.recorder.Load() }

// CloseAtExit closes the journal as the run closes, keeping what ClosedAtExit says.
func (sc *Scribe) CloseAtExit() { sc.closedAtExit, sc.closedAtExitAs = sc.CloseJournal() }

// ClosedAtExit is the journal CloseAtExit shut, "" with none, and why its tail may
// not have reached it.
func (sc *Scribe) ClosedAtExit() (string, error) { return sc.closedAtExit, sc.closedAtExitAs }
