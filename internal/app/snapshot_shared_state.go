package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/fsm"
	"github.com/lixenwraith/vi-fighter/internal/manifest"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/service"
)

// SnapshotSchema is the capture layout's version. It is not the journal's schema:
// the two describe different things and change for different reasons, and a
// capture names both so a mismatch says which one moved.
const SnapshotSchema = 3

// SharedCapture is a complete description of the shared world at one tick (D-19).
//
// It exists so that joining a session, reconnecting to one, and turning a solo
// run into a host are all one mechanism: send the current state. Cost is a
// function of world size rather than of session length, which is what makes a
// mid-run join possible at all.
//
// What is here is what cannot be re-derived: the shared component stores, the
// allocator's next ID, every RNG stream's position, and the private state each
// system declared under D-19. What is deliberately absent is everything an
// install recomputes — the flow fields, the spatial index, the passability grid —
// and everything player-domain, which no other instance holds and none may adopt.
type SharedCapture struct {
	Header  CaptureHeader           `json:"header"`
	World   engine.SharedWorldState `json:"world"`
	Streams []engine.StreamState    `json:"streams"`
	Systems []SystemStateRecord     `json:"systems"`

	// Status is the compared shared surface's registry half: every key sharedKey
	// admits. Cumulative species counters live here — how many swarms a run has
	// spawned and despawned, how many physics steps they have taken — and D-11
	// requires two instances to agree on them, because both re-derive the same
	// species lifecycle. They affect no future outcome, which is exactly why no
	// system declares them under D-19; but a joiner that arrived holding its own
	// totals would read as divergent on the compared surface from its first tick
	// and never converge, so a capture reproduces the surface as well as the state.
	Status StatusState `json:"status"`

	// FSM is the shared state machine's runtime position. It is not a system, so
	// it carries its own section: which state each region stands in and how long
	// it has stood there decides when the next timed transition fires, and an
	// installed world that entered its states at different ticks reaches its next
	// escalation on a different tick than the run it reproduces.
	FSM fsm.MachineState `json:"fsm"`
}

// StatusState is the shared half of the status registry, by metric type. Keys are
// sorted within each list so a capture is byte-comparable.
type StatusState struct {
	Ints    []IntCell    `json:"ints,omitempty"`
	Bools   []BoolCell   `json:"bools,omitempty"`
	Floats  []FloatCell  `json:"floats,omitempty"`
	Strings []StringCell `json:"strings,omitempty"`
}

// IntCell is one integer metric.
type IntCell struct {
	Key   string `json:"k"`
	Value int64  `json:"v"`
}

// BoolCell is one boolean metric.
type BoolCell struct {
	Key   string `json:"k"`
	Value bool   `json:"v"`
}

// FloatCell is one float metric.
type FloatCell struct {
	Key   string  `json:"k"`
	Value float64 `json:"v"`
}

// StringCell is one string metric.
type StringCell struct {
	Key   string `json:"k"`
	Value string `json:"v"`
}

// captureStatusLocked reads every shared-surface registry cell.
//
// The filter is sharedKey, the same predicate snapshotShared compares through, so
// the capture carries exactly what a session is asserted to agree on — no more,
// and nothing the surface would then find missing.
//
// Caller MUST hold updateMutex.
func (a *App) captureStatusLocked() StatusState {
	reg := a.world.Resources.Status
	var out StatusState

	keys := reg.Ints.Keys()
	sort.Strings(keys)
	for _, k := range keys {
		if sharedKey(k) {
			out.Ints = append(out.Ints, IntCell{Key: k, Value: reg.Ints.Get(k).Load()})
		}
	}
	keys = reg.Bools.Keys()
	sort.Strings(keys)
	for _, k := range keys {
		if sharedKey(k) {
			out.Bools = append(out.Bools, BoolCell{Key: k, Value: reg.Bools.Get(k).Load()})
		}
	}
	keys = reg.Floats.Keys()
	sort.Strings(keys)
	for _, k := range keys {
		if sharedKey(k) {
			out.Floats = append(out.Floats, FloatCell{Key: k, Value: reg.Floats.Get(k).Get()})
		}
	}
	keys = reg.Strings.Keys()
	sort.Strings(keys)
	for _, k := range keys {
		if sharedKey(k) {
			out.Strings = append(out.Strings, StringCell{Key: k, Value: reg.Strings.Get(k).Load()})
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
func (a *App) installStatusLocked(state StatusState) {
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

// CaptureHeader names the tick a capture describes and the build, configuration
// and corpus it assumes. A capture installed under a different one reconstructs a
// world whose future diverges from the sender's for reasons no digest attributes,
// so the identity is checked before anything is written.
type CaptureHeader struct {
	Schema        int           `json:"schema"`
	JournalSchema uint64        `json:"journal_schema"`
	Run           uint64        `json:"run"`
	Tick          uint64        `json:"tick"`
	TickInterval  time.Duration `json:"tick_interval"`
	Seed          uint64        `json:"seed"`
	Session       uint64        `json:"session"`
	ConfigID      string        `json:"config_id"`
	ContentID     string        `json:"content_id"`
	ContentPin    string        `json:"content_pin"`
	ContentFiles  uint64        `json:"content_files"`
	ContentBlocks uint64        `json:"content_blocks"`
	ContentLines  uint64        `json:"content_lines"`

	// MapWidth and MapHeight are the D-14 shared bounds. They are simulation
	// state, not this instance's terminal, and a joiner adopts them.
	MapWidth  int `json:"map_width"`
	MapHeight int `json:"map_height"`

	// Term is the authority generation this capture was produced under, and
	// Authority the participant that produced it. Every authoritative artifact
	// carries them, because "which world is this" and "who was allowed to say so"
	// are different questions and a receiver has to answer both: it ignores an
	// artifact from a term older than the one it holds, and refuses one from a
	// term it has never been handed. Zero on a solo run's capture, which has no
	// session to be authoritative in.
	Term      network.AuthorityTerm `json:"term,omitempty"`
	Authority uint32                `json:"authority,omitempty"`

	// AuthorityCrossingSeq is the source-local sequence through which the
	// authority had completed its ordinary local-first crossings when this world
	// was read. Their receive-side ApplyTick may still be in the future, so this
	// fence — rather than the capture tick — tells a receiver which queued copies
	// the capture already contains. Barrier-bound crossings continue to use their
	// agreed ApplyTick. Zero before the authority has applied its first crossing.
	AuthorityCrossingSeq uint64 `json:"authority_crossing_seq,omitempty"`

	// Integrity is a hash over the capture's body. It answers "did this arrive
	// intact", which is a different question from "does this describe my build",
	// and both have to be answered before an install writes anything.
	Integrity uint64 `json:"integrity"`
}

// SystemStateRecord is one system's declared private state (D-19), named by the
// system rather than by position so a capture survives systems being added,
// removed or reordered between the build that wrote it and the build that reads it.
//
// Data is opaque bytes, not embedded JSON. SaveShared promises bytes and nothing
// more, and the wall system's carrier proves the distinction matters: it hands
// over the maze generator's own binary form, which is not JSON at all.
type SystemStateRecord struct {
	System string `json:"system"`
	Data   []byte `json:"data"`
}

// CaptureShared reads the shared world at the current tick.
//
// The whole capture is taken inside one critical section. A capture assembled
// across two ticks would describe a world that never existed: entity placements
// from one tick beside a stream position from the next.
func (a *App) CaptureShared() (SharedCapture, error) {
	var (
		cap                     SharedCapture
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
		cap.Header = CaptureHeader{
			Term:          term,
			Authority:     holder,
			Schema:        SnapshotSchema,
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
		return SharedCapture{}, err
	}
	cap.Header.ContentPin = service.MustGet[*service.ContentService](a.hub, "content").Pin()

	cap.Header.Integrity, err = captureIntegrity(cap)
	if err != nil {
		return SharedCapture{}, err
	}
	return cap, nil
}

// captureSystemStatesLocked collects every declared carrier's state, in system
// name order so two instances holding equal state produce equal bytes.
//
// Caller MUST hold updateMutex.
func (a *App) captureSystemStatesLocked() ([]SystemStateRecord, error) {
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

	out := make([]SystemStateRecord, 0, len(carriers))
	for _, c := range carriers {
		data, err := c.saver.SaveShared()
		if err != nil {
			return nil, fmt.Errorf("capture %s: %w", c.name, err)
		}
		out = append(out, SystemStateRecord{System: c.name, Data: data})
	}
	return out, nil
}

// InstallShared replaces this instance's shared world with a capture.
//
// The identity is checked, then the integrity, then every system's state is
// offered — and only if all of that succeeds does anything get written. A world
// half-installed is worse than one not installed at all: the first is a
// divergence that looks like a working session.
//
// This is the direct form: it writes into the world it is called on. A running
// instance takes StageShared instead, which resolves the capture into a second
// world first and swaps at a tick boundary — see snapshot_stage.go.
func (a *App) InstallShared(cap SharedCapture) error {
	if err := a.VerifyCapture(cap); err != nil {
		return err
	}
	return a.installShared(cap)
}

// installShared writes a capture whose identity has already been established, by
// replacing the shared world wholesale.
func (a *App) installShared(cap SharedCapture) error {
	_, err := a.writeShared(cap, false)
	return err
}

// reconcileShared writes a capture by moving the live world onto it rather than
// replacing it, and reports how far apart the two were.
//
// The difference is read first and inside the same critical section as the write,
// because it is a statement about one instant: the world this instance predicted
// against the world the authority is handing it. Read a tick later and it would be
// the magnitude of a correction that had already happened.
func (a *App) reconcileShared(cap SharedCapture) (engine.WorldDifference, error) {
	return a.writeShared(cap, true)
}

// writeShared is the one install, with the store pass chosen by the caller.
//
// Everything outside that pass is identical and has to be: the roster rebind, the
// tick and record rebase, the stream positions, every declared carrier, the FSM
// and the compared surface are what make the world the sender's, and a correction
// that skipped any of them would leave an instance that looks corrected and is not.
func (a *App) writeShared(cap SharedCapture, reconcile bool) (engine.WorldDifference, error) {
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
		if e := a.scheduler.ImportFSM(cap.FSM); e != nil {
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
func (a *App) adoptSnapshotBarrierLocked(header CaptureHeader) {
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
func (a *App) VerifyCapture(cap SharedCapture) error {
	if cap.Header.Schema != SnapshotSchema {
		return fmt.Errorf("capture schema %d, this build reads %d",
			cap.Header.Schema, SnapshotSchema)
	}
	want, err := captureIntegrity(cap)
	if err != nil {
		return err
	}
	if want != cap.Header.Integrity {
		return errors.New("capture integrity hash does not match its body")
	}

	return firstAnchorMismatch("capture", a.anchorIdentity(captureAnchor(cap.Header)))
}

// captureAnchor is the identity half of a capture header, in the shape the journal
// anchor comparison takes.
//
// It is a function rather than an inline literal because a header now reaches that
// comparison from two places: a whole capture, which carries its own body and is
// verified with it, and a correction manifest, which carries the header alone and
// has to answer "is this my session" before this instance reads its own world to
// compare against it.
func captureAnchor(h CaptureHeader) event.JournalAnchor {
	return event.JournalAnchor{
		Schema:        h.JournalSchema,
		Seed:          h.Seed,
		Session:       h.Session,
		ConfigID:      h.ConfigID,
		ContentID:     h.ContentID,
		ContentPin:    h.ContentPin,
		ContentFiles:  h.ContentFiles,
		ContentBlocks: h.ContentBlocks,
		ContentLines:  h.ContentLines,
		TickInterval:  int64(h.TickInterval),
	}
}

// captureIntegrity hashes a capture's body with its header's own integrity field
// zeroed, so the value covers everything except itself.
func captureIntegrity(cap SharedCapture) (uint64, error) {
	cap.Header.Integrity = 0
	body, err := json.Marshal(cap)
	if err != nil {
		return 0, fmt.Errorf("capture encode: %w", err)
	}
	h := fnv.New64a()
	_, _ = h.Write(body)
	return h.Sum64(), nil
}

// EncodeCapture renders a capture in the bounded, compressed wire envelope.
func EncodeCapture(cap SharedCapture) ([]byte, error) { return encodeSnapshotJSON(cap) }

// DecodeCapture parses what EncodeCapture produced. It does not validate: the
// caller passes the result to VerifyCapture or InstallShared, which do.
func DecodeCapture(b []byte) (SharedCapture, error) {
	var cap SharedCapture
	if err := decodeSnapshotJSON(b, &cap); err != nil {
		return SharedCapture{}, fmt.Errorf("capture decode: %w", err)
	}
	return cap, nil
}
