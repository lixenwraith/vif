package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/lixenwraith/vif/internal/app/internal/corrector"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/service"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/pkg/position"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// ConfigFromAnchor rebuilds the headless config a journal was recorded under, mid-run
// openings included; NewReplay retargets it for presentation. Speed is dropped: the
// manual clock takes no rate. The scenario is asked for by name, so one outside the
// config roots needs -config-dir; VerifyAnchor proves what resolved is what ran.
func ConfigFromAnchor(a event.JournalAnchor) (Config, error) {
	if a.Schema != event.JournalSchema {
		return Config{}, fmt.Errorf("journal schema %d, this build reads %d", a.Schema, event.JournalSchema)
	}
	tick := time.Duration(a.TickInterval)
	if tick < parameter.TickIntervalMin || tick > parameter.TickIntervalMax {
		return Config{}, fmt.Errorf("journal tick interval %s is outside %s to %s",
			tick, parameter.TickIntervalMin, parameter.TickIntervalMax)
	}
	if a.Seed == 0 {
		return Config{}, errors.New("anchor carries no seed")
	}

	// The recorded map latch travels with the geometry it was derived from, so a
	// reproduction installs it before the FSM boots rather than re-deriving it from
	// the terminal the anchor names (D-14).
	cfg := Config{
		Mode: ModeHeadless, Seed: a.Seed, Session: a.Session,
		Width: a.Width, Height: a.Height, TickInterval: tick,
		MapWidth: a.MapWidth, MapHeight: a.MapHeight, CropOnResize: a.CropOnResize,
		CellAspect: vmath.Aspect(a.CellAspect), LockMap: a.SessionShared,
	}

	// Embedded on both sides is the only pairing Config states exactly; a mixed
	// anchor leaves the embedded side to discovery, which VerifyAnchor then rejects
	if a.ScenarioID == resource.EmbeddedLabel && a.ContentID == resource.EmbeddedLabel {
		cfg.Resources.Embedded = true
		return cfg, cfg.Validate()
	}
	if a.ScenarioID != resource.EmbeddedLabel {
		cfg.Resources.Scenario = a.ScenarioID
	}
	if a.ContentID != resource.EmbeddedLabel {
		cfg.Resources.Content = a.ContentID
		if a.ContentPin != "" {
			cfg.Resources.Content = filepath.Join(a.ContentID, a.ContentPin) // ResolveContent re-splits
		}
	}
	return cfg, cfg.Validate()
}

// sessionAnchorFields are what two participants in one session must agree on:
// record layout and tick rate, the seed and session counter every RNG stream
// derives from, and the scenario's bytes, whose name is only a lookup hint.
// Geometry is per-instance, and the corpus is player domain, read from each
// machine's own roots and never reconciled (D-11, doc/multi-player.md).
func (sc *scribe) sessionAnchorFields(an event.JournalAnchor) []corrector.AnchorField {
	return []corrector.AnchorField{
		{Name: "schema", Want: an.Schema, Got: uint64(event.JournalSchema)},
		{Name: "seed", Want: an.Seed, Got: sc.world.Resources.Rand.Root()},
		{Name: "session", Want: an.Session, Got: sc.world.Resources.Rand.Session()},
		{Name: "scenario_digest", Want: an.ScenarioDigest, Got: sc.scenario.Digest()},
		{Name: "tick_ns", Want: an.TickInterval, Got: int64(sc.world.TickInterval())},
	}
}

// anchorIdentity adds what a replay must reproduce and a join must not require.
// A recorded run typed the blocks of one corpus, so replaying its input against
// another yields different glyphs; a peer joining live only has to agree on the
// world, and brings its own text into it.
func (sc *scribe) anchorIdentity(an event.JournalAnchor) []corrector.AnchorField {
	reg := sc.world.Resources.Status
	svc := service.MustGet[*service.ContentService](sc.hub, "content")
	return append(sc.sessionAnchorFields(an),
		corrector.AnchorField{Name: "content_id", Want: an.ContentID, Got: reg.Strings.Get("content.source").Load()},
		corrector.AnchorField{Name: "content_pin", Want: an.ContentPin, Got: svc.Pin()},
		corrector.AnchorField{Name: "content_files", Want: an.ContentFiles, Got: uint64(reg.Ints.Get("content.files").Load())},
		corrector.AnchorField{Name: "content_blocks", Want: an.ContentBlocks, Got: uint64(reg.Ints.Get("content.blocks").Load())},
		corrector.AnchorField{Name: "content_lines", Want: an.ContentLines, Got: uint64(reg.Ints.Get("content.lines").Load())},
	)
}

// VerifyAnchor reports whether this App reproduces what the anchor recorded.
// A resolved path proves which corpus was asked for, not which one loaded, so the
// fingerprint is compared after construction: a discovered file or a changed corpus
// becomes a startup error instead of an unexplained snapshot diff many ticks later.
// Call after NewHeadless, before Replay.
func (sc *scribe) VerifyAnchor(an event.JournalAnchor) error {
	fields := append(sc.anchorIdentity(an),
		corrector.AnchorField{Name: "width", Want: an.Width, Got: sc.ctx.Width},
		corrector.AnchorField{Name: "height", Want: an.Height, Got: sc.ctx.Height})
	return corrector.FirstAnchorMismatch("anchor", fields)
}

// newReplayDriver checks App-specific policy, then hands the stream to
// internal/journal. The driver itself knows only the small replayTarget contract.
func newReplayDriver(a *App, s *journal.Stream) (*journal.ReplayDriver, error) {
	if !a.cfg.Mode.Driven() {
		return nil, errors.New("replay: requires a caller-driven App")
	}
	if a.cfg.Journal {
		return nil, errors.New("replay: journaling a replay records a run that never happened")
	}
	// The recorded run's session and the authority's worlds that settled its
	// predictions are not here; what they decided is in the records.
	a.world.RunSafe(func() { a.world.FollowJournal() })
	return journal.NewReplayDriver(replayTarget{a: a}, s), nil
}

type replayTarget struct{ a *App }

func (t replayTarget) Position() event.Stamp { return t.a.Position() }
func (t replayTarget) Tick(n int)            { t.a.Tick(n) }
func (t replayTarget) Settle()               { t.a.Settle() }
func (t replayTarget) PushRecord(rec event.JournalRecord, payload any) bool {
	return t.a.world.PushRecord(rec, payload)
}
func (t replayTarget) PushBody(rec event.JournalRecord, payload any) {
	t.a.world.PushBody(rec, payload)
}

func (t replayTarget) Digest() (d event.JournalDigest) {
	t.a.world.RunSafe(func() { d = t.a.scribe.journalDigestLocked() })
	return d
}

// journalDigestLocked is the world a journal's digest carries: both domains, since
// a replay reproduces the player's own as well. Caller MUST hold updateMutex.
func (sc *scribe) journalDigestLocked() event.JournalDigest {
	return snapshot.DigestWorld(sc.world, position.ScopeAll).Journal()
}

// Install writes a world the recorded run wrote, rebuilt from the one this replay
// holds, as the participant it wrote it as: identity first, because the write binds
// cursors by it, and a join writes before its transport attaches or notes anything.
func (t replayTarget) Install(c event.JournalCapture) error {
	var (
		d   snapshot.WrittenDelta
		own ownState
	)
	if err := snapshot.DecodeJSON(c.Body, &d); err != nil {
		return err
	}
	if c.Opens {
		if err := snapshot.DecodeJSON(c.Own, &own); err != nil {
			return fmt.Errorf("the run's own side: %w", err)
		}
	}
	a := t.a
	var err error
	a.world.RunSafe(func() {
		var before, cap snapshot.SharedCapture
		if before, err = a.glue.CaptureSharedLocked(); err == nil {
			cap, err = a.glue.Encoder().ApplyWritten(before, d)
		}
		if err != nil {
			return
		}
		// Participant zero wrote it solo, with no session.
		if c.Participant != 0 {
			s := a.world.Session()
			s.Participant, s.Authority = c.Participant, c.Authority
			a.world.AdoptSession(s)
			a.world.MarkSessionShared()
			a.ctx.PublishMapLock()
		}
		_, err = a.glue.WriteSharedLocked(cap, &before, true, c.Opens)
		if err == nil && c.Opens {
			err = a.scribe.openedLocked(c, own)
		}
		// The hold the run landed in with this world, held here as it was there.
		if err == nil && c.Held && !c.Opens {
			if c.Start {
				a.ctx.HoldAs(engine.HoldStart)
			} else {
				a.ctx.Hold(true)
			}
		}
		// The barrier stands as the run's write left it before anything still queued
		// dispatches: a crossing raised before the next tick is the run's, by record.
		if err == nil && c.Barrier && !a.barrierLocked() {
			a.membership.activateNetworkSessionLocked()
			if !a.barrierLocked() {
				err = errors.New("the run's write engaged its crossing barrier, which this replay cannot")
			}
		}
	})
	return err
}

// openedLocked settles what writing a journal's opening world queued, with input
// bound to the slot the run followed, writes the run's own side over what that
// derived, then takes up the play mode, the hold and the systems the run stood in.
// Caller MUST hold updateMutex.
func (sc *scribe) openedLocked(c event.JournalCapture, own ownState) error {
	sc.world.PushLocalOrigin(event.EventCursorSetLocalRequest, &event.CursorSetLocalPayload{Slot: c.Slot}, event.OriginSystem)
	sc.scheduler.SettleLocked()
	if err := sc.installOwnLocked(own); err != nil {
		return err
	}
	sc.ctx.SetPlayMode(c.Mode)
	sc.world.UpdateBoundsRadius()
	if c.Start {
		sc.ctx.TimeCtl.HoldAs(engine.HoldStart)
	} else {
		sc.ctx.TimeCtl.SetHeld(c.Held)
	}
	for _, sys := range sc.world.Systems() {
		if t, ok := sys.(engine.Toggled); ok {
			t.SetEnabled(!slices.Contains(c.Off, sys.Name()))
		}
	}
	return nil
}

// loadJournal reads a journal set and the configuration that reproduces it. The
// viewer supplies where the scenario the anchor names resolves, as the run's
// -config-dir did.
func loadJournal(viewer Config, paths []string) (journal.Set, Config, error) {
	event.EnsureRegistry()
	set, err := journal.Load(paths...)
	if err != nil {
		return set, Config{}, err
	}
	if len(set.Anchors) == 0 {
		return set, Config{}, errors.New("journal carries no anchor")
	}
	an := set.Anchors[0]
	if (an.StartRun != 0 || an.StartTick != 0) && (len(set.Captures) == 0 || !set.Captures[0].Opens) {
		return set, Config{}, fmt.Errorf("journal opened at run %d tick %d, and the world it opened on is in none of these files",
			an.StartRun, an.StartTick)
	}
	cfg, err := ConfigFromAnchor(an)
	cfg.Resources.Dir = viewer.Resources.Dir
	return set, cfg, err
}

// VerifyJournal replays a journal flat out without presenting it, up to its end or
// the first digest it does not reproduce, which is returned as the error.
func VerifyJournal(viewer Config, paths ...string) (journal.ReplayStats, error) {
	set, cfg, err := loadJournal(viewer, paths)
	if err != nil {
		return journal.ReplayStats{}, err
	}
	if err := set.CheckDense(); err != nil {
		return journal.ReplayStats{Records: len(set.Records)}, err
	}
	a, err := NewHeadless(cfg)
	if err != nil {
		return journal.ReplayStats{}, err
	}
	defer a.Close()
	if err := a.VerifyAnchor(set.Anchors[0]); err != nil {
		return journal.ReplayStats{}, err
	}
	d, err := newReplayDriver(a, set.Stream())
	if err != nil {
		return journal.ReplayStats{}, err
	}
	for more := true; more && d.Stats().Diverged == nil; {
		if more, err = d.Step(); err != nil {
			return d.Stats(), err
		}
	}
	if st := d.Stats(); st.Diverged != nil {
		return st, st.Diverged
	}
	return d.Stats(), nil
}

// Replay consumes an entire record stream. The caller runs any trailing ticks the
// last record misses.
func (a *App) Replay(records []event.JournalRecord) (journal.ReplayStats, error) {
	d, err := newReplayDriver(a, journal.Set{Records: records}.Stream())
	if err != nil {
		return journal.ReplayStats{Records: len(records)}, err
	}
	err = d.RunAll()
	return d.Stats(), err
}
