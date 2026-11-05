package corrector

import (
	"errors"
	"fmt"
	"time"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/snapshot"
)

// StagedInstall is a capture resolved against a second world and waiting for its
// tick boundary; nothing in the live world has been touched. The handle borrows the
// staging world and must release it — Commit on the way out, Discard otherwise —
// which hands it back to the run rather than closing it, because every later
// correction resolves into the same one.
type StagedInstall struct {
	live    *Corrector
	staging Staging
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
func (c *Corrector) StageShared(cap snapshot.SharedCapture) (*StagedInstall, error) {
	if err := c.VerifyCapture(cap); err != nil {
		return nil, err
	}
	return c.stageProved(cap)
}

// stageProved is StageShared for a capture the correction protocol has proved: a
// body's integrity is checked as it is resolved and a repair reproduces the
// authority's root, so hashing it again would prove nothing new.
func (c *Corrector) stageProved(cap snapshot.SharedCapture) (*StagedInstall, error) {
	started := time.Now() // [wall] telemetry only; the install carries no instant
	if err := c.VerifyCaptureIdentity(cap.Header); err != nil {
		return nil, err
	}

	staging, fresh, err := c.stagingWorld(cap)
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
	if err := staging.Corrector().installSharedResolved(cap); err != nil {
		c.DiscardStaging()
		return nil, fmt.Errorf("stage: %w", err)
	}

	st := &StagedInstall{live: c, staging: staging, capture: cap, stageDur: time.Since(started)}
	c.log.Info("converge", "msg", "capture staged",
		"tick", cap.Header.Tick, "streams", len(cap.Streams), "systems", len(cap.Systems),
		"stage_us", st.stageDur.Microseconds())
	return st, nil
}

// Tick names the tick the staged capture describes.
func (s *StagedInstall) Tick() uint64 { return s.capture.Header.Tick }

// Capture returns the staged capture, for a caller that has to answer the host
// about what it installed.
func (s *StagedInstall) Capture() snapshot.SharedCapture { return s.capture }

// StagingWorld exposes the resolved second world, for a test that wants to compare
// it against the live one before the swap. It is invalid after Commit or Discard.
func (s *StagedInstall) StagingWorld() Staging { return s.staging }

// Commit projects the staged capture to the live tick — the authority's world, this
// instance's owner-authored values and what it applied since, simulated to the
// present — and writes what the projection moves (multi-player.md §3.3). The live
// lock is held throughout: a tick landing between projection and write would be lost.
func (s *StagedInstall) Commit() error {
	switch {
	case s.committed:
		return errors.New("staged install already committed")
	case s.discarded:
		return errors.New("staged install already discarded")
	}
	var (
		err                  error
		behind               uint64
		written              StagedWrite
		place                event.Stamp
		mark                 uint64
		participant          uint32
		barrier              bool
		projectDur, writeDur time.Duration
	)
	started := time.Now() // [wall] telemetry only
	live, staging, header := s.live, s.staging, s.capture.Header
	sg := staging.Corrector()
	var journal *event.Journal

	live.world.RunSafe(func() {
		// Placed before the settlement below: its confirmations dispatch after the write.
		// The journal too, which an opening may have attached while this waited
		journal = live.world.Resources.Event.Queue.Journal()
		place, mark, participant = live.run.Position(), journal.Mark(), live.world.LocalParticipant()

		// The ledger is settled against the authority's world as installed, before
		// the projection re-derives this instance's own predictions over it.
		live.confirmPredictionsLocked(header.Tick, sg.world)

		// A capture ahead of the clock is adopted at its own tick: the buffer holds
		// one within NetworkHoldTicks, so reaching here ahead is a join or a jump. One
		// behind is projected to the present.
		at, tick := live.run.Position().Tick, header.Tick
		if at > tick {
			behind, tick = at-tick, at
		}
		live.prepareProjectionLocked(sg)
		live.feedProjectionLocked(sg, header)
		projecting := time.Now() // [wall] telemetry only
		staging.Tick(int(behind))
		// What the live world settled after its last completed tick — a copy the
		// queue published at once, a record the authority received late — is due now.
		sg.receiveDue(tick + 1)
		writing := time.Now() // [wall] telemetry only
		projectDur = writing.Sub(projecting)

		// The authority's identity and fences, at the live tick: what the barrier
		// prunes by is what the host applied, and the projection has moved the
		// world to where this instance stands. The two worlds are compared and
		// written in place; a journal takes the delta that write recorded.
		hdr := header
		hdr.Tick = tick
		s.difference, written, err = live.WriteStagedLocked(sg, hdr, journal != nil)
		if err == nil {
			barrier = live.run.BarrierLocked()
		}
		writeDur = time.Since(writing)
		if err == nil {
			m := live.telemetry
			m.InstallTick.Store(int64(header.Tick))
			m.Projected.Store(int64(behind))
			m.ProjectUS.Store(projectDur.Microseconds())
			m.WriteUS.Store(writeDur.Microseconds())
		}
	})
	var journalErr error
	if err == nil && journal != nil {
		// The written rows are hashed where they stand, before the staging world's
		// player half goes: a cursor's cells change with the entities they refer to.
		sg.world.RunSafe(func() {
			written.integrity, journalErr = sg.encoder.IntegrityLive(sg.world, written.written)
		})
	}
	sg.world.RunSafe(func() { sg.world.DestroyDomainEntities(core.DomainPlayer) })
	s.commitDur = time.Since(started)
	s.committed = true
	s.release()
	if err != nil {
		s.live.log.Error("converge", "msg", "staged capture failed its live install",
			"tick", header.Tick, "error", err.Error())
		return fmt.Errorf("commit a staged capture: %w", err)
	}
	if journal != nil {
		if journalErr == nil {
			journalErr = live.journalStaged(journal, place, mark, participant, barrier, written)
		}
		if journalErr != nil {
			live.world.RunSafe(func() { live.run.JournalLostLocked(journal, journalErr) })
		}
	}
	live.telemetry.StageUS.Store(s.stageDur.Microseconds())
	live.telemetry.CommitUS.Store(s.commitDur.Microseconds())
	s.live.log.Info("converge", "msg", "capture installed",
		"tick", header.Tick, "projected_ticks", behind,
		"stage_us", s.stageDur.Microseconds(), "commit_us", s.commitDur.Microseconds(),
		"project_us", projectDur.Microseconds(), "write_us", writeDur.Microseconds(),
		"correction_entries", s.difference.Entries,
		"correction_entities", s.difference.Entities,
		"correction_cells", s.difference.CellShift)
	return nil
}

// JournalWritten records a world this instance wrote, as what it changed, so a
// replay holding the same world writes it at the same place as the same
// participant, with the barrier it left. A join writes before its transport
// attaches, so the identity then is the one the offer assigned.
func (c *Corrector) JournalWritten(j *event.Journal, at event.Stamp, mark uint64, participant uint32, barrier bool, before, cap snapshot.SharedCapture) error {
	d, err := c.encoder.DiffWritten(before, cap)
	if err != nil {
		return err
	}
	return c.journalDelta(j, at, mark, participant, barrier, d)
}

// journalStaged is JournalWritten for a world written in place, from the delta the
// write recorded and the integrity its rows were hashed to where they stood.
func (c *Corrector) journalStaged(j *event.Journal, at event.Stamp, mark uint64, participant uint32, barrier bool, w StagedWrite) error {
	return c.journalDelta(j, at, mark, participant, barrier, c.encoder.DiffWrittenLight(w.before, w.written, *w.Delta, w.integrity))
}

// journalDelta records one written world as its delta, with the hold a join's
// world lands in, so a replay holds there as the run did.
func (c *Corrector) journalDelta(j *event.Journal, at event.Stamp, mark uint64, participant uint32, barrier bool, d snapshot.WrittenDelta) error {
	body, err := snapshot.EncodeJSON(d)
	if err == nil {
		err = journal.LineHolds(body)
	}
	if err != nil {
		return err
	}
	if participant == 0 {
		participant = c.run.AssignedParticipant()
	}
	held, start := c.run.LandingHold()
	j.Capture(event.JournalCapture{
		JSeq: mark, Run: at.Run, Tick: at.Tick, Boundary: at.Boundary,
		Participant: participant, Authority: d.Header.Authority, Barrier: barrier, Body: body,
		Held: held, Start: start,
	})
	return nil
}

// StagedWrite is what a write in place leaves for the journal: both worlds beside
// their rows, the rows' Delta the write recorded, nil with no journal open, and the
// integrity of the written rows, hashed where they stand through the staging
// world's own encoder, so the live one keeps the rows it last encoded.
type StagedWrite struct {
	before, written snapshot.SharedCapture
	Delta           *engine.SharedWorldDelta
	integrity       uint64
}

// WriteStagedLocked is WriteSharedLocked for the world that stands in the staging
// world: the stores are compared and written in place, with no capture of either,
// and everything beside them is read light. header is the capture's at the live
// tick; record asks for the delta a journal writes. Caller MUST hold updateMutex;
// the staging world's lock is taken here.
func (c *Corrector) WriteStagedLocked(staging *Corrector, header snapshot.CaptureHeader, record bool) (diff engine.WorldDifference, w StagedWrite, err error) {
	if w.before, err = c.CaptureLightLocked(); err != nil {
		return diff, w, err
	}
	staging.world.RunSafe(func() { w.written, err = staging.CaptureLightLocked() })
	if err != nil {
		return diff, w, err
	}
	w.written.Header = header

	// Dry run first: a carrier that rejects its record must do so before the stores
	// are touched. Only the live carrier can refuse because of state the live world
	// holds, which a staging pass never has.
	savers := SharedStateSaversLocked(c.world)
	for _, rec := range w.written.Systems {
		saver, ok := savers[rec.System]
		if !ok {
			return diff, w, fmt.Errorf("capture names system %q, which this build does not run", rec.System)
		}
		if checker, ok := saver.(engine.SharedStateChecker); ok {
			if e := checker.CheckShared(rec.Data); e != nil {
				return diff, w, fmt.Errorf("refuse %s: %w", rec.System, e)
			}
		}
	}

	// The roster and every cursor's control assignment are re-derived from this
	// instance's own position after the stores are written, not adopted (D-13).
	local := c.world.CaptureCursorControl()
	var r *engine.WorldReconcile
	staging.world.RunSafe(func() { r = c.world.ReconcileSharedFrom(staging.world, record) })
	diff, w.Delta = r.Difference, r.Delta
	// A world equal to the staging one on the compared surface and in every other
	// part takes clock and barrier alone, as a hash-only answer does.
	if diff.Entries == 0 && snapshot.HoldsBesideWorld(w.before, w.written) {
		c.adoptClockLocked(header)
		c.adoptSnapshotBarrierLocked(header)
		return diff, w, nil
	}
	staging.world.RunSafe(r.Finish)
	c.world.RebindCursorRoster(local)
	c.adoptClockLocked(header)

	if unknown := c.world.Resources.Rand.LoadStreams(core.DomainShared, w.written.Streams); len(unknown) > 0 {
		return diff, w, fmt.Errorf("capture names RNG streams this build does not issue: %v", unknown)
	}
	for _, rec := range w.written.Systems {
		if e := savers[rec.System].LoadShared(rec.Data); e != nil {
			return diff, w, fmt.Errorf("install %s: %w", rec.System, e)
		}
	}
	if e := c.scheduler.ImportFSM(w.written.FSM, true); e != nil {
		return diff, w, e
	}
	// The barrier is rebased with the world. Tick classifies barrier-bound
	// artifacts; each source's fence classifies its ordinary stream.
	c.adoptSnapshotBarrierLocked(header)
	// Last, so a carrier that publishes on load does not overwrite the captured
	// surface with a value derived from this instance's own history.
	InstallStatusLocked(c.world, w.written.Status)
	return diff, w, nil
}

// prepareProjectionLocked makes the staging world this instance's predictor: it
// drives no cursor, so no player-domain system simulates for one; it holds this
// instance's owner-authored values, which the shared species it predicts react to;
// and its barrier stamps by this instance's own lead under the session's authority.
// Caller MUST hold the live world's updateMutex.
func (c *Corrector) prepareProjectionLocked(staging *Corrector) {
	var (
		lead      uint64
		authority uint32
	)
	ctl := c.world.CaptureCursorControl()
	if r := c.world.Resources.Network; r != nil {
		lead, authority = r.BarrierDelayTicks, r.Authority.Load()
	}
	staging.world.RunSafe(func() {
		w := staging.world
		w.DisownCursors()
		w.AdoptOwnedCursorState(ctl)
		r := &engine.NetworkResource{BarrierDelayTicks: lead}
		r.Authority.Store(authority)
		w.Resources.Network = r
	})
}

// receiveDue applies what the barrier holds due at tick without running one, and
// settles what that dispatched.
func (c *Corrector) receiveDue(tick uint64) {
	c.world.RunSafe(func() { c.world.Resources.Event.Queue.ReceiveWire(tick) })
	c.scheduler.Settle()
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

// stagingKey is what a staging world cannot be re-used across: the D-14 map
// latch decides what a capture's placements mean, and the RNG session decides
// every root a system reads at Init, which a staging world never re-runs.
type stagingKey struct {
	w, h    int
	session uint64
}

// stagingWorld returns the second world captures resolve into, building it the first
// time and re-using it after; the second return says whether its FSM boot queue
// still needs settling. A world that kept anything from the previous install would
// resolve the next capture against a world the sender never had.
func (c *Corrector) stagingWorld(cap snapshot.SharedCapture) (Staging, bool, error) {
	c.stageMu.Lock()
	defer c.stageMu.Unlock()

	key := stagingKey{cap.Header.MapWidth, cap.Header.MapHeight, cap.Header.Session}
	if c.staging != nil {
		if c.stagedFor == key {
			return c.staging, false, nil
		}
		c.staging.Close()
		c.staging = nil
	}
	staging, err := c.run.NewStaging(cap)
	if err != nil {
		return nil, false, err
	}
	c.staging, c.stagedFor = staging, key
	return staging, true, nil
}

// DiscardStaging throws away a staging world a capture failed to resolve into.
//
// A failed load may have written part of itself, so the world is no longer a
// faithful stand-in for this instance and the next correction must not be resolved
// against it. This is the one path that closes one before the run ends.
func (c *Corrector) DiscardStaging() {
	c.stageMu.Lock()
	staging := c.staging
	c.staging, c.stagedFor = nil, stagingKey{}
	c.stageMu.Unlock()
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

// installSharedResolved is InstallShared without the identity check: StageShared has
// already asked the live instance, and a staging world built from the same
// configuration would re-derive the same verdict.
func (c *Corrector) installSharedResolved(cap snapshot.SharedCapture) error {
	// The staging world proves that the position resolves; it does not present or
	// simulate this participant's local effects. Reconciliation belongs only to
	// the live commit, where its emitted lifecycle events can reach those systems.
	return c.installShared(cap, false)
}
