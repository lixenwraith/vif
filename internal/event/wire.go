// Package event is the simulation's message layer: the EventType catalog and its
// generated registry, replication classes and producer origins, the MPSC queue and
// handler router, pooled payloads, the replay journal schema, and the wire codec.
// Replicated says whether every instance's journal must agree on a record; OnWire,
// narrower, whether a peer must receive it: a re-derived Shared event never does.
package event

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/lixenwraith/vif/internal/core"
)

// stampedCrossings names the Stamped types a player-domain producer pushes at a
// shared target. Every other Stamped type resolving shared came from a shared
// system reading shared state, so the receiver already produced it.
// Combat is the whole list: drain and fuse stamp player, and every other producer
// carries a shared profile.
var stampedCrossings = map[EventType]bool{
	EventCombatAttackDirectRequest: true,
}

// Derived is implemented by a payload the receiver reconstructs from the artifact
// that crossed, so its own record stays local (D-5). A chain follow-up is the case:
// the root hit crosses and the receiver chains from it.
type Derived interface {
	IsDerived() bool
}

// Crossed is implemented by a payload whose shared outcome needs a value no
// receiver can re-derive from the fields the producer wrote down. See
// StampCrossing and CrossingID.
type Crossed interface {
	StampCrossing(source uint32, seq uint64)
}

// StampCrossing names the artifact on its payload before the frame is encoded, so
// every copy carries one identity. A crossing that misses its lead lands later on
// the authority than on its producer, so a value drawn from a shared stream at apply
// time would go to a different artifact on each — a knockback in another direction.
// It is the part of the D-3 shared outcome a producer cannot write down.
func StampCrossing(payload any, source uint32, seq uint64) {
	if c, ok := payload.(Crossed); ok {
		c.StampCrossing(source, seq)
	}
}

// Timed is implemented by a payload that names the tick its producer made it at,
// which no receiver can check. See BoundTick.
type Timed interface {
	BoundTick(applyTick uint64)
}

// BoundTick holds a crossing's own tick to what the tick it applies at allows, so a
// peer can't claim an input older than its crossing could be. Every instance applies
// a crossing at one agreed tick, so every instance bounds it alike.
func BoundTick(payload any, applyTick uint64) {
	if t, ok := payload.(Timed); ok {
		t.BoundTick(applyTick)
	}
}

// OnWire reports whether a dispatched event must reach the other participants.
// An event a peer produced is never echoed: it already reached everyone.
// By value: Push must stay allocation-free, and a pointer here escapes it.
func OnWire(ev GameEvent) bool {
	if ev.Origin == OriginNetwork {
		return false
	}
	switch ClassOf(ev.Type) {
	case ClassBus:
		// A Bus artifact travels only when its producer explicitly stamps it as a
		// crossing; types with a shared producer leave that re-derived copy local.
		if ev.Domain != core.DomainPlayer {
			return false
		}
	case ClassStamped:
		// Here the tag is the target's domain, not the producer's (D-10), so the
		// crossing is the shared one and the table names which types can be it.
		if ev.Domain != core.DomainShared || !stampedCrossings[ev.Type] {
			return false
		}
	default:
		return false
	}
	d, ok := ev.Payload.(Derived)
	return !ok || !d.IsDerived()
}

// WireSink defers crossings into the barrier at this participant's playout lead.
// Cross reports whether it took the event, which the queue then neither journals
// nor publishes; the barrier publishes the local copy at its agreed tick. Receive
// reports whether a session is live. Cross is non-blocking and may run from any
// producer; Receive and Flush run under the world lock.
type WireSink interface {
	Receive(nextTick uint64) (live bool)
	Cross(ev GameEvent) (taken bool)
	CrossingApplied(sequence uint64)
	Flush(completedTick uint64)
}

// WireFrame is one artifact on the wire: its event as the registry's index, the
// domain its producer stamped, the payload in the generated codec, which the
// journal writes too, and its source-local sequence.
type WireFrame struct {
	Event   EventType
	Domain  core.Domain
	Payload []byte
	Seq     uint64
}

// ScheduledWireFrame names the simulation tick at whose opening an artifact applies.
type ScheduledWireFrame struct {
	Frame     WireFrame
	ApplyTick uint64
}

// WireBatch closes one participant's production epoch, even an empty one. Source
// and ProducedTick name the epoch, so a copy arriving by a second mesh path is
// recognised; a receiver in another Run has no tick for the frames and refuses it.
// Hops counts links since the copy's origin, its producer or the authority that
// committed it, and bounds relay loops; Committed marks the authority's copy.
type WireBatch struct {
	Frames       []ScheduledWireFrame
	Run          uint64
	ProducedTick uint64
	Source       uint32
	Hops         uint8
	Committed    bool

	// Age is how many of the authority's ticks after its production an epoch was
	// committed, on the committed copy: the up path and the clock offset its
	// producer's lead has to cover, measured where it matters.
	Age int64

	// Leaf marks a producer with nobody behind it, which reads its committed copy
	// for its age alone: the copy returns to it without the frames it holds.
	Leaf bool
}

// NewWireFrame encodes one crossing; an unencodable payload reports why rather
// than travelling as a silently empty frame.
func NewWireFrame(ev GameEvent) (WireFrame, string) {
	payload, encErr := encodePayload(ev.Type, ev.Payload)
	if encErr != "" {
		return WireFrame{}, encErr
	}
	return WireFrame{Event: ev.Type, Domain: ev.Domain, Payload: payload, Seq: ev.Seq}, ""
}

// Decode is DecodePayload for the frame's event.
func (f WireFrame) Decode() (any, error) { return DecodePayload(f.Event, f.Payload) }

// payloadCodec is the generated codec every registered payload carries: a
// presence mask, then each present field in declaration order (payload_gen.go).
type payloadCodec interface {
	appendPayload(b []byte) []byte
	readPayload(r *wireReader)
}

// DecodePayload allocates a fresh payload from the registry prototype and reads
// the bytes into it. No bytes means the producer pushed nil; bytes the codec does
// not consume are a payload of another build.
func DecodePayload(et EventType, b []byte) (any, error) {
	if len(b) == 0 {
		return nil, nil
	}
	p := NewPayloadStruct(et)
	if p == nil {
		return nil, fmt.Errorf("wire: %s carries a payload with no registry prototype", GetEventName(et))
	}
	r := wireReader{b: b}
	p.(payloadCodec).readPayload(&r)
	if r.err != nil {
		return nil, fmt.Errorf("wire: %s payload: %w", GetEventName(et), r.err)
	}
	if len(r.b) != 0 {
		return nil, fmt.Errorf("wire: %s payload leaves %d bytes unread", GetEventName(et), len(r.b))
	}
	return p, nil
}

// The envelope. A batch is [flags][source][run][produced tick][hops][age][frame
// count], then each frame [event][flags][sequence][apply tick][payload length]
// [payload]: counts and ticks as uvarints, the age and the deltas as varints, a
// sequence relative to the frame before and an apply tick to the frame before or,
// for the first, to the produced tick, and the flag bytes below.
const (
	batchCommitted = 0x01
	batchLeaf      = 0x02
	frameDomain    = 0x03 // the producer's domain, in the frame flags' low bits

	// minFrameBytes bounds the frames a body can hold, so a count cannot allocate
	// more than the bytes behind it.
	minFrameBytes = 5
)

// EncodeWireBatch serializes one closed barrier production epoch.
func EncodeWireBatch(batch WireBatch) ([]byte, error) {
	size := 24
	for i := range batch.Frames {
		size += 16 + len(batch.Frames[i].Frame.Payload)
	}
	flags := byte(0)
	if batch.Committed {
		flags |= batchCommitted
	}
	if batch.Leaf {
		flags |= batchLeaf
	}
	b := append(make([]byte, 0, size), flags)
	b = binary.AppendUvarint(b, uint64(batch.Source))
	b = binary.AppendUvarint(b, batch.Run)
	b = binary.AppendUvarint(b, batch.ProducedTick)
	b = append(b, batch.Hops)
	b = binary.AppendVarint(b, batch.Age)
	b = binary.AppendUvarint(b, uint64(len(batch.Frames)))
	seq, tick := uint64(0), batch.ProducedTick
	for i := range batch.Frames {
		f := &batch.Frames[i]
		if f.Frame.Event <= EventNone || f.Frame.Event >= EventTypeCount {
			return nil, fmt.Errorf("wire: event %d is not in the registry", f.Frame.Event)
		}
		if f.Frame.Domain >= core.DomainCount {
			return nil, fmt.Errorf("wire: unknown domain %d", f.Frame.Domain)
		}
		b = binary.AppendUvarint(b, uint64(f.Frame.Event))
		b = append(b, byte(f.Frame.Domain))
		b = binary.AppendVarint(b, int64(f.Frame.Seq-seq))
		b = binary.AppendVarint(b, int64(f.ApplyTick-tick))
		b = binary.AppendUvarint(b, uint64(len(f.Frame.Payload)))
		b = append(b, f.Frame.Payload...)
		seq, tick = f.Frame.Seq, f.ApplyTick
	}
	return b, nil
}

// DecodeWireBatch decodes one barrier production epoch, refusing a body the
// encoder could not have written rather than reading past it.
func DecodeWireBatch(b []byte) (WireBatch, error) {
	r := wireReader{b: b}
	var out WireBatch
	flags := r.u8()
	out.Committed, out.Leaf = flags&batchCommitted != 0, flags&batchLeaf != 0
	out.Source = uint32(r.uvarint())
	out.Run = r.uvarint()
	out.ProducedTick = r.uvarint()
	out.Hops = r.u8()
	out.Age = r.varint()
	n := r.uvarint()
	if r.err != nil {
		return WireBatch{}, r.err
	}
	if n > uint64(len(r.b))/minFrameBytes {
		return WireBatch{}, fmt.Errorf("wire: %d frames claimed in %d bytes", n, len(r.b))
	}
	if n > 0 {
		out.Frames = make([]ScheduledWireFrame, 0, n)
	}
	seq, tick := uint64(0), out.ProducedTick
	for range n {
		var f ScheduledWireFrame
		et := r.uvarint()
		fl := r.u8()
		seq += uint64(r.varint())
		tick += uint64(r.varint())
		if payload := r.bytes(r.uvarint()); len(payload) != 0 {
			f.Frame.Payload = append([]byte(nil), payload...)
		}
		if r.err != nil {
			return WireBatch{}, r.err
		}
		if et == 0 || et >= EventTypeCount {
			return WireBatch{}, fmt.Errorf("wire: unknown event %d", et)
		}
		if fl&^frameDomain != 0 || core.Domain(fl&frameDomain) >= core.DomainCount {
			return WireBatch{}, fmt.Errorf("wire: unknown frame flags %#x", fl)
		}
		f.Frame.Event, f.Frame.Domain, f.Frame.Seq, f.ApplyTick = EventType(et), core.Domain(fl&frameDomain), seq, tick
		out.Frames = append(out.Frames, f)
	}
	return out, nil
}

// wireReader walks a body with one sticky error, so a decoder reads its fields
// straight and checks once.
type wireReader struct {
	b   []byte
	err error
}

func (r *wireReader) fail() {
	if r.err == nil {
		r.err = errors.New("wire: body truncated")
	}
}

func (r *wireReader) u8() byte {
	if len(r.b) == 0 {
		r.fail()
		return 0
	}
	v := r.b[0]
	r.b = r.b[1:]
	return v
}

func (r *wireReader) uvarint() uint64 {
	v, n := binary.Uvarint(r.b)
	if n <= 0 {
		r.fail()
		return 0
	}
	r.b = r.b[n:]
	return v
}

func (r *wireReader) varint() int64 {
	v, n := binary.Varint(r.b)
	if n <= 0 {
		r.fail()
		return 0
	}
	r.b = r.b[n:]
	return v
}

func (r *wireReader) u64() uint64 {
	if len(r.b) < 8 {
		r.fail()
		return 0
	}
	v := binary.LittleEndian.Uint64(r.b)
	r.b = r.b[8:]
	return v
}

// bytes aliases the body: a caller that keeps them copies them.
func (r *wireReader) bytes(n uint64) []byte {
	if n > uint64(len(r.b)) {
		r.fail()
		return nil
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}

// length reads a count the bytes behind it can hold, so a corrupt count cannot
// allocate past the body.
func (r *wireReader) length() int {
	n := r.uvarint()
	if n > uint64(len(r.b)) {
		r.fail()
		return 0
	}
	return int(n)
}
