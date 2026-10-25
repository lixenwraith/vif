package app

import (
	"sync"

	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/service"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/internal/vlog"
)

// corrector is the correction glue: the shared world as the authority protocol reads
// and writes it, over the world and the capture encoder. It reads the run it corrects
// through correctionRun and nothing else, so what a capture, a staged install and a
// projection need of a run is the width of that interface.
type corrector struct {
	run correctionRun

	world     *engine.World
	scheduler *engine.Scheduler // wired when the run builds its own, after the world
	log       *vlog.Log
	content   *service.ContentService
	scenario  *resource.Scenario

	// telemetry is reserved during construction so a capture or an install can
	// publish its cost into a registry that is frozen by then.
	telemetry snapshot.Telemetry
	encoder   snapshot.Encoder // capture encodings the seal, the index and the journal share

	sharedStatus statusKeys // the registry keys a capture carries; world lock

	// staging is the second world a capture resolves into before it is written into
	// this one, built on first use and re-used for the life of the run: building one
	// per install costs 9 to 31 ms, which a correction five times a second cannot
	// afford.
	stageMu   sync.Mutex
	staging   *App
	stagedFor stagingKey
}

// correctionRun is what the glue reads of the run beyond its world: where the run
// stands, whose authority a capture is read under, which session it is in, the
// journal it reports a lost world to, and the second world a capture resolves into,
// which only the run can build from its own configuration.
type correctionRun interface {
	Position() event.Stamp
	authorityStamp() (network.AuthorityTerm, uint32)
	sessionAnchorFields(event.JournalAnchor) []anchorField
	assignedParticipant() uint32
	barrierLocked() bool
	landingHold() (held, start bool)
	journalLostLocked(*event.Journal, error)
	newStagingApp(snapshot.SharedCapture) (*App, error)
}

// open wires the glue over the run's world once the status registry its telemetry
// publishes into exists.
func (c *corrector) open(run correctionRun, w *engine.World, log *vlog.Log, content *service.ContentService, scenario *resource.Scenario) {
	c.run, c.world, c.log, c.content, c.scenario = run, w, log, content, scenario
	c.telemetry = snapshot.NewTelemetry(w.Resources.Status)
}
