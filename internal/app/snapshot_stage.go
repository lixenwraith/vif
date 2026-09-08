// The staged install.
//
// InstallShared writes into the world it is called on. That is the right shape for a
// harness, which owns both worlds and ticks neither, and the wrong one for a join:
// the instance being installed into is running, and a capture that turns out to be
// unloadable halfway through would leave it holding a world that is neither its own
// nor the session's.
//
// A stage resolves the whole capture into a second world first — a real one, with
// this build's system set, its FSM and its RNG stream inventory — and only then
// writes the same bytes into the live world, between two ticks. What survives the
// staging pass is what the live pass cannot fail on: identical code, identical input,
// and no dependence on the state being written over.
//
// Cost bounds the design. Building a second App per install costs 9 to 31 ms, which
// suits a join that happens once and not a correction five times a second, so the
// staging world is built on first use and re-used for the life of the run. Commit
// reconciles the live world onto the capture rather than clearing and re-inserting
// it, so it writes the size of the correction, not of the world.

package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/lifecycle"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// StagedInstall is a capture that has been resolved against a second world and is
// waiting for its tick boundary. Nothing in the live world has been touched.
//
// The handle borrows the staging world and must release it — Commit does that on
// the way out, Discard on the way out of a join that failed for some other reason.
// Releasing hands the world back to the run rather than closing it: it is built
// once and every later correction resolves into the same one.
type StagedInstall struct {
	live    *App
	staging *App
	capture snapshot.SharedCapture

	stageDur  time.Duration
	commitDur time.Duration
	committed bool
	discarded bool

	// difference is how far the live world had drifted from the capture when the
	// commit wrote it. On a guest that is the correction magnitude — the distance
	// between what this instance predicted and what the host actually had — and it
	// is telemetry rather than an error (weakened D-11).
	difference engine.WorldDifference
}

// StageShared resolves a capture into a second world without touching this one.
//
// The order is deliberate. Identity and integrity are checked against the *live*
// instance, because those are questions about whether this participant is in the
// sender's session at all and a staging world built from the same config would
// answer them the same way twice. Everything after that is a question about
// whether the capture can be loaded by this build — carrier names, stream names,
// FSM regions, every carrier's own decode — and that is what the second world is
// for.
func (a *App) StageShared(cap snapshot.SharedCapture) (*StagedInstall, error) {
	started := time.Now() // [wall] telemetry only; the install carries no instant
	if err := a.VerifyCapture(cap); err != nil {
		return nil, err
	}

	staging, fresh, err := a.stagingWorld(cap)
	if err != nil {
		return nil, fmt.Errorf("stage: %w", err)
	}
	if fresh {
		// The FSM boot script's queued spawn is what declares the cursor template a
		// late arrival is created from, and it is still queued: the machine enters
		// its boot state inside New and nothing has ticked. Settling it here makes
		// the staging world the same shape as the instance it stands in for — a
		// joiner settles the same queue before it installs, for the same reason.
		// A re-used staging world has settled it already and has never ticked
		// since, so there is nothing queued to settle a second time.
		staging.Settle()
	}
	if err := staging.installSharedResolved(cap); err != nil {
		a.discardStagingWorld()
		return nil, fmt.Errorf("stage: %w", err)
	}

	st := &StagedInstall{live: a, staging: staging, capture: cap, stageDur: time.Since(started)}
	vlog.Info("app", "msg", "capture staged",
		"tick", cap.Header.Tick, "streams", len(cap.Streams), "systems", len(cap.Systems),
		"stage_ms", st.stageDur.Milliseconds())
	return st, nil
}

// Tick names the tick the staged capture describes.
func (s *StagedInstall) Tick() uint64 { return s.capture.Header.Tick }

// Capture returns the staged capture, for a caller that has to answer the host
// about what it installed.
func (s *StagedInstall) Capture() snapshot.SharedCapture { return s.capture }

// StagingWorld exposes the resolved second world, for a test that wants to compare
// it against the live one before the swap. It is invalid after Commit or Discard.
func (s *StagedInstall) StagingWorld() *App { return s.staging }

// Commit writes the staged capture into the live world and releases the staging
// world. World.RunSafe holds the update mutex, and a tick runs entirely inside one
// acquisition of it, so a commit is between two ticks by construction rather than
// by a scheduler handshake.
//
// A failure here is not a rejected capture: the same bytes loaded into the same
// build a moment ago. It is reported as the inconsistency it is, and the live world
// is left holding whatever the partial write reached — there is nothing better to
// do, and pretending otherwise would hide it.
func (s *StagedInstall) Commit() error {
	switch {
	case s.committed:
		return errors.New("staged install already committed")
	case s.discarded:
		return errors.New("staged install already discarded")
	}
	var err error
	started := time.Now() // [wall] telemetry only
	s.difference, err = s.live.reconcileSharedResolved(s.capture)
	s.commitDur = time.Since(started)
	s.committed = true
	s.release()
	if err != nil {
		vlog.Error("app", "msg", "staged capture failed its live install",
			"tick", s.capture.Header.Tick, "error", err.Error())
		return fmt.Errorf("commit a staged capture: %w", err)
	}
	s.live.world.RunSafe(func() {
		m := s.live.telemetry
		m.StageUS.Store(s.stageDur.Microseconds())
		m.CommitUS.Store(s.commitDur.Microseconds())
		m.InstallTick.Store(int64(s.capture.Header.Tick))
	})
	vlog.Info("app", "msg", "capture installed",
		"tick", s.capture.Header.Tick,
		"stage_ms", s.stageDur.Milliseconds(), "commit_ms", s.commitDur.Milliseconds(),
		"correction_entries", s.difference.Entries,
		"correction_entities", s.difference.Entities,
		"correction_cells", s.difference.CellShift)
	return nil
}

// Difference is how far the live world had drifted from the capture at the moment
// it was committed: the correction magnitude, valid after Commit.
func (s *StagedInstall) Difference() engine.WorldDifference { return s.difference }

// Discard releases the staging world without writing anything.
func (s *StagedInstall) Discard() {
	if s.committed || s.discarded {
		return
	}
	s.discarded = true
	s.release()
}

// stagingWorld returns the second world captures resolve into, building it the first
// time and re-using it after. The second return says whether it was just built,
// which decides whether its FSM boot queue still needs settling.
//
// A staging world that kept anything from the previous install — a carrier that
// merged rather than replaced, an entity a store did not drop — would resolve the
// next capture against a world the sender never had;
// TestStagingWorldIsBuiltOnceAndReused holds that. What it cannot re-use is a world
// built on different bounds: the D-14 map latch decides what the level setup reflows
// and what a capture's placements mean.
func (a *App) stagingWorld(cap snapshot.SharedCapture) (*App, bool, error) {
	a.stageMu.Lock()
	defer a.stageMu.Unlock()

	if a.staging != nil {
		if a.stagingW == cap.Header.MapWidth && a.stagingH == cap.Header.MapHeight {
			return a.staging, false, nil
		}
		a.staging.Close()
		a.staging = nil
	}
	staging, err := a.newStagingApp(cap)
	if err != nil {
		return nil, false, err
	}
	a.staging = staging
	a.stagingW, a.stagingH = cap.Header.MapWidth, cap.Header.MapHeight
	return staging, true, nil
}

// discardStagingWorld throws away a staging world a capture failed to resolve into.
//
// A failed load may have written part of itself, so the world is no longer a
// faithful stand-in for this instance and the next correction must not be resolved
// against it. This is the one path that closes one before the run ends.
func (a *App) discardStagingWorld() {
	a.stageMu.Lock()
	staging := a.staging
	a.staging, a.stagingW, a.stagingH = nil, 0, 0
	a.stageMu.Unlock()
	if staging != nil {
		staging.Close()
	}
}

// closeStagingWorld releases the run's staging world. Called from Close.
func (a *App) closeStagingWorld() { a.discardStagingWorld() }

// Timings reports what the two halves cost, for choosing the cadence.
func (s *StagedInstall) Timings() (stage, commit time.Duration) { return s.stageDur, s.commitDur }

// release hands the staging world back. It is not closed: the world is the run's,
// built once and re-used by every later install, and closing it here is what made
// a correction pay for a construction.
func (s *StagedInstall) release() { s.staging = nil }

// newStagingApp builds the second world a capture is resolved into.
//
// It is this instance's own configuration with every outward-facing part removed:
// no transport (it would dial or bind a second time), no journal (it would record a
// run that never happened), no recorder or status cadence (they are telemetry about
// a world nobody plays). What it keeps is what decides whether a capture loads —
// the seed, the FSM config, the corpus, and therefore the whole system set.
//
// The map latch comes from the capture rather than from this instance: the FSM boot
// spawns cursor slot zero centred on the map inside New, and a staging world built
// on different bounds would reject nothing but would answer a different question
// from the one being asked.
func (a *App) newStagingApp(cap snapshot.SharedCapture) (*App, error) {
	cfg := a.cfg
	cfg.Mode = ModeHeadless
	cfg.Journal = false
	cfg.JournalSink = nil
	cfg.HostAddress, cfg.JoinAddress = "", ""
	cfg.networkConfig = nil
	cfg.scriptedSession = false
	cfg.Participants = 0
	// A staging world is not a supervised session and is not an allocated one: it
	// answers no probe and nothing may end the live run because a capture resolved
	// into it. Both are refused outright by a non-serving mode, so leaving them set
	// would make a correction on a dedicated host fail to stage at all.
	cfg.ProbeAddress = ""
	cfg.Lifetime = lifecycle.Policy{}
	cfg.TimeScaleSpec = ""
	cfg.RecTicks = -1
	cfg.StatTicks = -1
	cfg.LockMap = true
	if cap.Header.MapWidth > 0 && cap.Header.MapHeight > 0 {
		cfg.MapWidth, cfg.MapHeight = cap.Header.MapWidth, cap.Header.MapHeight
	}
	// CropOnResize is not in the capture: it decides how *this* instance answers a
	// resize, which a staging world never receives. It is copied from the live
	// world rather than from the flag so the staging world reports the same context
	// record, which is what a comparison against it is for.
	a.world.RunSafe(func() { cfg.CropOnResize = a.world.Resources.Config.CropOnResize })
	// A live run owns the terminal; the staging world only needs a viewport large
	// enough to hold the latched map, which is what the live instance already runs.
	cfg.Width, cfg.Height = a.ctx.Width, a.ctx.Height
	return NewHeadless(cfg)
}

// installSharedResolved is InstallShared without the identity check.
//
// The live instance answers "is this my session" once, in StageShared. The staging
// world is built from that same instance's configuration, so asking it again would
// re-derive the same verdict from the same inputs — and would fail outright after a
// reset, whose session counter a freshly constructed world has not reached.
func (a *App) installSharedResolved(cap snapshot.SharedCapture) error {
	// The staging world proves that the position resolves; it does not present or
	// simulate this participant's local effects. Reconciliation belongs only to
	// the live commit, where its emitted lifecycle events can reach those systems.
	return a.installShared(cap, false)
}

// reconcileSharedResolved is the live half of a staged install: the same capture,
// already proved loadable by the staging pass, written onto the world this instance
// is holding rather than over the top of it.
func (a *App) reconcileSharedResolved(cap snapshot.SharedCapture) (engine.WorldDifference, error) {
	return a.reconcileShared(cap)
}
