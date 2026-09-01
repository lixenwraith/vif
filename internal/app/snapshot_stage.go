// Package app: the staged install.
//
// InstallShared writes into the world it is called on. That is the right shape for
// a harness, which owns both worlds and ticks neither, and the wrong one for a
// join: the instance being installed into is running, and a capture that turns out
// to be unloadable halfway through would leave it holding a world that is neither
// its own nor the session's.
//
// A stage resolves the whole capture into a second world first — a real one, with
// this build's system set, its FSM and its RNG stream inventory — and only then
// writes the same bytes into the live world, between two ticks. What survives the
// staging pass is what the live pass cannot fail on: identical code, identical
// input, and no dependence on the state being written over.
package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// StagedInstall is a capture that has been resolved against a second world and is
// waiting for its tick boundary. Nothing in the live world has been touched.
//
// The handle owns the staging world and must be released — Commit does that on the
// way out, Discard does it on the way out of a join that failed for some other
// reason.
type StagedInstall struct {
	live    *App
	staging *App
	capture SharedCapture

	stageDur   time.Duration
	commitDur  time.Duration
	committed  bool
	discarded  bool
	encodedLen int
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
func (a *App) StageShared(cap SharedCapture) (*StagedInstall, error) {
	started := time.Now() // [wall] telemetry only; the install carries no instant
	if err := a.VerifyCapture(cap); err != nil {
		return nil, err
	}

	staging, err := a.newStagingApp(cap)
	if err != nil {
		return nil, fmt.Errorf("stage: %w", err)
	}
	// The FSM boot script's queued spawn is what declares the cursor template a
	// late arrival is created from, and it is still queued: the machine enters its
	// boot state inside New and nothing has ticked. Settling it here makes the
	// staging world the same shape as the instance it stands in for — a joiner
	// settles the same queue before it installs, for the same reason.
	staging.Settle()
	if err := staging.installSharedResolved(cap); err != nil {
		staging.Close()
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
func (s *StagedInstall) Capture() SharedCapture { return s.capture }

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
	started := time.Now() // [wall] telemetry only
	err := s.live.installSharedResolved(s.capture)
	s.commitDur = time.Since(started)
	s.committed = true
	s.release()
	if err != nil {
		vlog.Error("app", "msg", "staged capture failed its live install",
			"tick", s.capture.Header.Tick, "error", err.Error())
		return fmt.Errorf("commit a staged capture: %w", err)
	}
	vlog.Info("app", "msg", "capture installed",
		"tick", s.capture.Header.Tick,
		"stage_ms", s.stageDur.Milliseconds(), "commit_ms", s.commitDur.Milliseconds())
	return nil
}

// Discard releases the staging world without writing anything.
func (s *StagedInstall) Discard() {
	if s.committed || s.discarded {
		return
	}
	s.discarded = true
	s.release()
}

// Timings reports what the two halves cost, for the cadence Phase 4 has to choose.
func (s *StagedInstall) Timings() (stage, commit time.Duration) { return s.stageDur, s.commitDur }

func (s *StagedInstall) release() {
	if s.staging != nil {
		s.staging.Close()
		s.staging = nil
	}
}

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
func (a *App) newStagingApp(cap SharedCapture) (*App, error) {
	cfg := a.cfg
	cfg.Mode = ModeHeadless
	cfg.Journal = false
	cfg.JournalSink = nil
	cfg.HostAddress, cfg.JoinAddress = "", ""
	cfg.networkConfig = nil
	cfg.scriptedSession = false
	cfg.Participants = 0
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
func (a *App) installSharedResolved(cap SharedCapture) error {
	return a.installShared(cap)
}
