package event

import (
	"sync/atomic"

	"github.com/lixenwraith/toml"
)

// JournalSchema is the record layout version; bump on any field change
const JournalSchema = 1

// JournalRecord is one replayable event, produced synchronously at push time.
// Payload is TOML text that decodes into the registry prototype for Type.
// Run and tick are absent: the sink stamps them from its own correlation source.
type JournalRecord struct {
	Payload   string
	EncodeErr string // non-empty when Payload could not be produced
	JSeq      uint64 // dense record counter; a gap is exactly one lost record
	Seq       uint64 // queue slot, legitimately sparse in the journal
	Type      EventType
	Origin    Origin
}

// JournalAnchor is a self-describing header re-emitted periodically so a
// rotated log file can be replayed without its predecessors.
type JournalAnchor struct {
	Speed        string // time scale ladder token; exact, unlike a float
	ConfigID     string
	Schema       uint32
	Seed         uint64
	Session      uint64 // RNG session counter; streams derive from it, so replay must match
	JSeq         uint64
	TickInterval int64 // nanoseconds
}

// JournalSink consumes journal output. Called on the producing goroutine
// between the queue slot claim and its publish: it must not block, must not
// retain the record, and must not push events.
type JournalSink interface {
	Record(JournalRecord)
	Anchor(JournalAnchor)
}

// Journal captures non-system events for replay. One per queue, so concurrent
// harness runs in a single process cannot share a counter.
type Journal struct {
	sink    JournalSink // immutable after NewJournal
	seq     atomic.Uint64
	encFail atomic.Uint64
}

// NewJournal binds a sink; a nil sink yields a nil Journal, which disables capture
func NewJournal(sink JournalSink) *Journal {
	if sink == nil {
		return nil
	}
	return &Journal{sink: sink}
}

// Stats returns the emitted record count and the encode failure count
func (j *Journal) Stats() (emitted, encodeFailed uint64) {
	if j == nil {
		return 0, 0
	}
	return j.seq.Load(), j.encFail.Load()
}

// Anchor emits a header record; the caller owns the cadence
func (j *Journal) Anchor(a JournalAnchor) {
	if j == nil {
		return
	}
	a.Schema = JournalSchema
	a.JSeq = j.seq.Load()
	j.sink.Anchor(a)
}

// record emits one event against the producer's own copy, before the queue slot
// is published; after that a handler may recycle a pooled payload.
func (j *Journal) record(ev *GameEvent) {
	payload, encErr := encodePayload(ev.Type, ev.Payload)
	if encErr != "" {
		j.encFail.Add(1)
	}
	j.sink.Record(JournalRecord{
		Payload:   payload,
		EncodeErr: encErr,
		JSeq:      j.seq.Add(1),
		Seq:       ev.Seq,
		Type:      ev.Type,
		Origin:    ev.Origin,
	})
}

// encodePayload marshals a payload to TOML text. Only registry-shaped payloads
// round-trip, so pooled and scalar ones are reported rather than guessed at.
func encodePayload(et EventType, p any) (text, encErr string) {
	if p == nil {
		return "", ""
	}
	if !HasPayload(et) {
		return "", "no registry prototype"
	}
	b, err := toml.Marshal(p)
	if err != nil {
		return "", err.Error()
	}
	return string(b), ""
}
