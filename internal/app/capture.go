package app

import (
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/manifest"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/service"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
)

// captureStatusLocked reads every shared-surface registry cell, through
// snapshot.SharedKey — the same predicate the compared surface uses, so a capture
// carries exactly what a session is asserted to agree on.
//
// Caller MUST hold updateMutex.
func (a *App) captureStatusLocked() snapshot.StatusState {
	reg := a.world.Resources.Status
	return snapshot.StatusState{
		Ints: sharedCells(reg.Ints.Keys(), func(k string) snapshot.IntCell {
			return snapshot.IntCell{Key: k, Value: reg.Ints.Get(k).Load()}
		}),
		Bools: sharedCells(reg.Bools.Keys(), func(k string) snapshot.BoolCell {
			return snapshot.BoolCell{Key: k, Value: reg.Bools.Get(k).Load()}
		}),
		Floats: sharedCells(reg.Floats.Keys(), func(k string) snapshot.FloatCell {
			return snapshot.FloatCell{Key: k, Value: reg.Floats.Get(k).Get()}
		}),
		Strings: sharedCells(reg.Strings.Keys(), func(k string) snapshot.StringCell {
			return snapshot.StringCell{Key: k, Value: reg.Strings.Get(k).Load()}
		}),
	}
}

// sharedCells is one registry kind's shared-surface cells, in key order so two
// instances holding equal state produce equal bytes. The result is nil when nothing
// matches rather than an empty slice, because the encoding distinguishes the two and
// the capture's integrity hash covers it.
func sharedCells[C any](keys []string, cell func(string) C) []C {
	slices.Sort(keys)
	var out []C
	for _, k := range keys {
		if snapshot.SharedKey(k) {
			out = append(out, cell(k))
		}
	}
	return out
}

// installStatusLocked writes the captured surface back.
//
// A key this build does not carry is skipped rather than refused: the registry is
// frozen after construction, so writing an unknown key would be counted late
// rather than stored, and a metric added or removed between builds is a
// telemetry difference, not a simulation one. The identity check has already
// established the two are the same build.
//
// Caller MUST hold updateMutex.
func (a *App) installStatusLocked(state snapshot.StatusState) {
	reg := a.world.Resources.Status
	for _, c := range state.Ints {
		if reg.Ints.Has(c.Key) {
			reg.Ints.Get(c.Key).Store(c.Value)
		}
	}
	for _, c := range state.Bools {
		if reg.Bools.Has(c.Key) {
			reg.Bools.Get(c.Key).Store(c.Value)
		}
	}
	for _, c := range state.Floats {
		if reg.Floats.Has(c.Key) {
			reg.Floats.Get(c.Key).Set(c.Value)
		}
	}
	for _, c := range state.Strings {
		if reg.Strings.Has(c.Key) {
			reg.Strings.Get(c.Key).Store(c.Value)
		}
	}
}

// CaptureShared reads the shared world at the current tick. The whole capture is
// taken inside one critical section: one assembled across two ticks would describe
// a world that never existed.
func (a *App) CaptureShared() (snapshot.SharedCapture, error) {
	var (
		cap                     snapshot.SharedCapture
		err                     error
		crossSource             uint32
		appliedCrossingSequence uint64
	)
	a.world.RunSafe(func() {
		cap.World = a.world.CaptureSharedWorld()
		cap.Streams = a.world.Resources.Rand.SaveStreams()
		cap.FSM = a.scheduler.ExportFSM()
		cap.Status = a.captureStatusLocked()
		cap.Systems, err = a.captureSystemStatesLocked()
		for _, sys := range a.world.Systems() {
			if fence, ok := sys.(interface {
				LocalAppliedCrossingSequence() (uint32, uint64)
			}); ok {
				crossSource, appliedCrossingSequence = fence.LocalAppliedCrossingSequence()
				break
			}
		}

		// The header's tick and crossing fence are part of the world reading,
		// not labels added afterwards. Reading them under the same lock prevents
		// a tick or a just-dispatched host input from falling between the body and
		// the boundary that tells receivers what the body contains.
		st := a.Position()
		reg := a.world.Resources.Status
		cfg := a.world.Resources.Config
		term, holder := a.authorityStamp()
		cap.Header = snapshot.CaptureHeader{
			Term:          term,
			Authority:     holder,
			Schema:        snapshot.Schema,
			JournalSchema: uint64(event.JournalSchema),
			Run:           st.Run,
			Tick:          st.Tick,
			TickInterval:  parameter.GameUpdateInterval,
			Seed:          a.world.Resources.Rand.Root(),
			Session:       a.world.Resources.Rand.Session(),
			ConfigID:      resolveConfigID(a.cfg),
			ContentID:     reg.Strings.Get("content.source").Load(),
			ContentFiles:  uint64(reg.Ints.Get("content.files").Load()),
			ContentBlocks: uint64(reg.Ints.Get("content.blocks").Load()),
			ContentLines:  uint64(reg.Ints.Get("content.lines").Load()),
			MapWidth:      cfg.MapWidth,
			MapHeight:     cfg.MapHeight,
		}
		if holder != 0 && crossSource == holder {
			cap.Header.AuthorityCrossingSeq = appliedCrossingSequence
		}
	})
	if err != nil {
		return snapshot.SharedCapture{}, err
	}
	cap.Header.ContentPin = service.MustGet[*service.ContentService](a.hub, "content").Pin()

	cap.Header.Integrity, err = snapshot.Integrity(cap)
	if err != nil {
		return snapshot.SharedCapture{}, err
	}
	return cap, nil
}

// captureSystemStatesLocked collects every declared carrier's state, in system
// name order so two instances holding equal state produce equal bytes.
//
// Caller MUST hold updateMutex.
func (a *App) captureSystemStatesLocked() ([]snapshot.SystemStateRecord, error) {
	type carrier struct {
		name  string
		saver engine.SharedStateSaver
	}
	carriers := make([]carrier, 0, 8)
	for _, sys := range a.world.Systems() {
		saver, ok := sys.(engine.SharedStateSaver)
		if !ok {
			continue
		}
		if manifest.SnapshotFor(sys.Name()) != engine.SnapshotState {
			// The boundary suite fails this pair at build time; refusing here as
			// well keeps a capture from silently carrying undeclared state if the
			// suite is ever skipped.
			return nil, fmt.Errorf("system %q saves shared state without declaring it", sys.Name())
		}
		carriers = append(carriers, carrier{name: sys.Name(), saver: saver})
	}
	sort.Slice(carriers, func(i, j int) bool { return carriers[i].name < carriers[j].name })

	out := make([]snapshot.SystemStateRecord, 0, len(carriers))
	for _, c := range carriers {
		data, err := c.saver.SaveShared()
		if err != nil {
			return nil, fmt.Errorf("capture %s: %w", c.name, err)
		}
		out = append(out, snapshot.SystemStateRecord{System: c.name, Data: data})
	}
	return out, nil
}

// InstallShared replaces this instance's shared world with a capture: identity,
// then integrity, then every system's state offered, and only then is anything
// written. A world half-installed is worse than one not installed at all — it is a
// divergence that looks like a working session.
//
// This is the direct form, writing into the world it is called on. A running
// instance takes StageShared instead.
func (a *App) InstallShared(cap snapshot.SharedCapture) error {
	if err := a.VerifyCapture(cap); err != nil {
		return err
	}
	return a.installShared(cap, true)
}

// installShared writes a capture whose identity has already been established, by
// replacing the shared world wholesale.
func (a *App) installShared(cap snapshot.SharedCapture, reconcileLocal bool) error {
	_, err := a.writeShared(cap, false, reconcileLocal)
	return err
}

// reconcileShared writes a capture by moving the live world onto it rather than
// replacing it, and reports how far apart the two were.
//
// The difference is read first and inside the same critical section as the write,
// because it is a statement about one instant: the world this instance predicted
// against the world the authority is handing it. Read a tick later and it would be
// the magnitude of a correction that had already happened.
func (a *App) reconcileShared(cap snapshot.SharedCapture) (engine.WorldDifference, error) {
	return a.writeShared(cap, true, true)
}

// writeShared is the one install, with the store pass chosen by the caller.
//
// Everything outside that pass is identical and has to be: the roster rebind, the
// tick and record rebase, the stream positions, every declared carrier, the FSM
// and the compared surface are what make the world the sender's, and a correction
// that skipped any of them would leave an instance that looks corrected and is not.
func (a *App) writeShared(cap snapshot.SharedCapture, reconcile, reconcileLocal bool) (engine.WorldDifference, error) {
	var (
		err  error
		diff engine.WorldDifference
	)
	a.world.RunSafe(func() {
		// Dry run first: a carrier that rejects its record must do so before the
		// stores are touched.
		//
		// Two questions, and the second is the one a staging pass cannot answer. A
		// capture naming a system this build does not run is a build mismatch, and
		// any world would say so. A capture a carrier refuses because of state the
		// *live* world holds — a genetic registry whose species set this instance
		// entered a level ahead of the authority to reach — is invisible to a
		// staging world that has never been in that state, so it would arrive as a
		// failure after the store pass had already rewritten everything. Asking the
		// live carrier here is what keeps the refusal atomic.
		savers := a.sharedStateSaversLocked()
		for _, rec := range cap.Systems {
			saver, ok := savers[rec.System]
			if !ok {
				err = fmt.Errorf("capture names system %q, which this build does not run", rec.System)
				return
			}
			if checker, ok := saver.(engine.SharedStateChecker); ok {
				if e := checker.CheckShared(rec.Data); e != nil {
					err = fmt.Errorf("refuse %s: %w", rec.System, e)
					return
				}
			}
		}

		// The roster and every cursor's control assignment are read before the
		// stores are replaced, because both are re-derived from this instance's own
		// position afterwards rather than adopted (D-13).
		local := a.captureCursorControlLocked()

		if reconcile {
			// The measurement and the write are one pass over the same stores.
			diff = engine.SharedWorldDifference(a.world.CaptureSharedWorld(), cap.World)
			a.world.ReconcileSharedWorld(cap.World)
		} else {
			a.world.InstallSharedWorld(cap.World)
		}
		a.rebindCursorRosterLocked(local)

		// The tick is shared identity. Adopting it also adopts the simulation
		// clock, because engine.SimTime derives the instant from the tick — which
		// is what lets an installed world resolve its stored deadlines on the same
		// ticks the run it came from will.
		a.world.Resources.Game.State.SetGameTicks(cap.Header.Tick)

		// The record position is the same identity seen from the event queue. Every
		// crossing's apply tick is computed from it, so an installed world stamping
		// its own tick zero would schedule the session's next artifact into a past
		// the barrier has already refused.
		a.world.Resources.Event.Queue.RebaseStamp(cap.Header.Run, cap.Header.Tick)
		a.world.Resources.Status.Correlation().SetRun(cap.Header.Run)
		a.world.Resources.Status.Correlation().SetTick(cap.Header.Tick)

		// Telemetry the scheduler publishes from the tick alone is republished
		// with it, so the installed world reports the instant it is at rather than
		// the one this instance had reached. Anything derived from more than the
		// tick is left to the next tick to recompute, which is where an installed
		// participant enters the session.
		reg := a.world.Resources.Status
		reg.Ints.Get("engine.ticks").Store(int64(cap.Header.Tick))
		reg.Ints.Get("time.game_elapsed_ms").Store(
			engine.SimTime(cap.Header.Tick, cap.Header.TickInterval).Sub(engine.SimEpoch).Milliseconds())
		a.world.Resources.Time.Update(
			engine.SimTime(cap.Header.Tick, cap.Header.TickInterval),
			a.world.Resources.Time.RealTime,
			cap.Header.TickInterval)

		if unknown := a.world.Resources.Rand.LoadStreams(cap.Streams); len(unknown) > 0 {
			err = fmt.Errorf("capture names RNG streams this build does not issue: %v", unknown)
			return
		}
		for _, rec := range cap.Systems {
			if e := savers[rec.System].LoadShared(rec.Data); e != nil {
				err = fmt.Errorf("install %s: %w", rec.System, e)
				return
			}
		}
		if e := a.scheduler.ImportFSM(cap.FSM, reconcileLocal); e != nil {
			err = e
			return
		}

		// The barrier is rebased with the world. Tick classifies peer and
		// barrier-bound artifacts; the completed authority sequence classifies the
		// authority's local-first stream.
		a.adoptSnapshotBarrierLocked(cap.Header)

		// Last, so a carrier that publishes on load does not overwrite the
		// captured surface with a value derived from this instance's own history.
		a.installStatusLocked(cap.Status)
	})
	return diff, err
}

// adoptSnapshotBarrierLocked tells the crossing barrier which world it now holds:
// the tick boundary for peer/barrier artifacts and the authority's exact local-
// first sequence fence. Caller MUST hold updateMutex.
func (a *App) adoptSnapshotBarrierLocked(header snapshot.CaptureHeader) {
	for _, sys := range a.world.Systems() {
		if b, ok := sys.(interface {
			AdoptSnapshot(uint64, uint32, uint64)
		}); ok {
			b.AdoptSnapshot(header.Tick, header.Authority, header.AuthorityCrossingSeq)
		}
	}
}

// sharedStateSaversLocked indexes this world's declared carriers by name.
// Caller MUST hold updateMutex.
func (a *App) sharedStateSaversLocked() map[string]engine.SharedStateSaver {
	out := make(map[string]engine.SharedStateSaver, 8)
	for _, sys := range a.world.Systems() {
		if saver, ok := sys.(engine.SharedStateSaver); ok {
			out[sys.Name()] = saver
		}
	}
	return out
}

// VerifyCapture reports whether this instance can install a capture: whether it
// is intact, and whether it describes the same build, configuration and corpus.
//
// The identity set is anchorIdentity's, deliberately. A capture and a journal
// anchor answer the same question — "are these two instances running the same
// simulation" — and one of them drifting from the other would let a join succeed
// where a replay of the same pair fails.
func (a *App) VerifyCapture(cap snapshot.SharedCapture) error {
	if cap.Header.Schema != snapshot.Schema {
		return fmt.Errorf("capture schema %d, this build reads %d",
			cap.Header.Schema, snapshot.Schema)
	}
	want, err := snapshot.Integrity(cap)
	if err != nil {
		return err
	}
	if want != cap.Header.Integrity {
		return errors.New("capture integrity hash does not match its body")
	}

	return firstAnchorMismatch("capture", a.anchorIdentity(snapshot.Anchor(cap.Header)))
}
