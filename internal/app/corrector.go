package app

import (
	"github.com/lixenwraith/vif/internal/app/internal/corrector"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/internal/vlog"
)

// correctionRun is the run as its correction glue reads it: the corrector.Run seam,
// answered by the membership, the scribe and the run's own configuration.
type correctionRun struct{ a *App }

func (r correctionRun) Position() event.Stamp { return r.a.Position() }
func (r correctionRun) AuthorityStamp() (network.AuthorityTerm, uint32) {
	return r.a.membership.authorityStamp()
}
func (r correctionRun) SessionAnchorFields(an event.JournalAnchor) []corrector.AnchorField {
	return r.a.sessionAnchorFields(an)
}
func (r correctionRun) AssignedParticipant() uint32     { return r.a.membership.assignedParticipant() }
func (r correctionRun) BarrierLocked() bool             { return r.a.membership.barrierLocked() }
func (r correctionRun) LandingHold() (held, start bool) { return r.a.membership.landingHold() }
func (r correctionRun) JournalLostLocked(j *event.Journal, err error) {
	r.a.journalLostLocked(j, err)
}

func (r correctionRun) NewStaging(cap snapshot.SharedCapture) (corrector.Staging, error) {
	staging, err := r.a.newStagingApp(cap)
	if err != nil {
		return nil, err
	}
	return stagingRun{staging}, nil
}

// stagingRun is a headless run standing in for this one as the world a capture
// resolves into, read through its own glue.
type stagingRun struct{ *App }

func (s stagingRun) Corrector() *corrector.Corrector { return &s.glue }

// newStagingApp builds the second world a capture is resolved into: this instance's
// configuration with every outward-facing part removed — no transport, no journal,
// no telemetry cadence — keeping what decides whether a capture loads, which is the
// seed, the scenario and the corpus. The map latch and the session come from the
// capture: a world on other bounds or another session's roots answers another question.
func (a *App) newStagingApp(cap snapshot.SharedCapture) (*App, error) {
	// Only inputs that can change the simulated world are projected: subtracting I/O
	// options from the live Config lets a new local option reach the staging world.
	// Dir stays, since the corpus and FSM files resolve through it. The scenario is
	// handed over, not re-resolved: it is what the capture was produced against,
	// and it may have come from the coordinator and exist nowhere on this host.
	staged := a.scenario
	cfg := Config{
		Mode: ModeHeadless,
		Resources: resource.Options{
			Dir:      a.cfg.Resources.Dir,
			Content:  a.cfg.Resources.Content,
			Embedded: a.cfg.Resources.Embedded,
			Provided: &staged,
		},
		Seed:      a.cfg.Seed,
		Session:   cap.Header.Session,
		RecTicks:  -1,
		StatTicks: -1,
		LockMap:   true,
	}
	if cap.Header.MapWidth > 0 && cap.Header.MapHeight > 0 {
		cfg.MapWidth, cfg.MapHeight = cap.Header.MapWidth, cap.Header.MapHeight
	} else {
		cfg.MapWidth, cfg.MapHeight = a.cfg.MapWidth, a.cfg.MapHeight
	}
	// CropOnResize is not in the capture: it decides how *this* instance answers a
	// resize, which a staging world never receives. It is copied from the live
	// world rather than from the flag so the staging world reports the same context
	// record, which is what a comparison against it is for.
	a.world.RunSafe(func() { cfg.CropOnResize = a.world.Resources.Config.CropOnResize })
	// A live run owns the terminal; the staging world only needs a viewport large
	// enough to hold the latched map, which is what the live instance already runs.
	cfg.Width, cfg.Height = a.ctx.Width, a.ctx.Height
	staging, err := newDriven(runConfig{Config: cfg, wiring: wiring{log: vlog.NewLog("staging")}})
	if err == nil {
		staging.world.Project()
	}
	return staging, err
}

// CaptureShared, InstallShared, StageShared and VerifyCapture are the glue's read,
// install and verdict as the run's own API, kept while anything reads them through it.
func (a *App) CaptureShared() (snapshot.SharedCapture, error) { return a.glue.CaptureShared() }
func (a *App) InstallShared(cap snapshot.SharedCapture) error { return a.glue.InstallShared(cap) }
func (a *App) StageShared(cap snapshot.SharedCapture) (*corrector.StagedInstall, error) {
	return a.glue.StageShared(cap)
}
func (a *App) VerifyCapture(cap snapshot.SharedCapture) error { return a.glue.VerifyCapture(cap) }
