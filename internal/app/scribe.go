package app

import (
	"github.com/lixenwraith/vif/internal/app/internal/corrector"
	"github.com/lixenwraith/vif/internal/app/internal/scribe"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/vlog"
)

// scribeRun is the run as its journal reads it: the scribe.Run seam, answered by the
// membership, the run's configuration and the twin it builds.
type scribeRun struct{ a *App }

func (r scribeRun) Position() event.Stamp          { return r.a.Position() }
func (r scribeRun) Settle()                        { r.a.Settle() }
func (r scribeRun) BarrierLocked() bool            { return r.a.membership.barrierLocked() }
func (r scribeRun) JournalSink() event.JournalSink { return r.a.cfg.JournalSink }
func (r scribeRun) AskSeatJournal(slot int, start bool) error {
	return r.a.membership.askSeatJournal(slot, start)
}
func (r scribeRun) SeatJournalSummaries() []string { return r.a.membership.seatJournalSummaries() }
func (r scribeRun) Replacing() bool                { return r.a.replacing() }

func (r scribeRun) JournalTwin(an event.JournalAnchor) (scribe.Twin, error) {
	twin, err := r.a.journalTwin(an)
	if err != nil {
		return nil, err
	}
	return twinRun{twin}, nil
}

// twinRun is a headless replay standing as the boot a journal's opening is written
// against, read through its own scribe.
type twinRun struct{ *App }

func (t twinRun) Scribe() *scribe.Scribe { return &t.scribe }
func (t twinRun) ReplayDriver(s *journal.Stream) (*journal.ReplayDriver, error) {
	return newReplayDriver(t.App, s)
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

// The anchor and the lost journal stay the run's for the glue and the membership,
// which read the run.
func (a *App) VerifyAnchor(an event.JournalAnchor) error { return a.scribe.VerifyAnchor(an) }
func (a *App) buildAnchor() event.JournalAnchor          { return a.scribe.BuildAnchor() }
func (a *App) journalLostLocked(j *event.Journal, err error) {
	a.scribe.JournalLostLocked(j, err)
}
func (a *App) sessionAnchorFields(an event.JournalAnchor) []corrector.AnchorField {
	return a.scribe.SessionAnchorFields(an)
}
