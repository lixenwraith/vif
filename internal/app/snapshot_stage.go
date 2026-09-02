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
//
// Phase 4 changed what this has to cost. Phase 3 built a whole second App per
// install and threw it away — 9 to 31 ms, which is right for a join that happens
// once and wrong for a correction that happens five times a second. The staging
// world is kept now: it is built the first time something is staged into it and
// re-used for the life of the run, so a correction pays the load and not the
// construction. And Commit stopped being a second full write: it reconciles the
// live world onto the capture instead of clearing and re-inserting it, so what it
// writes is the size of the correction rather than the size of the world.
package app

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/status"
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
	capture SharedCapture

	stageDur   time.Duration
	commitDur  time.Duration
	committed  bool
	discarded  bool
	encodedLen int

	// difference is how far the live world had drifted from the capture when the
	// commit wrote it. On a guest that is the correction magnitude — the distance
	// between what this instance predicted and what the host actually had — and it
	// is telemetry rather than an error (D-11 as Phase 4 weakened it).
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
func (a *App) StageShared(cap SharedCapture) (*StagedInstall, error) {
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
		m := s.live.snapshotTelemetry
		m.stageUS.Store(s.stageDur.Microseconds())
		m.commitUS.Store(s.commitDur.Microseconds())
		m.installTick.Store(int64(s.capture.Header.Tick))
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

// stagingWorld returns the second world captures resolve into, building it the
// first time and re-using it after.
//
// Re-use is the whole point and it is also the thing that could go wrong. A staging
// world that kept anything from the previous install — a carrier that merged rather
// than replaced, an entity a store did not drop — would resolve the next capture
// against a world the sender never had, and the staging pass would stop being the
// question it is asked. TestStagingWorldReuseMatchesAFreshOne is what holds that:
// two captures installed into one staging world in sequence must leave exactly what
// a world built for the second alone would.
//
// What it cannot re-use is a world built on different bounds. The D-14 map latch
// decides what the level setup reflows and what a capture's placements mean, so a
// capture that names other bounds gets a world built for them.
//
// The second return reports whether the world was just built, which is what decides
// whether its FSM boot queue still needs settling.
func (a *App) stagingWorld(cap SharedCapture) (*App, bool, error) {
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

// snapshotTelemetry is the capture and install cost, reserved before the metric set
// is frozen so a join can publish into it.
//
// It is deliberately per-instance and deliberately excluded from the compared
// surface: a host publishes what a read cost it and a joiner publishes what an
// install cost it, and neither is a fact about the world they now share. What the
// numbers are for is Phase 4's cadence, which has to be chosen from a measurement
// rather than a guess.
type snapshotTelemetry struct {
	captureUS   *atomic.Int64
	encodeUS    *atomic.Int64
	bytes       *atomic.Int64
	stageUS     *atomic.Int64
	commitUS    *atomic.Int64
	installTick *atomic.Int64
	catchUp     *atomic.Int64

	// The correction counters. sent/sent_bytes/keyframes are the host's side of
	// the cadence — what it published and how much of it had to be whole — and
	// applied/refused/superseded are the guest's. None of them is an error count:
	// a refused delta is one whose keyframe this instance does not hold, and a
	// superseded correction is one a fresher correction overtook, both of which a
	// keyframe resolves on its own.
	sent       *atomic.Int64
	sentBytes  *atomic.Int64
	keyframes  *atomic.Int64
	applied    *atomic.Int64
	refused    *atomic.Int64
	superseded *atomic.Int64

	// The correction magnitude, which is what Phase 4 puts where DESYNC was. It is
	// how far this instance's prediction had drifted from the authority at the
	// moment the authority arrived: component cells, the distinct entities behind
	// them, and the largest distance a shared placement moved — the one a player
	// would actually see.
	correctionEntries  *atomic.Int64
	correctionEntities *atomic.Int64
	correctionCells    *atomic.Int64
	correctionTick     *atomic.Int64

	// The operating point, which is Phase 5's answer to "the cadence is a
	// constant". cadenceTicks and keyframeInterval are what the controller
	// currently holds; keyframePeriod is their product, which is the value the
	// convergence floor bounds and therefore the one worth reading first.
	//
	// The three rates are all bytes per second and they are three different
	// claims. uplinkBps is what the schedule in force costs. budgetBps is what the
	// tightest link was measured to allow after the utilisation share. floorBps is
	// what the floor costs on a world this size — the cheapest schedule that still
	// delivers a whole world per floor window — and a budget under it is the
	// unrecoverable condition floorBreached names.
	cadenceTicks     *atomic.Int64
	keyframeInterval *atomic.Int64
	keyframePeriod   *atomic.Int64
	uplinkBps        *atomic.Int64
	budgetBps        *atomic.Int64
	floorBps         *atomic.Int64
	constrained      *atomic.Bool
	floorBreached    *atomic.Bool

	// keyframeAge is how long this instance has gone without a whole
	// authoritative world, in ticks. It is the *receiving* end of the same
	// guarantee: the host promises to publish one per floor window, and this is
	// what says whether one actually arrived.
	keyframeAge *atomic.Int64

	// The Phase 6 selective-correction counters.
	//
	// They are grouped by the question each group answers. The first is "what did
	// the index cost" — manifests published and received, and their bytes, which
	// is the traffic that replaces a whole delta on a healthy link. The second is
	// "how often did it prove convergence outright": hashOnly is the case the
	// whole design is for, and sectionsCompared/pagesCompared say how much work
	// finding that answer took. The third is the repair itself, counted at every
	// point a shard can be at — asked for, sent, arrived, refused, applied — so a
	// gap between two of them names which side dropped it.
	manifestSent      *atomic.Int64
	manifestRecv      *atomic.Int64
	manifestBytesSent *atomic.Int64
	manifestBytesRecv *atomic.Int64
	hashOnly          *atomic.Int64
	sectionsCompared  *atomic.Int64
	pagesCompared     *atomic.Int64

	shardsRequested *atomic.Int64
	shardsSent      *atomic.Int64
	shardsRecv      *atomic.Int64
	shardsRefused   *atomic.Int64
	shardsApplied   *atomic.Int64
	shardBytesSent  *atomic.Int64
	shardBytesRecv  *atomic.Int64
	requestBytes    *atomic.Int64
	selectiveBytes  *atomic.Int64

	// What a repair moved, and what refused one. proofFailures counts a shard
	// whose rows did not reproduce their declared page hash or whose root did not
	// verify; baselineRefusals a set naming a tick, run or session this instance
	// is not holding. Neither is an error condition on its own — both end at the
	// keyframe fallback, which keyframeFallbacks counts.
	pagesRepaired    *atomic.Int64
	entitiesRepaired *atomic.Int64
	cellsRepaired    *atomic.Int64
	proofFailures    *atomic.Int64
	baselineRefusals *atomic.Int64
	keyframeFallback *atomic.Int64

	// hashUS is what indexing and comparing one capture cost outside the world
	// lock, beside captureUS which is the bounded read inside it. The pair is the
	// whole of requirement 8 as a measurement: if the second grows with the first,
	// work has moved under the lock that should not have.
	hashUS *atomic.Int64

	// The bounded replay suffix (deliverable 2). retained is what this instance is
	// currently holding, replayed what the last correction re-applied, overflowed
	// how many records retention dropped, and skipped how many corrections found
	// the suffix unavailable and fell back to the authority alone.
	replaySuffix   *atomic.Int64
	replayReplayed *atomic.Int64
	replayOverflow *atomic.Int64
	replaySkipped  *atomic.Int64
	replayUnusable *atomic.Bool
}

// newSnapshotTelemetry reserves the cells. Called during construction, because a
// key first written after Freeze is counted late rather than stored.
func newSnapshotTelemetry(reg *status.Registry) snapshotTelemetry {
	return snapshotTelemetry{
		captureUS:   reg.Ints.Get("snapshot.capture_us"),
		encodeUS:    reg.Ints.Get("snapshot.encode_us"),
		bytes:       reg.Ints.Get("snapshot.bytes"),
		stageUS:     reg.Ints.Get("snapshot.stage_us"),
		commitUS:    reg.Ints.Get("snapshot.commit_us"),
		installTick: reg.Ints.Get("snapshot.install_tick"),
		catchUp:     reg.Ints.Get("snapshot.catch_up_ticks"),

		sent:       reg.Ints.Get("snapshot.corrections_sent"),
		sentBytes:  reg.Ints.Get("snapshot.correction_bytes_sent"),
		keyframes:  reg.Ints.Get("snapshot.keyframes"),
		applied:    reg.Ints.Get("snapshot.corrections_applied"),
		refused:    reg.Ints.Get("snapshot.corrections_refused"),
		superseded: reg.Ints.Get("snapshot.corrections_superseded"),

		correctionEntries:  reg.Ints.Get("snapshot.correction_entries"),
		correctionEntities: reg.Ints.Get("snapshot.correction_entities"),
		correctionCells:    reg.Ints.Get("snapshot.correction_cells"),
		correctionTick:     reg.Ints.Get("snapshot.correction_tick"),

		cadenceTicks:     reg.Ints.Get("snapshot.cadence_ticks"),
		keyframeInterval: reg.Ints.Get("snapshot.cadence_keyframe_interval"),
		keyframePeriod:   reg.Ints.Get("snapshot.cadence_keyframe_period_ticks"),
		uplinkBps:        reg.Ints.Get("snapshot.cadence_uplink_bps"),
		budgetBps:        reg.Ints.Get("snapshot.cadence_budget_bps"),
		floorBps:         reg.Ints.Get("snapshot.cadence_floor_bps"),
		constrained:      reg.Bools.Get("snapshot.cadence_constrained"),
		floorBreached:    reg.Bools.Get("snapshot.cadence_floor_breached"),
		keyframeAge:      reg.Ints.Get("snapshot.cadence_keyframe_age_ticks"),

		manifestSent:      reg.Ints.Get("snapshot.manifests_sent"),
		manifestRecv:      reg.Ints.Get("snapshot.manifests_received"),
		manifestBytesSent: reg.Ints.Get("snapshot.manifest_bytes_sent"),
		manifestBytesRecv: reg.Ints.Get("snapshot.manifest_bytes_received"),
		hashOnly:          reg.Ints.Get("snapshot.corrections_hash_only"),
		sectionsCompared:  reg.Ints.Get("snapshot.sections_compared"),
		pagesCompared:     reg.Ints.Get("snapshot.pages_compared"),

		shardsRequested: reg.Ints.Get("snapshot.shards_requested"),
		shardsSent:      reg.Ints.Get("snapshot.shards_sent"),
		shardsRecv:      reg.Ints.Get("snapshot.shards_received"),
		shardsRefused:   reg.Ints.Get("snapshot.shards_refused"),
		shardsApplied:   reg.Ints.Get("snapshot.shards_applied"),
		shardBytesSent:  reg.Ints.Get("snapshot.shard_bytes_sent"),
		shardBytesRecv:  reg.Ints.Get("snapshot.shard_bytes_received"),
		requestBytes:    reg.Ints.Get("snapshot.request_bytes"),
		selectiveBytes:  reg.Ints.Get("snapshot.selective_bytes"),

		pagesRepaired:    reg.Ints.Get("snapshot.pages_repaired"),
		entitiesRepaired: reg.Ints.Get("snapshot.entities_repaired"),
		cellsRepaired:    reg.Ints.Get("snapshot.cells_repaired"),
		proofFailures:    reg.Ints.Get("snapshot.proof_failures"),
		baselineRefusals: reg.Ints.Get("snapshot.baseline_refusals"),
		keyframeFallback: reg.Ints.Get("snapshot.keyframe_fallbacks"),

		hashUS: reg.Ints.Get("snapshot.hash_us"),

		replaySuffix:   reg.Ints.Get("snapshot.replay_suffix_records"),
		replayReplayed: reg.Ints.Get("snapshot.replay_records"),
		replayOverflow: reg.Ints.Get("snapshot.replay_overflow"),
		replaySkipped:  reg.Ints.Get("snapshot.replay_skipped"),
		replayUnusable: reg.Bools.Get("snapshot.replay_suffix_unavailable"),
	}
}

// Timings reports what the two halves cost, for the cadence Phase 4 has to choose.
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

// reconcileSharedResolved is the live half of a staged install: the same capture,
// already proved loadable by the staging pass, written onto the world this instance
// is holding rather than over the top of it.
func (a *App) reconcileSharedResolved(cap SharedCapture) (engine.WorldDifference, error) {
	return a.reconcileShared(cap)
}
