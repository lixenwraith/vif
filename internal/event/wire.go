// Package event: the wire predicate and frame codec.
//
// Compared is not sent. Replicated answers "must both instances hold this
// record", which is what the journal filter needs; OnWire answers "must a peer
// receive it", which is narrower. A Shared event is re-derived identically on
// every instance (D-5) and must never travel, or it applies twice. What crosses
// is the D-3 artifact: the Bus class, plus the one Stamped type a player-domain
// producer aims at a shared target.
package event

import (
	"encoding/json"
	"fmt"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vi-fighter/internal/core"
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

// WireSink defers crossings into a fixed-delay barrier. Cross returns true only
// when it took ownership of the event; the queue then neither journals nor publishes
// the original. Receive admits due local and peer artifacts before the next tick,
// and Flush closes one production epoch. Cross is non-blocking and may run from any
// producer; Receive and Flush run under the world lock.
type WireSink interface {
	Receive(nextTick uint64) int
	Cross(ev GameEvent) bool
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

// WireBatch closes one participant's production epoch, including an empty one.
// Source provides the canonical ordering key shared by every receiver, and with
// ProducedTick it names the epoch uniquely — which is what lets a receiver
// recognise a copy that reached it by a second path through the mesh.
//
// Hops counts the links crossed so far. It bounds a relay loop; it is not what
// terminates flooding, which is the receiver's per-source epoch window, and it is
// deliberately not part of the artifact's identity.
type WireBatch struct {
	Frames       []ScheduledWireFrame `json:"frames,omitempty"`
	ProducedTick uint64               `json:"produced_tick"`
	Source       uint32               `json:"source"`
	Hops         uint8                `json:"hops,omitempty"`
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

// EncodeFrames packs one tick's crossings into a single message payload
func EncodeFrames(frames []WireFrame) ([]byte, error) { return json.Marshal(frames) }

// DecodeFrames unpacks a message payload back into frames
func DecodeFrames(b []byte) ([]WireFrame, error) {
	var out []WireFrame
	err := json.Unmarshal(b, &out)
	return out, err
}

// EncodeWireBatch serializes one closed barrier production epoch.
func EncodeWireBatch(batch WireBatch) ([]byte, error) { return json.Marshal(batch) }

// DecodeWireBatch decodes one barrier production epoch.
func DecodeWireBatch(b []byte) (WireBatch, error) {
	var out WireBatch
	err := json.Unmarshal(b, &out)
	return out, err
}

// LogRecord is one journal record on its way to a participant catching up. Event,
// origin and domain travel as names for the same reason the journal file writes
// names: two builds agree on a name where they need not agree on an enum value.
type LogRecord struct {
	Ev        string `json:"ev"`
	Origin    string `json:"origin"`
	Domain    string `json:"domain"`
	Payload   string `json:"payload,omitempty"`
	EncodeErr string `json:"encode_err,omitempty"`
	JSeq      uint64 `json:"jseq"`
	Seq       uint64 `json:"seq"`
	Run       uint64 `json:"run"`
	Tick      uint64 `json:"tick"`
	Boundary  uint64 `json:"boundary"`
}

// LogChunk is one bounded slice of a session log. A framed message carries at most
// 64 KiB, and a log is unbounded, so it crosses as a sequence with the last marked.
type LogChunk struct {
	Records []LogRecord `json:"records"`
	Seq     uint32      `json:"seq"`
	Total   uint32      `json:"total"`
	Final   bool        `json:"final,omitempty"`
}

// EncodeSessionLog splits a log into chunks no larger than maxBytes each. A single
// record that cannot fit is an error rather than a silently truncated stream: the
// replay is only exact if every record reaches it.
func EncodeSessionLog(records []JournalRecord, maxBytes int) ([][]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("session log: chunk budget %d is not usable", maxBytes)
	}
	var out [][]byte
	for i := 0; i < len(records) || len(out) == 0; {
		chunk := LogChunk{Seq: uint32(len(out))}
		n := i
		for n < len(records) {
			chunk.Records = append(chunk.Records, newLogRecord(records[n]))
			body, err := json.Marshal(chunk)
			if err != nil {
				return nil, err
			}
			if len(body) > maxBytes {
				if len(chunk.Records) == 1 {
					return nil, fmt.Errorf("session log: record %d encodes to %d bytes, over the %d-byte chunk budget",
						records[n].JSeq, len(body), maxBytes)
				}
				chunk.Records = chunk.Records[:len(chunk.Records)-1]
				break
			}
			n++
		}
		body, err := json.Marshal(chunk)
		if err != nil {
			return nil, err
		}
		out = append(out, body)
		i = n
	}
	// Total and Final are known only once the split is done, so the chunks are
	// re-encoded with them rather than the caller being asked to count.
	for seq := range out {
		var chunk LogChunk
		if err := json.Unmarshal(out[seq], &chunk); err != nil {
			return nil, err
		}
		chunk.Total, chunk.Final = uint32(len(out)), seq == len(out)-1
		body, err := json.Marshal(chunk)
		if err != nil {
			return nil, err
		}
		out[seq] = body
	}
	return out, nil
}

// DecodeSessionLogChunk decodes one chunk back into journal records.
func DecodeSessionLogChunk(body []byte) (LogChunk, []JournalRecord, error) {
	var chunk LogChunk
	if err := json.Unmarshal(body, &chunk); err != nil {
		return LogChunk{}, nil, err
	}
	out := make([]JournalRecord, 0, len(chunk.Records))
	for _, r := range chunk.Records {
		rec, err := r.record()
		if err != nil {
			return LogChunk{}, nil, err
		}
		out = append(out, rec)
	}
	return chunk, out, nil
}

func newLogRecord(r JournalRecord) LogRecord {
	return LogRecord{
		Ev: GetEventName(r.Type), Origin: r.Origin.String(), Domain: core.DomainNames[r.Domain],
		Payload: r.Payload, EncodeErr: r.EncodeErr,
		JSeq: r.JSeq, Seq: r.Seq, Run: r.Run, Tick: r.Tick, Boundary: r.Boundary,
	}
}

func (r LogRecord) record() (JournalRecord, error) {
	et, ok := GetEventType(r.Ev)
	if !ok {
		return JournalRecord{}, fmt.Errorf("session log: jseq %d names unknown event %q", r.JSeq, r.Ev)
	}
	origin, ok := ParseOrigin(r.Origin)
	if !ok {
		return JournalRecord{}, fmt.Errorf("session log: jseq %d names unknown origin %q", r.JSeq, r.Origin)
	}
	domain, ok := core.ParseDomain(r.Domain)
	if !ok {
		return JournalRecord{}, fmt.Errorf("session log: jseq %d names unknown domain %q", r.JSeq, r.Domain)
	}
	return JournalRecord{
		Payload: r.Payload, EncodeErr: r.EncodeErr,
		JSeq: r.JSeq, Seq: r.Seq, Run: r.Run, Tick: r.Tick, Boundary: r.Boundary,
		Type: et, Origin: origin, Domain: domain,
	}, nil
}
