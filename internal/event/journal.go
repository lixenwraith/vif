package event

import (
	"reflect"
	"sync/atomic"

	"github.com/lixenwraith/toml"
)

// JournalSchema is the record layout version; bump on any field change
const JournalSchema = 4

// Stamp locates a record in the run/tick/settle lattice. Run advances on game
// reset, tick on each simulation step, boundary on each completed settle group.
type Stamp struct {
	Run      uint64
	Tick     uint64
	Boundary uint64
}

// JournalRecord is one replayable event, produced synchronously at push time.
// Payload is TOML text that decodes into the registry prototype for Type.
type JournalRecord struct {
	Payload   string
	EncodeErr string // non-empty when Payload could not be produced
	JSeq      uint64 // dense record counter; a gap is exactly one lost record
	Seq       uint64 // queue slot, legitimately sparse in the journal
	Run       uint64 // reset generation; the tick counter restarts in each
	Tick      uint64 // completed ticks in this run
	Boundary  uint64 // completed between-tick settles this tick
	Type      EventType
	Origin    Origin
}

// JournalAnchor is a self-describing header re-emitted periodically so a
// rotated log file can be replayed without its predecessors.
type JournalAnchor struct {
	Speed      string // time scale ladder token; exact, unlike a float
	ConfigID   string
	ContentID  string
	ContentPin string // file the corpus is restricted to, empty when unpinned

	Schema  uint64 // uint64, not uint32: the log formatter stringifies narrow uints
	Seed    uint64
	Session uint64 // RNG session counter; streams derive from it, so replay must match
	JSeq    uint64

	// Position at emission, and the position the journal opened at. A non-zero
	// start marks a mid-run capture, which needs a world snapshot to replay.
	Run       uint64
	Tick      uint64
	StartRun  uint64
	StartTick uint64

	// Corpus fingerprint: a replay compares these against its own telemetry, since
	// a resolved path proves which corpus was asked for, not which one loaded
	ContentFiles  uint64
	ContentBlocks uint64
	ContentLines  uint64

	TickInterval int64 // nanoseconds
	Width        int   // terminal-equivalent dimensions the run started with
	Height       int
}

// JournalSink consumes journal output. Record is called on the producing
// goroutine between the queue slot claim and its publish: it must not block,
// must not retain the record, and must not push events. Anchor is called on the
// tick goroutine, so the two can overlap and an implementation must be safe for
// concurrent use.
type JournalSink interface {
	Record(JournalRecord)
	Anchor(JournalAnchor)
}

// AnchorIntervalTicks is the tick period between anchor records, so a rotated
// file carries one within this many ticks of its first record
const AnchorIntervalTicks = 600

// AnchorDue reports whether a tick falls on the anchor cadence
func AnchorDue(tick uint64) bool { return tick%AnchorIntervalTicks == 0 }

// Journal captures non-system events for replay. One per queue, so concurrent
// harness runs in a single process cannot share a counter.
type Journal struct {
	sink    JournalSink // immutable after NewJournal
	seq     atomic.Uint64
	encFail atomic.Uint64
	anchor  atomic.Pointer[JournalAnchor] // run-invariant template
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

// SetAnchor installs the run-invariant anchor fields and emits one immediately.
// start is the stamp the journal opened at; a non-zero one marks a mid-run capture.
func (j *Journal) SetAnchor(a JournalAnchor, start Stamp) {
	if j == nil {
		return
	}
	a.Schema = JournalSchema
	a.StartRun, a.StartTick = start.Run, start.Tick
	j.anchor.Store(&a)
	j.Anchor(start, a.Session, a.Speed, a.Width, a.Height)
}

// Anchor re-emits the template with the fields only the engine can supply.
// Position and dimensions are live: a file rotated after a resize or a reset must
// describe the geometry and generation its records were produced under.
func (j *Journal) Anchor(st Stamp, session uint64, speed string, width, height int) {
	if j == nil {
		return
	}
	p := j.anchor.Load()
	if p == nil {
		return
	}
	a := *p
	a.Run, a.Tick = st.Run, st.Tick
	a.Session = session
	a.Speed = speed
	a.Width = width
	a.Height = height
	a.JSeq = j.seq.Load()
	j.sink.Anchor(a)
}

// record emits one event against the producer's own copy, before the queue slot
// is published; after that a handler may recycle a pooled payload.
func (j *Journal) record(ev *GameEvent, st Stamp) {
	payload, encErr := encodePayload(ev.Type, ev.Payload)
	if encErr != "" {
		j.encFail.Add(1)
	}
	j.sink.Record(JournalRecord{
		Payload:   payload,
		EncodeErr: encErr,
		JSeq:      j.seq.Add(1),
		Seq:       ev.Seq,
		Run:       st.Run,
		Tick:      st.Tick,
		Boundary:  st.Boundary,
		Type:      ev.Type,
		Origin:    ev.Origin,
	})
}

// encodePayload marshals a payload to TOML text. Only the registry prototype's
// own type round-trips, so a mismatch is reported rather than silently encoded.
func encodePayload(et EventType, p any) (text, encErr string) {
	if p == nil {
		return "", ""
	}
	proto, ok := typeToPayload[et]
	if !ok {
		return "", "no registry prototype"
	}
	t := reflect.TypeOf(p)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t != proto {
		return "", "payload type " + t.String() + " is not the registered prototype"
	}
	b, err := toml.Marshal(p)
	if err != nil {
		return "", err.Error()
	}
	return string(b), ""
}
