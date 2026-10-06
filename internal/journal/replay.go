package journal

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/event"
)

// ReplayTarget is the runtime surface needed to reproduce a journal. App owns
// world construction and policy; the journal package owns record ordering.
type ReplayTarget interface {
	Position() event.Stamp
	Tick(int)
	Settle()
	PushRecord(event.JournalRecord, any) bool // false: applied, nothing queued
	Install(event.JournalCapture) error
	Digest() event.JournalDigest // the world now; the driver stamps nothing
}

// ReplayStats reports what a replay consumed. Digests counts the written digests
// it reproduced; Diverged is the first it did not, after which none is compared.
type ReplayStats struct {
	Records   int
	Injected  int
	Groups    int
	Installed int
	Digests   int
	Diverged  *Divergence
	End       event.Stamp
}

// Divergence is the first written digest a replay did not reproduce: it left the
// recorded run after Since, the last digest that matched, and by At.
type Divergence struct {
	At      event.Stamp
	Since   *event.Stamp
	Classes []string // the state classes that differ, in digest order
}

func (v *Divergence) Error() string {
	msg := fmt.Sprintf("replay left its run by run %d tick %d (%s)", v.At.Run, v.At.Tick, strings.Join(v.Classes, ", "))
	if v.Since != nil {
		msg += fmt.Sprintf("; it matched at run %d tick %d", v.Since.Run, v.Since.Tick)
	}
	return msg
}

type groupKey struct{ run, tick, boundary uint64 }

func keyOf(r event.JournalRecord) groupKey { return groupKey{r.Run, r.Tick, r.Boundary} }

// SameReplayGroup reports whether two records were produced in one settle group.
func SameReplayGroup(a, b event.JournalRecord) bool { return keyOf(a) == keyOf(b) }

func (k groupKey) before(o groupKey) bool {
	if k.run != o.run {
		return k.run < o.run
	}
	if k.tick != o.tick {
		return k.tick < o.tick
	}
	return k.boundary < o.boundary
}

// Stream is a journal ordered for replay, which every driver over it reads and
// none writes. Each settle group's records are split where a world was written
// among them, and each part is in queue slot order, the order the run dispatched.
type Stream struct {
	records  []event.JournalRecord
	captures []event.JournalCapture
	digests  []event.JournalDigest
	end      *event.Stamp // input-free trailing ticks; older journals end at their last record
}

// Stream orders the set for replay. A driver over it compares the set's digests
// and, when the set has an end, plays on to it.
func (s Set) Stream() *Stream {
	recs, caps := slices.Clone(s.Records), slices.Clone(s.Captures)
	// A Capture sink holds records as producers appended them, which jseq can invert.
	slices.SortStableFunc(recs, func(a, b event.JournalRecord) int { return cmp.Compare(a.JSeq, b.JSeq) })
	slices.SortStableFunc(caps, func(a, b event.JournalCapture) int { return cmp.Compare(a.JSeq, b.JSeq) })
	for i, c := 0, 0; i < len(recs); {
		for c < len(caps) && caps[c].JSeq < recs[i].JSeq {
			c++
		}
		j := segmentEnd(recs, i, caps, c)
		slices.SortStableFunc(recs[i:j], func(a, b event.JournalRecord) int { return cmp.Compare(a.Seq, b.Seq) })
		i = j
	}
	st := &Stream{records: recs, captures: caps, digests: slices.Clone(s.Digests)}
	if s.End != nil {
		end := *s.End
		st.end = &end
	}
	return st
}

// segmentEnd is where the part of a settle group opening at i ends: at a record of
// another group, or one emitted after the next written world, which landed on it.
// It reads no order inside the part, so it finds the same end once that is sorted.
func segmentEnd(recs []event.JournalRecord, i int, caps []event.JournalCapture, c int) int {
	k, j := keyOf(recs[i]), i+1
	for j < len(recs) && keyOf(recs[j]) == k && (c >= len(caps) || recs[j].JSeq <= caps[c].JSeq) {
		j++
	}
	return j
}

// ReplayDriver injects a record stream into a caller-driven target. Step consumes
// one tick so a presenting loop can pace it and a harness can run it flat out.
type ReplayDriver struct {
	target  ReplayTarget
	s       *Stream
	next    int
	nextCap int
	cur     groupKey
	landed  bool // an install moved the clock past records stamped before it
	stats   ReplayStats
	nextDig int
	matched *event.Stamp
}

// NewReplayDriver binds a stream to a target.
func NewReplayDriver(target ReplayTarget, s *Stream) *ReplayDriver {
	return &ReplayDriver{target: target, s: s, stats: ReplayStats{Records: len(s.records)}}
}

// Cursor is where a driver stands in its stream. Another driver over the same
// stream resumes from it, as a replay restored from a checkpoint does.
type Cursor struct {
	next, nextCap, nextDig int
	cur                    groupKey
	landed                 bool
	stats                  ReplayStats
	matched                *event.Stamp
}

// Cursor reads where the driver stands.
func (d *ReplayDriver) Cursor() Cursor {
	return Cursor{next: d.next, nextCap: d.nextCap, nextDig: d.nextDig, cur: d.cur,
		landed: d.landed, stats: d.stats, matched: d.matched}
}

// Resume places the driver where c was read; its target must stand where c's did.
func (d *ReplayDriver) Resume(c Cursor) {
	d.next, d.nextCap, d.nextDig, d.cur = c.next, c.nextCap, c.nextDig, c.cur
	d.landed, d.stats, d.matched = c.landed, c.stats, c.matched
}

// Done reports whether every record has been injected and every world installed.
func (d *ReplayDriver) Done() bool {
	end := d.s.end
	return d.streamDone() && (end == nil || d.target.Position().Run == end.Run && d.target.Position().Tick >= end.Tick)
}

// tick advances the target one tick and compares the digest written after it. The
// digests are in tick order; one the replay never stands on is skipped.
func (d *ReplayDriver) tick() {
	d.target.Tick(1)
	at := d.target.Position()
	for ; d.nextDig < len(d.s.digests) && d.stats.Diverged == nil; d.nextDig++ {
		w := d.s.digests[d.nextDig]
		if w.Run > at.Run || w.Run == at.Run && w.Tick > at.Tick {
			return
		}
		if w.Run != at.Run || w.Tick != at.Tick {
			continue
		}
		got := d.target.Digest()
		var classes []string
		for _, c := range []struct {
			name      string
			want, got uint64
		}{{"positions", w.Positions, got.Positions}, {"kinetics", w.Kinetics, got.Kinetics},
			{"combat", w.Combat, got.Combat}, {"entities", w.Entities, got.Entities}} {
			if c.want != c.got {
				classes = append(classes, c.name)
			}
		}
		if len(classes) > 0 {
			d.stats.Diverged = &Divergence{At: event.Stamp{Run: at.Run, Tick: at.Tick}, Since: d.matched, Classes: classes}
			return
		}
		d.stats.Digests++
		d.matched = &event.Stamp{Run: at.Run, Tick: at.Tick}
	}
}

func (d *ReplayDriver) streamDone() bool {
	return d.next >= len(d.s.records) && d.nextCap >= len(d.s.captures)
}

// dueCapture reports whether the next thing in the stream is a written world: one
// whose place is before the next record, or any left after the last. Every record
// of the part opening at next lies on one side of each world, so any one answers.
func (d *ReplayDriver) dueCapture() bool {
	return d.nextCap < len(d.s.captures) &&
		(d.next >= len(d.s.records) || d.s.captures[d.nextCap].JSeq < d.s.records[d.next].JSeq)
}

// install ticks to where the recorded run stood when it wrote the next world, then
// writes it; the install moves the clock, so the position is read back.
func (d *ReplayDriver) install() (bool, error) {
	c := d.s.captures[d.nextCap]
	at := d.target.Position()
	if at.Run != c.Run || at.Tick > c.Tick {
		return false, fmt.Errorf("replay: capture after jseq %d was written at run %d tick %d, the replay is at run %d tick %d",
			c.JSeq, c.Run, c.Tick, at.Run, at.Tick)
	}
	if at.Tick < c.Tick {
		d.tick()
		return true, nil
	}
	d.settleTo(c.Boundary)
	if err := d.target.Install(c); err != nil {
		return false, fmt.Errorf("replay: capture after jseq %d: %w", c.JSeq, err)
	}
	p := d.target.Position()
	d.cur = groupKey{run: p.Run, tick: p.Tick, boundary: p.Boundary}
	d.landed = true
	d.nextCap++
	d.stats.Installed++
	return true, nil
}

// Stats reports what has been consumed so far, with the target's live position.
func (d *ReplayDriver) Stats() ReplayStats {
	st := d.stats
	st.End = d.target.Position()
	return st
}

// End includes input-free trailing ticks when the recorder supplied its end.
func (d *ReplayDriver) End() event.Stamp {
	if d.s.end != nil {
		return *d.s.end
	}
	if len(d.s.records) == 0 {
		return event.Stamp{}
	}
	r := d.s.records[len(d.s.records)-1]
	return event.Stamp{Run: r.Run, Tick: r.Tick, Boundary: r.Boundary}
}

// Step advances one tick and applies every settle group stamped on it.
func (d *ReplayDriver) Step() (bool, error) {
	if d.streamDone() {
		end := d.s.end
		if end == nil {
			return false, nil
		}
		at := d.target.Position()
		if at.Run != end.Run || at.Tick > end.Tick {
			return false, fmt.Errorf("replay: position %v exceeds recorded end %v", at, *end)
		}
		if at.Tick == end.Tick {
			return false, nil
		}
		d.tick()
		return true, nil
	}
	if d.dueCapture() {
		return d.install()
	}
	k := keyOf(d.s.records[d.next])
	if k.before(d.cur) {
		if !d.landed {
			return false, fmt.Errorf("replay: jseq %d stamped run %d tick %d boundary %d, out of order",
				d.s.records[d.next].JSeq, k.run, k.tick, k.boundary)
		}
		// Pushed just before a write that moved the clock: it lands on that world.
		return true, d.injectGroup(k)
	}
	d.landed = false

	if k.run != d.cur.run {
		if got := d.target.Position().Run; got != k.run {
			return false, fmt.Errorf("replay: jseq %d opens run %d, the replay is in run %d",
				d.s.records[d.next].JSeq, k.run, got)
		}
		d.cur = groupKey{run: k.run}
	}

	if k.tick > d.cur.tick {
		d.tick()
		d.cur.tick++
		if k.tick > d.cur.tick {
			return true, nil
		}
	}

	for d.next < len(d.s.records) && !d.dueCapture() {
		k = keyOf(d.s.records[d.next])
		if k.run != d.cur.run || k.tick != d.cur.tick {
			break
		}
		if err := d.injectGroup(k); err != nil {
			return false, err
		}
	}
	return true, nil
}

// RunAll consumes the whole stream.
func (d *ReplayDriver) RunAll() error {
	for {
		more, err := d.Step()
		if err != nil || !more {
			return err
		}
	}
}

// settleTo settles once when the recorded run had settled past where the replay
// stands: what it held then, a fresh run's boot or a tick's leftovers among it,
// was no record, and only the boundary says the settle happened.
func (d *ReplayDriver) settleTo(boundary uint64) {
	if d.target.Position().Boundary < boundary {
		d.target.Settle()
	}
}

func (d *ReplayDriver) injectGroup(k groupKey) error {
	if p := d.target.Position(); p.Run == k.run && p.Tick == k.tick {
		d.settleTo(k.boundary)
	}
	j := segmentEnd(d.s.records, d.next, d.s.captures, d.nextCap)
	group := d.s.records[d.next:j]

	queued := false
	for i := range group {
		rec := &group[i]
		if err := checkRecord(rec); err != nil {
			return fmt.Errorf("replay: jseq %d: %w", rec.JSeq, err)
		}
		payload, err := DecodePayload(rec.Type, rec.Payload)
		if err != nil {
			return fmt.Errorf("replay: jseq %d %s: %w", rec.JSeq, event.GetEventName(rec.Type), err)
		}
		queued = d.target.PushRecord(*rec, payload) || queued
	}
	// The recorded run settles only what it pushed; a settle here would dispatch
	// events it held until its next tick.
	if queued {
		d.target.Settle()
	}

	d.stats.Injected += len(group)
	d.stats.Groups++
	d.next = j
	if !k.before(d.cur) {
		d.cur = k
	}
	return nil
}

func checkRecord(rec *event.JournalRecord) error {
	if rec.EncodeErr != "" {
		return fmt.Errorf("encode error %q: the payload was never captured", rec.EncodeErr)
	}
	if rec.Type <= event.EventNone || int(rec.Type) >= event.EventTypeCount ||
		event.GetEventName(rec.Type) == "" {
		return fmt.Errorf("unregistered event type %d", rec.Type)
	}
	if !rec.Origin.Journaled() && rec.Crossing == 0 {
		return fmt.Errorf("origin %s is not a journaled producer", rec.Origin)
	}
	return nil
}

// DecodePayload allocates a typed event payload and decodes journal-compatible
// TOML into it. Empty text represents a nil payload.
func DecodePayload(et event.EventType, text string) (any, error) {
	if text == "" {
		return nil, nil
	}
	p := event.NewPayloadStruct(et)
	if p == nil {
		return nil, errors.New("payload text with no registry prototype")
	}
	if err := toml.Unmarshal([]byte(text), p); err != nil {
		return nil, err
	}
	return p, nil
}
