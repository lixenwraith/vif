// Package event is the simulation's message layer: the EventType catalog and its
// generated registry, replication classes and producer origins, the MPSC queue and
// handler router, pooled payloads, the replay journal schema, and the wire codec.
// Replicated says whether every instance's journal must agree on a record; OnWire,
// narrower, whether a peer must receive it: a re-derived Shared event never does.
package event

import (
	"encoding/json"
	"fmt"

	"github.com/lixenwraith/toml"
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

// WireFrame is one artifact on the wire. The payload is the same TOML text the
// journal writes, so one encoder serves replay and transport.
type WireFrame struct {
	Event   string `json:"ev"`
	Domain  string `json:"domain"`
	Payload string `json:"payload"`
	Seq     uint64 `json:"seq"`
}

// ScheduledWireFrame names the simulation tick at whose opening an artifact applies.
type ScheduledWireFrame struct {
	Frame     WireFrame `json:"frame"`
	ApplyTick uint64    `json:"apply_tick"`
}

// WireBatch closes one participant's production epoch, even an empty one. Source is
// every receiver's ordering key and, with ProducedTick, names the epoch, so a copy
// arriving by a second mesh path is recognised. Run names the run the ticks count
// in: a receiver in another run has no tick for the frames and refuses the batch.
// Hops counts links since the copy's origin (its producer, or the authority that
// committed it) and bounds relay loops. Committed marks the authority's copy,
// carrying the apply ticks it chose.
type WireBatch struct {
	Frames       []ScheduledWireFrame `json:"frames,omitempty"`
	Run          uint64               `json:"run"`
	ProducedTick uint64               `json:"produced_tick"`
	Source       uint32               `json:"source"`
	Hops         uint8                `json:"hops,omitempty"`
	Committed    bool                 `json:"committed,omitempty"`

	// Age is how many of the authority's ticks after its production an epoch was
	// committed, on the committed copy: the up path and the clock offset its
	// producer's lead has to cover, measured where it matters.
	Age int64 `json:"age,omitempty"`
}

// NewWireFrame encodes one crossing; an unencodable payload reports why rather
// than travelling as a silently empty frame.
func NewWireFrame(ev GameEvent) (WireFrame, string) {
	payload, encErr := encodePayload(ev.Type, ev.Payload)
	if encErr != "" {
		return WireFrame{}, encErr
	}
	return WireFrame{
		Event:   GetEventName(ev.Type),
		Domain:  core.DomainNames[ev.Domain],
		Payload: payload,
		Seq:     ev.Seq,
	}, ""
}

// Decode resolves a frame back into the event a peer pushed. The registry
// prototype decides the payload type, so an unknown name is an error rather than
// a nil payload the consumer would have to guess at.
func (f WireFrame) Decode() (EventType, any, core.Domain, error) {
	et, ok := GetEventType(f.Event)
	if !ok {
		return EventNone, nil, core.DomainShared, fmt.Errorf("wire: unknown event %q", f.Event)
	}
	domain, ok := core.ParseDomain(f.Domain)
	if !ok {
		return EventNone, nil, core.DomainShared, fmt.Errorf("wire: unknown domain %q", f.Domain)
	}
	payload, err := decodeFramePayload(et, f.Payload)
	if err != nil {
		return EventNone, nil, core.DomainShared, err
	}
	return et, payload, domain, nil
}

// decodeFramePayload allocates a fresh payload from the registry prototype and
// decodes the frame text into it. Empty text means the producer pushed nil.
func decodeFramePayload(et EventType, text string) (any, error) {
	if text == "" {
		return nil, nil
	}
	p := NewPayloadStruct(et)
	if p == nil {
		return nil, fmt.Errorf("wire: %s carries payload text with no registry prototype", GetEventName(et))
	}
	if err := toml.Unmarshal([]byte(text), p); err != nil {
		return nil, err
	}
	return p, nil
}

// EncodeWireBatch serializes one closed barrier production epoch.
func EncodeWireBatch(batch WireBatch) ([]byte, error) { return json.Marshal(batch) }

// DecodeWireBatch decodes one barrier production epoch.
func DecodeWireBatch(b []byte) (WireBatch, error) {
	var out WireBatch
	err := json.Unmarshal(b, &out)
	return out, err
}
