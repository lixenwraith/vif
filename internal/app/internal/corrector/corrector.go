// Package corrector is the correction glue: a run's shared world as the authority
// protocol reads and writes it, over the run's world and the capture encoder. It
// captures, verifies, stages, projects and installs, reads the run through Run
// alone, and resolves a capture into a second run, Staging, which only the run can
// build. See doc/multi-player.md §3.3.
package corrector

import (
	"fmt"
	"sync"

	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/service"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/internal/vlog"
)

// Corrector is the glue over one run's world. What a capture, a staged install and
// a projection need of the run is the width of Run.
type Corrector struct {
	run Run

	world     *engine.World
	scheduler *engine.Scheduler // wired when the run builds its own, after the world
	log       *vlog.Log
	content   *service.ContentService
	scenario  *resource.Scenario

	// telemetry is reserved during construction so a capture or an install can
	// publish its cost into a registry that is frozen by then.
	telemetry snapshot.Telemetry
	encoder   snapshot.Encoder // capture encodings the seal, the index and the journal share

	sharedStatus StatusKeys // the registry keys a capture carries; world lock

	// staging is the second world a capture resolves into, built on first use and
	// re-used for the life of the run: building one per install costs 9 to 31 ms,
	// which a correction five times a second cannot afford.
	stageMu   sync.Mutex
	staging   Staging
	stagedFor stagingKey
}

// Run is what the glue reads of the run beyond its world: where the run stands,
// whose authority a capture is read under, which session it is in, the journal it
// reports a lost world to, and the second world a capture resolves into, which only
// the run can build from its own configuration.
type Run interface {
	Position() event.Stamp
	AuthorityStamp() (network.AuthorityTerm, uint32)
	SessionAnchorFields(event.JournalAnchor) []AnchorField
	AssignedParticipant() uint32
	BarrierLocked() bool
	LandingHold() (held, start bool)
	JournalLostLocked(*event.Journal, error)
	NewStaging(snapshot.SharedCapture) (Staging, error)
}

// Staging is the second world a capture resolves into: a headless run of the same
// seed, scenario and corpus, read through its own glue.
type Staging interface {
	Corrector() *Corrector
	Settle()
	Tick(n int)
	Close()
}

// Open wires the glue over the run's world once the status registry its telemetry
// publishes into exists.
func (c *Corrector) Open(run Run, w *engine.World, log *vlog.Log, content *service.ContentService, scenario *resource.Scenario) {
	c.run, c.world, c.log, c.content, c.scenario = run, w, log, content, scenario
	c.telemetry = snapshot.NewTelemetry(w.Resources.Status)
}

// SetScheduler wires the run's scheduler, which it builds after the world.
func (c *Corrector) SetScheduler(s *engine.Scheduler) { c.scheduler = s }

// Telemetry is the capture and install cost cells the protocol publishes into.
func (c *Corrector) Telemetry() snapshot.Telemetry { return c.telemetry }

// AnchorField is one value an anchor names and this run must reproduce.
type AnchorField struct {
	Name      string
	Want, Got any
}

// FirstAnchorMismatch reports the first field this run does not reproduce.
func FirstAnchorMismatch(kind string, fields []AnchorField) error {
	for _, f := range fields {
		if f.Want != f.Got {
			return fmt.Errorf("%s mismatch: %s recorded %v, this run has %v", kind, f.Name, f.Want, f.Got)
		}
	}
	return nil
}

// IndexSharedLocked indexes the live world under its lock against the authority
// and term a manifest names, copying nothing but the index's own.
func (c *Corrector) IndexSharedLocked(authority uint32, term network.AuthorityTerm) (*snapshot.Manifest, snapshot.SharedCapture, error) {
	light, err := c.CaptureLightLocked()
	if err != nil {
		return nil, snapshot.SharedCapture{}, err
	}
	light.Header.Term = term
	index, err := c.encoder.BuildLive(c.world, light, authority)
	return index, light, err
}

// InstallCapture resolves a capture against the staging world, projects it to this
// instance's tick and commits it between two ticks, reporting how far this instance
// had drifted from the projection.
func (c *Corrector) InstallCapture(cap snapshot.SharedCapture) (engine.WorldDifference, error) {
	staged, err := c.stageProved(cap)
	if err != nil {
		return engine.WorldDifference{}, err
	}
	if err := staged.Commit(); err != nil {
		return engine.WorldDifference{}, err
	}
	return staged.Difference(), nil
}

func (c *Corrector) Encoder() *snapshot.Encoder { return &c.encoder }

// AdoptAuthority takes a header whose world this instance already holds: the
// barrier's fences move to it and the ledger is settled against this world, which
// the authority has just proved is its own.
func (c *Corrector) AdoptAuthority(h snapshot.CaptureHeader) {
	c.world.RunSafe(func() { c.adoptSnapshotBarrierLocked(h) })
	c.confirmPredictions(h.Tick, c.world)
	c.world.RunSafe(func() { c.telemetry.InstallTick.Store(int64(h.Tick)) })
}
