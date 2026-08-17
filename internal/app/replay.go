// Package app: journal replay.
//
// The driver re-pushes journal records into a fresh headless App at the tick they
// were recorded on. A record carries the vlog correlation tick, which
// ClockScheduler.processTick sets to GetGameTicks()+1 before the tick body, so a
// record stamped T was produced after tick T completed and before tick T+1: the
// driver runs tick T, then injects and settles every record stamped T.
//
// Nothing is filtered. Every non-system-origin record is injected, including pause,
// rate and step control — the replay does not honour those, but it does reproduce
// their events, and what must not be compared is declared once in denySim rather
// than judged per event at the injection site.
//
// Settle granularity inside a tick boundary is not observable: the queue returns Seq
// order however a batch was split, and no handler on the dispatch path accumulates
// across passes. TestReplaySplitSettle is the tripwire for the first one that does.
//
// Bit-exact reproduction is claimed for headless source runs only. A live run races
// two scheduler goroutines against the main loop for the update mutex, so its journal
// reconstructs what the player did, not a comparable world.
package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/service"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// ReplayRecord is one journal record paired with the tick it was produced on
type ReplayRecord struct {
	Rec  event.JournalRecord
	Tick uint64
}

// Capture is an in-memory JournalSink for in-process replay. Retaining the
// record is safe because every field is a value: nothing references the pooled
// payload the producer still owns.
type Capture struct {
	mu      sync.Mutex
	records []ReplayRecord
	anchors []event.JournalAnchor
}

// NewCapture creates an empty capture sink
func NewCapture() *Capture { return &Capture{} }

// Record appends one record, stamping the tick from the same correlation source
// the file sink uses, so an in-process capture and a rotated file agree.
// A build without vlog (novlog, wasm) reports no tick and cannot be aligned.
func (c *Capture) Record(r event.JournalRecord) {
	_, tick, _ := vlog.Stamp()
	c.mu.Lock()
	c.records = append(c.records, ReplayRecord{Rec: r, Tick: tick})
	c.mu.Unlock()
}

// Anchor appends one header record
func (c *Capture) Anchor(a event.JournalAnchor) {
	c.mu.Lock()
	c.anchors = append(c.anchors, a)
	c.mu.Unlock()
}

// Records returns a copy of the captured records in emission order
func (c *Capture) Records() []ReplayRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ReplayRecord, len(c.records))
	copy(out, c.records)
	return out
}

// Anchors returns a copy of the captured anchors; the first describes the run
func (c *Capture) Anchors() []event.JournalAnchor {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]event.JournalAnchor, len(c.anchors))
	copy(out, c.anchors)
	return out
}

// CheckDense reports the first jseq gap; a gap is exactly one lost record
func (c *Capture) CheckDense() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.records {
		if want := uint64(i + 1); c.records[i].Rec.JSeq != want {
			return fmt.Errorf("journal gap at index %d: jseq %d, want %d",
				i, c.records[i].Rec.JSeq, want)
		}
	}
	return nil
}

// ConfigFromAnchor rebuilds the configuration a journal was recorded under.
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
	if a.Seed == 0 {
		return Config{}, errors.New("anchor carries no seed")
	}

	cfg := Config{Headless: true, Seed: a.Seed, Width: a.Width, Height: a.Height}

	// Embedded on both sides is the only pairing Config states exactly; a mixed
	// anchor leaves the embedded side to discovery, which VerifyAnchor then rejects
	if a.ConfigID == embeddedLabel && a.ContentID == embeddedLabel {
		cfg.ForceDefault = true
		return cfg, cfg.Validate()
	}
	if a.ConfigID != embeddedLabel {
		cfg.GameScript = a.ConfigID
	}
	if a.ContentID != embeddedLabel {
		cfg.ContentPath = a.ContentID
		if a.ContentPin != "" {
			cfg.ContentPath = filepath.Join(a.ContentID, a.ContentPin) // ResolveContent re-splits
		}
	}
	return cfg, cfg.Validate()
}

// VerifyAnchor reports whether this App reproduces what the anchor recorded.
// A resolved path proves which corpus was asked for, not which one loaded, so the
// fingerprint is compared after construction: a discovered file or a changed corpus
// becomes a startup error instead of an unexplained snapshot diff many ticks later.
// Call after NewHeadless, before Replay.
func (a *App) VerifyAnchor(an event.JournalAnchor) error {
	reg := a.world.Resources.Status
	svc := service.MustGet[*service.ContentService](a.hub, "content")

	for _, f := range []struct {
		name      string
		want, got any
	}{
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
		{"width", an.Width, a.ctx.Width},
		{"height", an.Height, a.ctx.Height},
	} {
		if f.want != f.got {
			return fmt.Errorf("anchor mismatch: %s recorded %v, this run has %v", f.name, f.want, f.got)
		}
	}
	return nil
}

// ReplayStats reports what a replay consumed
type ReplayStats struct {
	Records  int    // records offered
	Injected int    // records pushed into the queue
	Ticks    uint64 // ticks executed, i.e. the tick of the last record
}

// Replay injects records at their recorded tick, advancing the clock between
// groups. Records must arrive in emission order; each tick group is sorted in
// place by queue slot, which is dispatch order. The caller runs any trailing ticks
// the last record misses.
func (a *App) Replay(records []ReplayRecord) (ReplayStats, error) {
	st := ReplayStats{Records: len(records)}
	if !a.cfg.Headless {
		return st, errors.New("replay: requires a headless App")
	}
	if a.cfg.Journal {
		return st, errors.New("replay: journaling a replay records a run that never happened")
	}

	for i := 0; i < len(records); {
		tick := records[i].Tick
		if tick < st.Ticks {
			return st, fmt.Errorf("replay: jseq %d stamped tick %d, already at tick %d",
				records[i].Rec.JSeq, tick, st.Ticks)
		}
		if tick > st.Ticks {
			a.Tick(int(tick - st.Ticks))
			st.Ticks = tick
		}

		j := i
		for j < len(records) && records[j].Tick == tick {
			j++
		}
		group := records[i:j]
		sort.SliceStable(group, func(x, y int) bool { return group[x].Rec.Seq < group[y].Rec.Seq })

		injected, err := a.injectGroup(group)
		st.Injected += injected
		if err != nil {
			return st, err
		}
		i = j
	}
	return st, nil
}

// injectGroup pushes one tick's records and settles them together
func (a *App) injectGroup(group []ReplayRecord) (int, error) {
	pushed := 0
	for i := range group {
		rec := &group[i].Rec
		if err := checkRecord(rec); err != nil {
			return pushed, fmt.Errorf("replay: jseq %d: %w", rec.JSeq, err)
		}
		payload, err := decodeRecordPayload(rec)
		if err != nil {
			return pushed, fmt.Errorf("replay: jseq %d %s: %w", rec.JSeq, event.GetEventName(rec.Type), err)
		}
		a.ctx.PushEventOrigin(rec.Type, payload, rec.Origin)
		pushed++
	}
	if pushed > 0 {
		a.Settle()
	}
	return pushed, nil
}

// checkRecord rejects a record this build cannot inject faithfully
func checkRecord(rec *event.JournalRecord) error {
	if rec.EncodeErr != "" {
		return fmt.Errorf("encode error %q: the payload was never captured", rec.EncodeErr)
	}
	if rec.Type <= event.EventNone || int(rec.Type) >= event.EventTypeCount ||
		event.GetEventName(rec.Type) == "" {
		return fmt.Errorf("unregistered event type %d", rec.Type)
	}
	if !rec.Origin.Journaled() {
		return fmt.Errorf("origin %s is not a journaled producer", rec.Origin)
	}
	return nil
}

// decodeRecordPayload allocates a fresh payload from the registry prototype and
// decodes the recorded text into it. Empty text means the producer pushed nil.
// Never a pooled payload: release is a consumer concern and a replayed payload
// has no owner.
func decodeRecordPayload(rec *event.JournalRecord) (any, error) {
	if rec.Payload == "" {
		return nil, nil
	}
	p := event.NewPayloadStruct(rec.Type)
	if p == nil {
		return nil, errors.New("payload text with no registry prototype")
	}
	if err := toml.Unmarshal([]byte(rec.Payload), p); err != nil {
		return nil, err
	}
	return p, nil
}
