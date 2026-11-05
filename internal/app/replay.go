package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/lixenwraith/vif/internal/app/internal/scribe"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// ConfigFromAnchor rebuilds the headless config a journal was recorded under, mid-run
// openings included; PlayJournal retargets it for presentation. Speed is dropped: the
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
	t.a.world.RunSafe(func() { d = t.a.scribe.JournalDigestLocked() })
	return d
}

// Install writes a world the recorded run wrote, rebuilt from the one this replay
// holds, as the participant it wrote it as: identity first, because the write binds
// cursors by it, and a join writes before its transport attaches or notes anything.
func (t replayTarget) Install(c event.JournalCapture) error {
	var (
		d   snapshot.WrittenDelta
		own scribe.OwnState
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
			err = a.scribe.OpenedLocked(c, own)
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
