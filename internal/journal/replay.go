package journal

import (
	"errors"
	"fmt"
	"sort"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vi-fighter/internal/event"
)

// ReplayTarget is the runtime surface needed to reproduce a journal. App owns
// world construction and policy; the journal package owns record ordering.
type ReplayTarget interface {
	Position() event.Stamp
	Tick(int)
	Settle()
	PushRecord(event.JournalRecord, any)
}

// ReplayStats reports what a replay consumed.
type ReplayStats struct {
	Records  int
	Injected int
	Groups   int
	End      event.Stamp
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

// ReplayDriver injects a record stream into a caller-driven target. Step consumes
// one tick so a presenting loop can pace it and a harness can run it flat out.
type ReplayDriver struct {
	target  ReplayTarget
	records []event.JournalRecord
	next    int
	cur     groupKey
	stats   ReplayStats
}

// NewReplayDriver binds a record stream to a target. The record slice belongs to
// the driver for its lifetime; each settle group is sorted in place by queue slot.
func NewReplayDriver(target ReplayTarget, records []event.JournalRecord) *ReplayDriver {
	return &ReplayDriver{target: target, records: records, stats: ReplayStats{Records: len(records)}}
}

// Done reports whether every record has been injected.
func (d *ReplayDriver) Done() bool { return d.next >= len(d.records) }

// Stats reports what has been consumed so far, with the target's live position.
func (d *ReplayDriver) Stats() ReplayStats {
	st := d.stats
	st.End = d.target.Position()
	return st
}

// End returns the position of the final record.
func (d *ReplayDriver) End() event.Stamp {
	if len(d.records) == 0 {
		return event.Stamp{}
	}
	r := d.records[len(d.records)-1]
	return event.Stamp{Run: r.Run, Tick: r.Tick, Boundary: r.Boundary}
}

// Step advances one tick and applies every settle group stamped on it.
func (d *ReplayDriver) Step() (bool, error) {
	if d.Done() {
		return false, nil
	}
	k := keyOf(d.records[d.next])
	if k.before(d.cur) {
		return false, fmt.Errorf("replay: jseq %d stamped run %d tick %d boundary %d, out of order",
			d.records[d.next].JSeq, k.run, k.tick, k.boundary)
	}

	if k.run != d.cur.run {
		if got := d.target.Position().Run; got != k.run {
			return false, fmt.Errorf("replay: jseq %d opens run %d, the replay is in run %d",
				d.records[d.next].JSeq, k.run, got)
		}
		d.cur = groupKey{run: k.run}
	}

	if k.tick > d.cur.tick {
		d.target.Tick(1)
		d.cur.tick++
		if k.tick > d.cur.tick {
			return true, nil
		}
	}

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

// RunAll consumes the whole stream.
func (d *ReplayDriver) RunAll() error {
	for {
		more, err := d.Step()
		if err != nil || !more {
			return err
		}
	}
}

func (d *ReplayDriver) injectGroup(k groupKey) error {
	j := d.next
	for j < len(d.records) && keyOf(d.records[j]) == k {
		j++
	}
	group := d.records[d.next:j]
	sort.SliceStable(group, func(x, y int) bool { return group[x].Seq < group[y].Seq })

	for i := range group {
		rec := &group[i]
		if err := checkRecord(rec); err != nil {
			return fmt.Errorf("replay: jseq %d: %w", rec.JSeq, err)
		}
		payload, err := DecodePayload(rec.Type, rec.Payload)
		if err != nil {
			return fmt.Errorf("replay: jseq %d %s: %w", rec.JSeq, event.GetEventName(rec.Type), err)
		}
		d.target.PushRecord(*rec, payload)
	}
	if len(group) > 0 {
		d.target.Settle()
	}

	d.stats.Injected += len(group)
	d.stats.Groups++
	d.cur, d.next = k, j
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
	if !rec.Origin.Journaled() {
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
