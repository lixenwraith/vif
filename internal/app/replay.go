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
//
// Tick reconciliation. A run ends only at a journaled record: NextRun is reached
// solely through EventGameResetRequest, which never carries OriginSystem, so the last
// record of run R is stamped at R's final tick and the driver ticks to it. Only the
// final run can end with unrecorded trailing ticks, which the caller runs itself.
// Anything else that re-bases the tick counter breaks this and silently under-ticks
// every run but the last.
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
		Mode: ModeHeadless, Seed: a.Seed, Width: a.Width, Height: a.Height,
		MapWidth: a.MapWidth, MapHeight: a.MapHeight, CropOnResize: a.CropOnResize,
		LockMap: a.SessionShared,
	}

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

// anchorField is one value the anchor names and this App must reproduce
type anchorField struct {
	name      string
	want, got any
}

// anchorIdentity is what any two participants in one session must agree on: the
// record layout and tick rate the stream assumes, the seed and session counter
// every RNG stream derives from, and the config and corpus the simulation reads.
// Terminal geometry is deliberately absent — it is per-instance, and only a replay,
// which reconstructs the recording terminal, compares it.
// Shared by VerifyAnchor and the join handshake so the two cannot disagree.
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
	Records  int         // records offered
	Injected int         // records pushed into the queue
	Groups   int         // settle groups executed
	End      event.Stamp // position the App reached, which a trailing reset advances past the last record's own stamp
}

// ReplayDriver injects a record stream into a caller-driven App, advancing the clock
// as it goes. Resumable: Step consumes one tick, so a presenting loop paces playback
// and a harness runs the stream out. The record slice is the driver's for its lifetime;
// each group is sorted in place by queue slot, which is dispatch order.
type ReplayDriver struct {
	a       *App
	records []event.JournalRecord
	next    int      // index of the next record to inject
	cur     groupKey // position reached
	stats   ReplayStats
}

// NewReplayDriver binds a record stream to an App
func NewReplayDriver(a *App, records []event.JournalRecord) (*ReplayDriver, error) {
	if !a.cfg.Mode.Driven() {
		return nil, errors.New("replay: requires a caller-driven App")
	}
	if a.cfg.Journal {
		return nil, errors.New("replay: journaling a replay records a run that never happened")
	}
	return &ReplayDriver{a: a, records: records, stats: ReplayStats{Records: len(records)}}, nil
}

// Done reports whether every record has been injected
func (d *ReplayDriver) Done() bool { return d.next >= len(d.records) }

// Stats reports what has been consumed so far, with the App's live position
func (d *ReplayDriver) Stats() ReplayStats {
	st := d.stats
	st.End = d.a.Position()
	return st
}

// End returns the position of the last record, for a progress readout
func (d *ReplayDriver) End() event.Stamp {
	if len(d.records) == 0 {
		return event.Stamp{}
	}
	r := d.records[len(d.records)-1]
	return event.Stamp{Run: r.Run, Tick: r.Tick, Boundary: r.Boundary}
}

// Step advances one tick and applies every settle group the journal stamped on it.
// Ticking toward a distant record counts as a step, so a presenting loop calls this
// once per displayed tick. Returns false once the stream is exhausted.
func (d *ReplayDriver) Step() (bool, error) {
	if d.Done() {
		return false, nil
	}
	k := keyOf(d.records[d.next])
	if k.before(d.cur) {
		return false, fmt.Errorf("replay: jseq %d stamped run %d tick %d boundary %d, out of order",
			d.records[d.next].JSeq, k.run, k.tick, k.boundary)
	}

	// A run boundary is opened by the reset the previous run's records requested, so
	// the driver follows the queue's generation rather than driving it
	if k.run != d.cur.run {
		if got := d.a.runIndex(); got != k.run {
			return false, fmt.Errorf("replay: jseq %d opens run %d, the replay is in run %d",
				d.records[d.next].JSeq, k.run, got)
		}
		d.cur = groupKey{run: k.run}
	}

	if k.tick > d.cur.tick {
		d.a.Tick(1)
		d.cur.tick++
		if k.tick > d.cur.tick {
			return true, nil // still ticking toward the record
		}
	}

	// Every group stamped on this tick settles before the next one runs
	for !d.Done() {
		k = keyOf(d.records[d.next])
		if k.run != d.cur.run || k.tick != d.cur.tick {
			break
		}
		if err := d.injectGroup(k); err != nil {
			return false, err
		}
	}
	return true, nil
}

// RunAll consumes the whole stream
func (d *ReplayDriver) RunAll() error {
	for {
		more, err := d.Step()
		if err != nil || !more {
			return err
		}
	}
}

// injectGroup pushes one settle group's records in queue-slot order and settles them
func (d *ReplayDriver) injectGroup(k groupKey) error {
	j := d.next
	for j < len(d.records) && keyOf(d.records[j]) == k {
		j++
	}
	group := d.records[d.next:j]
	sort.SliceStable(group, func(x, y int) bool { return group[x].Seq < group[y].Seq })

	pushed := 0
	for i := range group {
		rec := &group[i]
		if err := checkRecord(rec); err != nil {
			return fmt.Errorf("replay: jseq %d: %w", rec.JSeq, err)
		}
		payload, err := decodeRecordPayload(rec)
		if err != nil {
			return fmt.Errorf("replay: jseq %d %s: %w", rec.JSeq, event.GetEventName(rec.Type), err)
		}
		d.a.world.PushRecord(rec.Type, payload, rec.Origin, rec.Domain)
		pushed++
	}
	if pushed > 0 {
		d.a.Settle()
	}

	d.stats.Injected += pushed
	d.stats.Groups++
	d.cur, d.next = k, j
	return nil
}

// Replay consumes an entire record stream. The caller runs any trailing ticks the
// last record misses.
func (a *App) Replay(records []event.JournalRecord) (ReplayStats, error) {
	d, err := NewReplayDriver(a, records)
	if err != nil {
		return ReplayStats{Records: len(records)}, err
	}
	err = d.RunAll()
	return d.Stats(), err
}

// runIndex returns the reset generation the replay's event queue is stamping
func (a *App) runIndex() uint64 { return a.world.Resources.Event.Queue.Stamp().Run }

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
