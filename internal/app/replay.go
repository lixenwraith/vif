// Package app: journal replay.
//
// The driver re-pushes journal records into a fresh headless App at the position they
// were recorded at. Position is (run, tick, boundary), stamped on the event queue under
// the world lock: run advances where a game reset re-bases the tick counter, tick at the
// top of each tick body, boundary on each completed settle group. A record stamped
// (R, T, b) was produced after tick T of run R completed, in the b'th settle since.
//
// Nothing is filtered. Every non-system-origin record is injected, including pause,
// rate and step control — the replay does not honour those, but it does reproduce
// their events, and what must not be compared is declared once in denySim rather
// than judged per event at the injection site.
//
// Settle granularity is recorded, not assumed: a pass can queue a system event that a
// later pass applies over a replayed one, so merging two settles into one changes the
// result. A run boundary is followed, not driven: the reset that opens run R+1 is a
// record in run R, and servicing it is the replay's own reset path.
//
// Bit-exact reproduction is claimed for headless source runs only. A live run races
// two scheduler goroutines against the main loop for the update mutex, so its journal
// reconstructs what the player did, not a comparable world.
package app

import (
	"cmp"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"sync"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/service"
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
	records []event.JournalRecord
	anchors []event.JournalAnchor
}

// NewCapture creates an empty capture sink
func NewCapture() *Capture { return &Capture{} }

// Record appends one record; the queue stamped it at push time, so the sink needs
// no correlation source of its own
func (c *Capture) Record(r event.JournalRecord) {
	c.mu.Lock()
	c.records = append(c.records, r)
	c.mu.Unlock()
}

// Anchor appends one header record
func (c *Capture) Anchor(a event.JournalAnchor) {
	c.mu.Lock()
	c.anchors = append(c.anchors, a)
	c.mu.Unlock()
}

// Records returns a copy of the captured records in emission order
func (c *Capture) Records() []event.JournalRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]event.JournalRecord, len(c.records))
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

// CheckDense reports the first jseq gap; a gap is exactly one lost record.
// Sorted, so a concurrent producer's append order cannot read as a gap.
func (c *Capture) CheckDense() error {
	recs := c.Records()
	slices.SortFunc(recs, func(x, y event.JournalRecord) int { return cmp.Compare(x.JSeq, y.JSeq) })
	for i := range recs {
		if want := uint64(i + 1); recs[i].JSeq != want {
			return fmt.Errorf("journal gap at index %d: jseq %d, want %d", i, recs[i].JSeq, want)
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
	if a.StartRun != 0 || a.StartTick != 0 {
		return Config{}, fmt.Errorf("journal opened mid-run at run %d tick %d; replaying one needs a world snapshot",
			a.StartRun, a.StartTick)
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

// groupKey identifies one settle group: a run, a tick within it, and the settles
// completed within that tick
type groupKey struct{ run, tick, boundary uint64 }

func keyOf(r event.JournalRecord) groupKey { return groupKey{r.Run, r.Tick, r.Boundary} }

// before reports whether k precedes o in emission order
func (k groupKey) before(o groupKey) bool {
	if k.run != o.run {
		return k.run < o.run
	}
	if k.tick != o.tick {
		return k.tick < o.tick
	}
	return k.boundary < o.boundary
}

// ReplayStats reports what a replay consumed
type ReplayStats struct {
	Records  int    // records offered
	Injected int    // records pushed into the queue
	Groups   int    // settle groups executed
	Run      uint64 // reset generation the replay ended in
	Ticks    uint64 // ticks executed within that run
}

// Replay injects records at their recorded run, tick and settle boundary, advancing
// the clock between ticks. Records must arrive in emission order; each group is sorted
// in place by queue slot, which is dispatch order. The caller runs any trailing ticks
// the last record misses.
func (a *App) Replay(records []event.JournalRecord) (ReplayStats, error) {
	st := ReplayStats{Records: len(records)}
	if !a.cfg.Headless {
		return st, errors.New("replay: requires a headless App")
	}
	if a.cfg.Journal {
		return st, errors.New("replay: journaling a replay records a run that never happened")
	}

	var cur groupKey
	for i := 0; i < len(records); {
		k := keyOf(records[i])
		if k.before(cur) {
			return st, fmt.Errorf("replay: jseq %d stamped run %d tick %d boundary %d, out of order",
				records[i].JSeq, k.run, k.tick, k.boundary)
		}

		// A run change is opened by the reset the previous run's records requested,
		// so the driver follows the queue's generation rather than driving it
		if k.run != cur.run {
			if got := a.runIndex(); got != k.run {
				return st, fmt.Errorf("replay: jseq %d opens run %d, the replay is in run %d",
					records[i].JSeq, k.run, got)
			}
			cur = groupKey{run: k.run}
		}
		if k.tick > cur.tick {
			a.Tick(int(k.tick - cur.tick))
			cur.tick = k.tick
		}

		j := i
		for j < len(records) && keyOf(records[j]) == k {
			j++
		}
		group := records[i:j]
		sort.SliceStable(group, func(x, y int) bool { return group[x].Seq < group[y].Seq })

		injected, err := a.injectGroup(group)
		st.Injected += injected
		st.Groups++
		if err != nil {
			return st, err
		}
		cur, i = k, j
	}
	st.Run, st.Ticks = cur.run, cur.tick
	return st, nil
}

// runIndex returns the reset generation the replay's event queue is stamping
func (a *App) runIndex() uint64 { return a.world.Resources.Event.Queue.Stamp().Run }

// injectGroup pushes one group's records and settles them together
func (a *App) injectGroup(group []event.JournalRecord) (int, error) {
	pushed := 0
	for i := range group {
		rec := &group[i]
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
