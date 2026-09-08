package app

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/journal"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/service"
)

// ConfigFromAnchor rebuilds the configuration a journal was recorded under, as a headless config; NewReplay retargets it for presentation.
// Speed is dropped: a replay runs the manual clock, which a headless config
// rejects a rate for. The result asks for a corpus and a config; VerifyAnchor
// proves the ones that resolved are the recorded ones.
func ConfigFromAnchor(a event.JournalAnchor) (Config, error) {
	if a.Schema != event.JournalSchema {
		return Config{}, fmt.Errorf("journal schema %d, this build reads %d", a.Schema, event.JournalSchema)
	}
	if a.TickInterval != int64(parameter.GameUpdateInterval) {
		return Config{}, fmt.Errorf("journal tick interval %dns, this build ticks at %dns",
			a.TickInterval, int64(parameter.GameUpdateInterval))
	}
	if a.StartRun != 0 || a.StartTick != 0 {
		return Config{}, fmt.Errorf("journal opened mid-run at run %d tick %d; replaying one needs a world snapshot",
			a.StartRun, a.StartTick)
	}
	if a.Seed == 0 {
		return Config{}, errors.New("anchor carries no seed")
	}

	// The recorded map latch travels with the geometry it was derived from, so a
	// reproduction installs it before the FSM boots rather than re-deriving it from
	// the terminal the anchor names (D-14).
	cfg := Config{
		Mode: ModeHeadless, Seed: a.Seed, Session: a.Session,
		Width: a.Width, Height: a.Height,
		MapWidth: a.MapWidth, MapHeight: a.MapHeight, CropOnResize: a.CropOnResize,
		LockMap: a.SessionShared,
	}

	// Embedded on both sides is the only pairing Config states exactly; a mixed
	// anchor leaves the embedded side to discovery, which VerifyAnchor then rejects
	if a.ConfigID == embeddedLabel && a.ContentID == embeddedLabel {
		cfg.Resources.Embedded = true
		return cfg, cfg.Validate()
	}
	if a.ConfigID != embeddedLabel {
		cfg.Resources.Game = a.ConfigID
	}
	if a.ContentID != embeddedLabel {
		cfg.Resources.Content = a.ContentID
		if a.ContentPin != "" {
			cfg.Resources.Content = filepath.Join(a.ContentID, a.ContentPin) // ResolveContent re-splits
		}
	}
	return cfg, cfg.Validate()
}

// anchorField is one value the anchor names and this App must reproduce
type anchorField struct {
	name      string
	want, got any
}

// anchorIdentity is what any two participants in one session must agree on: record
// layout and tick rate, the seed and session counter every RNG stream derives from,
// and the config and corpus the simulation reads. Terminal geometry is absent — it is
// per-instance. Shared by VerifyAnchor and the join handshake, so the two agree.
func (a *App) anchorIdentity(an event.JournalAnchor) []anchorField {
	reg := a.world.Resources.Status
	svc := service.MustGet[*service.ContentService](a.hub, "content")
	return []anchorField{
		{"schema", an.Schema, uint64(event.JournalSchema)},
		{"seed", an.Seed, a.world.Resources.Rand.Root()},
		{"session", an.Session, a.world.Resources.Rand.Session()},
		{"config_id", an.ConfigID, resolveConfigID(a.cfg)},
		{"content_id", an.ContentID, reg.Strings.Get("content.source").Load()},
		{"content_pin", an.ContentPin, svc.Pin()},
		{"content_files", an.ContentFiles, uint64(reg.Ints.Get("content.files").Load())},
		{"content_blocks", an.ContentBlocks, uint64(reg.Ints.Get("content.blocks").Load())},
		{"content_lines", an.ContentLines, uint64(reg.Ints.Get("content.lines").Load())},
		{"tick_ns", an.TickInterval, int64(parameter.GameUpdateInterval)},
	}
}

// firstAnchorMismatch reports the first field this App does not reproduce
func firstAnchorMismatch(kind string, fields []anchorField) error {
	for _, f := range fields {
		if f.want != f.got {
			return fmt.Errorf("%s mismatch: %s recorded %v, this run has %v", kind, f.name, f.want, f.got)
		}
	}
	return nil
}

// VerifyAnchor reports whether this App reproduces what the anchor recorded.
// A resolved path proves which corpus was asked for, not which one loaded, so the
// fingerprint is compared after construction: a discovered file or a changed corpus
// becomes a startup error instead of an unexplained snapshot diff many ticks later.
// Call after NewHeadless, before Replay.
func (a *App) VerifyAnchor(an event.JournalAnchor) error {
	fields := append(a.anchorIdentity(an),
		anchorField{"width", an.Width, a.ctx.Width},
		anchorField{"height", an.Height, a.ctx.Height})
	return firstAnchorMismatch("anchor", fields)
}

// newReplayDriver checks App-specific policy, then hands the record timeline to
// internal/journal. The driver itself knows only the small replayTarget contract.
func newReplayDriver(a *App, records []event.JournalRecord) (*journal.ReplayDriver, error) {
	if !a.cfg.Mode.Driven() {
		return nil, errors.New("replay: requires a caller-driven App")
	}
	if a.cfg.Journal {
		return nil, errors.New("replay: journaling a replay records a run that never happened")
	}
	return journal.NewReplayDriver(replayTarget{a: a}, records), nil
}

type replayTarget struct{ a *App }

func (t replayTarget) Position() event.Stamp { return t.a.Position() }
func (t replayTarget) Tick(n int)            { t.a.Tick(n) }
func (t replayTarget) Settle()               { t.a.Settle() }
func (t replayTarget) PushRecord(rec event.JournalRecord, payload any) {
	t.a.world.PushRecord(rec.Type, payload, rec.Origin, rec.Domain)
}

// Replay consumes an entire record stream. The caller runs any trailing ticks the
// last record misses.
func (a *App) Replay(records []event.JournalRecord) (journal.ReplayStats, error) {
	d, err := newReplayDriver(a, records)
	if err != nil {
		return journal.ReplayStats{Records: len(records)}, err
	}
	err = d.RunAll()
	return d.Stats(), err
}
