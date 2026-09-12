package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/resource"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// StagedInstall is a capture resolved against a second world and waiting for its
// tick boundary; nothing in the live world has been touched. The handle borrows the
// staging world and must release it — Commit on the way out, Discard otherwise —
// which hands it back to the run rather than closing it, because every later
// correction resolves into the same one.
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
// Identity and integrity are checked against the live instance — they ask whether
// this participant is in the sender's session at all — and everything after that
// asks whether this build can load the capture, which is what the second world is
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
		// The FSM boot script's queued spawn declares the cursor template a late
		// arrival is created from, and nothing has ticked yet. Settling it makes the
		// staging world the same shape as the instance it stands in for; a re-used
		// one has settled it already and never ticked since.
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
// world. A tick runs entirely inside one acquisition of the update mutex, so a
// commit that takes it is between two ticks by construction. A failure here is not a
// rejected capture but an inconsistency — the same bytes loaded into the same build
// a moment ago — so it is reported rather than hidden.
func (s *StagedInstall) Commit() error {
	switch {
	case s.committed:
		return errors.New("staged install already committed")
	case s.discarded:
		return errors.New("staged install already discarded")
	}
	var err error
	started := time.Now() // [wall] telemetry only
	s.difference, err = s.live.reconcileShared(s.capture)
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
// time and re-using it after; the second return says whether its FSM boot queue
// still needs settling. A world that kept anything from the previous install would
// resolve the next capture against a world the sender never had. Different map
// bounds cannot be re-used: the D-14 latch decides what a capture's placements mean.
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

// Timings reports what the two halves cost, for choosing the cadence.
func (s *StagedInstall) Timings() (stage, commit time.Duration) { return s.stageDur, s.commitDur }

// release hands the staging world back. It is not closed: the world is the run's,
// built once and re-used by every later install, and closing it here is what made
// a correction pay for a construction.
func (s *StagedInstall) release() { s.staging = nil }

// newStagingApp builds the second world a capture is resolved into: this instance's
// configuration with every outward-facing part removed — no transport, no journal,
// no telemetry cadence — keeping what decides whether a capture loads, which is the
// seed, the FSM config and the corpus. The map latch comes from the capture, because
// a world built on different bounds would answer a different question.
func (a *App) newStagingApp(cap snapshot.SharedCapture) (*App, error) {
	// Project only the inputs that can change the simulated world. Starting from
	// the live Config and subtracting known I/O options is brittle: a newly added
	// local option can otherwise reach NewHeadless and either alter staging or be
	// rejected as unused. That is how an explicit guest colour mode used to abort
	// join and every later correction; audio overrides had the same latent path.
	//
	// Dir remains part of the simulation resource set because installed game names,
	// corpus discovery and files referenced by the FSM resolve through it. Keymap,
	// music and sounds belong to the live instance's input and audio services.
	cfg := Config{
		Mode: ModeHeadless,
		Resources: resource.Options{
			Dir:      a.cfg.Resources.Dir,
			Game:     a.cfg.Resources.Game,
			Content:  a.cfg.Resources.Content,
			Embedded: a.cfg.Resources.Embedded,
		},
		Seed:      a.cfg.Seed,
		Session:   a.cfg.Session,
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
	return NewHeadless(cfg)
}

// installSharedResolved is InstallShared without the identity check: StageShared has
// already asked the live instance, and a staging world built from the same
// configuration would re-derive the same verdict — and would fail outright after a
// reset, whose session counter a freshly constructed world has not reached.
func (a *App) installSharedResolved(cap snapshot.SharedCapture) error {
	// The staging world proves that the position resolves; it does not present or
	// simulate this participant's local effects. Reconciliation belongs only to
	// the live commit, where its emitted lifecycle events can reach those systems.
	return a.installShared(cap, false)
}
