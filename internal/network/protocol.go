package network

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// MessageType identifies the semantic meaning of a message
type MessageType uint8

// ByteKind is the family a frame's bytes are counted under, so a link's traffic is
// read by what it carries: the command stream (epochs and syncs), whole
// corrections, the repair exchange, and control.
type ByteKind uint8

const (
	KindEpoch ByteKind = iota
	KindSync
	KindCorrection
	KindRepair
	KindControl
	KindCount
)

var byteKindNames = [KindCount]string{"epoch", "sync", "correction", "repair", "control"}

func (k ByteKind) String() string { return byteKindNames[k] }

// Bundles reports whether frames of kind t travel bundled: what a link is sent of a
// tick's epochs and syncs leaves as one MsgBundle.
func (t MessageType) Bundles() bool { return t == MsgEvent || t == MsgStateSync }

// Precompressed reports whether t carries a body the snapshot codec deflated, which
// a link passes raw past its own stream rather than deflating twice.
func (t MessageType) Precompressed() bool {
	switch t {
	case MsgStateSnapshot, MsgStateCorrection, MsgStateShard, MsgScenarioBody:
		return true
	}
	return false
}

// Kind is the family t's bytes count under.
func (t MessageType) Kind() ByteKind {
	switch t {
	case MsgEvent, MsgBundle:
		return KindEpoch
	case MsgStateSync:
		return KindSync
	case MsgStateCorrection, MsgStateSnapshot:
		return KindCorrection
	case MsgStateManifest, MsgStateRequest, MsgStateShard, MsgStateUnserved:
		return KindRepair
	}
	return KindControl
}

// Live message types are the ones a session actually exchanges. The reserved codes
// hold their place in the numbering for capabilities the transport does not have
// yet; nothing sends or accepts them, and NetworkSystem counts an unrecognised type
// as a drop rather than translating it.
const (
	// MsgSessionRoute is the one frame a dialer sends before it reads: the session
	// name it was given, empty for none, so one address can carry several. A front
	// door reads it to place the connection, and the session refuses a name not its
	// own, so a stale link is refused rather than seated in a stranger's match. It is
	// a routing key, not a credential: the port is unauthenticated by decision.
	MsgSessionRoute MessageType = 0x05 // Live: the session name a dialer is holding

	// MsgJoinClaims is the coordinator asking a dialer, before it assigns a place,
	// which places it can come back to, and the dialer's answer: the token of each
	// claim it keeps. Asked rather than sent with the route, so a door that refuses
	// the route has read all a dialer sent and its refusal arrives whole.
	MsgJoinClaims MessageType = 0x06 // Live: the claims a dialer comes back with

	// Preparing holds nothing in the session. A preview is the offer an assignment
	// would make, with no identity in it, which the dialer builds its world on.
	// Prepared opens the coordinator's ready period for the player's press; seat is
	// the press, or a dialer that needs none, and only then is a place assigned. A
	// ready period that runs out ends the handshake with nothing to give back.
	MsgJoinPreview  MessageType = 0x07 // Live: the session a dialer prepares for
	MsgJoinPrepared MessageType = 0x08 // Live: the dialer is ready and waits for its press
	MsgJoinSeat     MessageType = 0x09 // Live: the dialer asks for its place now

	// Game. 0x10 is reserved and stays so: raw participant input is not a message
	// kind — a peer sends the resolved D-3 artifact, never the keystroke.
	MsgStateSync MessageType = 0x11 // Live: one cursor's owner-authored state (D-13)
	MsgEvent     MessageType = 0x12 // Live: one closed barrier production epoch

	// MsgLinkProbe and MsgLinkEcho are the only round trip the protocol times; every
	// other measurement is one-directional. The transport answers a probe before it
	// reaches a tick, so they measure the wire, not this instance's scheduling. An
	// echo returns the probe's bytes, the far end's received byte count and read time,
	// and the LinkReport its world last published: the only game state on them.
	MsgLinkProbe MessageType = 0x14 // Live: a link measurement, awaiting its echo
	MsgLinkEcho  MessageType = 0x15 // Live: one probe answered, with the peer's report

	// MsgScenarioRequest and MsgScenarioBody carry the session's scenario to a joiner
	// whose roots lack it: the request names the preview's digest, and the answer is
	// the deflated canonical form, chunked like a capture. They sit before the joiner
	// reports an identity the coordinator would refuse it on, while it prepares or,
	// seated at once, before its reply, and one request fits a handshake.
	MsgScenarioRequest MessageType = 0x16 // Live: a joiner asking for the session's scenario
	MsgScenarioBody    MessageType = 0x17 // Live: one chunk of the deflated scenario

	// MsgSessionRestart is the authority saying this session is rebuilding on
	// another scenario. Its payload is the address to redial, where the authority
	// listens or the one it declared on inheriting the session, so a restart follows
	// a migrated authority; the offer given on redial names what is being played.
	// Both sides restart the run, so there is no state to hand over.
	MsgSessionRestart MessageType = 0x18 // Live: the authority is rebuilding the session

	// MsgBundle is a tick's raw epochs and syncs for the authority as one frame: a
	// guest's own and those it carries for the participants behind it. Each entry is
	// [type:1][len:2][payload]; see Bundle.
	MsgBundle MessageType = 0x19 // Live: several frames for the authority, packed into one

	// MsgStateSnapshot carries one chunk of an authoritative shared-world capture
	// (D-19). Its size is a function of the world rather than of the format, so it is
	// split; see snapshot.go for the chunk header.
	MsgStateSnapshot MessageType = 0x26 // Live: one chunk of a shared-world capture

	// MsgStateCorrection carries one chunk of a periodic authoritative correction, a
	// whole capture or a delta against the last keyframe. It is chunked as
	// MsgStateSnapshot but is its own kind: a capture is part of a handshake on a
	// stream nothing else owns yet, a correction arrives mid-session beside epochs,
	// syncs and digests.
	MsgStateCorrection MessageType = 0x27 // Live: one chunk of an authoritative correction

	// The selective-correction exchange: a manifest indexes the capture a correction
	// would carry, sent on each peer's cadence; a request, sent back to the peer the
	// manifest came from, names the pages a receiver could not reproduce; a shard set
	// is that peer's repair, chunked as a correction body is and bounded by the
	// keyframe it stands in for, which answers a wider one.
	MsgStateManifest MessageType = 0x28 // Live: a correction manifest, root and section hashes
	MsgStateRequest  MessageType = 0x29 // Live: one receiver's answer to a manifest
	MsgStateShard    MessageType = 0x2A // Live: the pages one request asked for

	// MsgStateUnserved answers a request a retention holder cannot serve: it holds no
	// index for the tick or none that answers the pages, the repair is too wide, or
	// the request asks a relay for a whole world. Silence would cost the receiver a
	// cadence, and a body from another baseline is the mixed-baseline assembly the
	// supersession rules make unreachable.
	MsgStateUnserved MessageType = 0x2B // Live: a request this peer's retention cannot answer

	// Succession, in the order it runs. A report is the news that the authority is
	// gone, flooded so a participant two links away learns of a loss only its
	// neighbour observed; a handoff, a membership change, lets a receiver adopt a
	// term it has never seen. There is no vote: the successor is a function of the
	// chain and the roster (see authority.go), so 0x2D is retired rather than reused.
	MsgAuthorityReport  MessageType = 0x2C // Live: a survivor's notice that the authority is gone
	MsgAuthorityHandoff MessageType = 0x2E // Live: the record a successor authors under

	// MsgPeerList is the succession chain, flooded whole: the eligible participants
	// in join order and where each listens, so a survivor can dial the successor a
	// handoff names. It rides the authority's term and is refused below the term the
	// receiver holds, like every other authoritative artifact. See reach.go.
	MsgPeerList MessageType = 0x20 // Live: the succession chain and its addresses

	// MsgRelayJoin and MsgRelayAdmit admit a participant on the link of the one it
	// dialled rather than on the coordinator's: a guest's bot joins through its
	// holder. The holder runs the handshake and serves the world; the coordinator
	// still decides, asked on the holder's own link at the two steps that change the
	// roster — allocating the identity and crossing the arrival.
	MsgRelayJoin  MessageType = 0x21 // Live: a relay asking the coordinator to decide one step
	MsgRelayAdmit MessageType = 0x2F // Live: the coordinator's answer, on the same link

	// MsgConnect and MsgAck are the peer link: two participants that already hold
	// identities the coordinator assigned opening a stream between themselves.
	// Deliberately not a join — nothing is allocated, offered or captured — and
	// deliberately not the coordinator's handshake, which a guest could not serve.
	MsgConnect MessageType = 0x02 // Live: a peer opening a link, or confirming a bind
	MsgAck     MessageType = 0x04 // Live: the peer link answered

	// Membership. A departure is observed only by a direct neighbour, so a neighbour
	// that is not the coordinator forwards a notice rather than acting on it.
	MsgDisconnect MessageType = 0x03 // Live: a participant's link was lost

	// Coordination, in the order the startup handshake runs them
	MsgJoinOffer MessageType = 0x22 // Live: host offers the session anchor and roster assignment
	MsgJoinReply MessageType = 0x23 // Live: joiner accepts or rejects the offered identity
	MsgStart     MessageType = 0x24 // Live: host releases a joiner into the capture that follows
	MsgReady     MessageType = 0x25 // Live: joiner confirms it received the start gate

	// Reserved, unused: authentication.
	MsgAuthRequest  MessageType = 0x30
	MsgAuthResponse MessageType = 0x31
)

// Header precedes every message on the wire
// Fixed 12 bytes: [Type:1][Flags:1][Seq:4][Ack:4][Len:2]
const HeaderSize = 12

// MaxPayloadSize is what the header's 16-bit length field can describe. Anything
// larger than this has to be split by its producer.
const MaxPayloadSize = 65535

// Header flags
const (
	FlagNone    uint8 = 0x00
	FlagNeedAck uint8 = 0x01 // Sender expects acknowledgment
)

// Message represents a framed network message
type Message struct {
	Type    MessageType
	Flags   uint8
	Seq     uint32 // Sender's sequence number
	Ack     uint32 // Last received sequence from peer
	Payload []byte
}

// Encode writes the message to a writer with length prefix; an opened link stream
// takes it as a frame of its own.
func (m *Message) Encode(w io.Writer) error {
	var header [HeaderSize]byte
	if s, ok := w.(*flateStream); ok && s.w != nil {
		return s.writeFrame(m, header[:])
	}
	return m.encode(w, header[:])
}

// encode frames the message through header, a HeaderSize scratch the caller owns:
// a link's writer frames thousands a second and keeps one.
func (m *Message) encode(w io.Writer, header []byte) error {
	payloadLen := len(m.Payload)
	if payloadLen > MaxPayloadSize {
		return errors.New("payload exceeds maximum size")
	}

	header = header[:HeaderSize]
	header[0] = byte(m.Type)
	header[1] = m.Flags
	binary.BigEndian.PutUint32(header[2:6], m.Seq)
	binary.BigEndian.PutUint32(header[6:10], m.Ack)
	binary.BigEndian.PutUint16(header[10:12], uint16(payloadLen))

	if err := writeFull(w, header); err != nil {
		return err
	}

	if payloadLen > 0 {
		if err := writeFull(w, m.Payload); err != nil {
			return err
		}
	}

	return nil
}

// writeFull turns a short stream write into either a complete frame or an error.
func writeFull(w io.Writer, p []byte) error {
	for len(p) != 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}

// Decode reads a message from a reader
func Decode(r io.Reader) (*Message, error) {
	var header [HeaderSize]byte
	return decode(r, MaxPayloadSize, header[:])
}

// decode refuses a payload longer than limit before reading any of it; header is
// a HeaderSize scratch the caller owns.
func decode(r io.Reader, limit int, header []byte) (*Message, error) {
	header = header[:HeaderSize]
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}

	payloadLen := binary.BigEndian.Uint16(header[10:12])
	if int(payloadLen) > limit {
		return nil, fmt.Errorf("message %#x: a %d-byte payload exceeds %d", header[0], payloadLen, limit)
	}

	m := &Message{
		Type:  MessageType(header[0]),
		Flags: header[1],
		Seq:   binary.BigEndian.Uint32(header[2:6]),
		Ack:   binary.BigEndian.Uint32(header[6:10]),
	}

	if payloadLen > 0 {
		m.Payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(r, m.Payload); err != nil {
			return nil, err
		}
	}

	return m, nil
}

// Bundle packs frames bound for one peer into as few MsgBundle payloads as the frame
// bound allows; a frame too large to share one travels alone.
type Bundle struct{ out []Message }

// Add appends one frame.
func (b *Bundle) Add(kind MessageType, payload []byte) {
	entry := 3 + len(payload)
	if entry > MaxPayloadSize {
		b.out = append(b.out, Message{Type: kind, Payload: payload})
		return
	}
	n := len(b.out)
	if n == 0 || b.out[n-1].Type != MsgBundle || len(b.out[n-1].Payload)+entry > MaxPayloadSize {
		b.out = append(b.out, Message{Type: MsgBundle})
		n++
	}
	last := &b.out[n-1]
	last.Payload = binary.BigEndian.AppendUint16(append(last.Payload, byte(kind)), uint16(len(payload)))
	last.Payload = append(last.Payload, payload...)
}

// Take returns the frames packed so far and empties the bundle.
func (b *Bundle) Take() []Message {
	out := b.out
	b.out = nil
	return out
}

// Unbundle calls each for every frame a MsgBundle payload carries, in order, and
// stops at the first malformed entry.
func Unbundle(body []byte, each func(MessageType, []byte)) error {
	for len(body) > 0 {
		if len(body) < 3 {
			return errors.New("bundle entry header truncated")
		}
		n := 3 + int(binary.BigEndian.Uint16(body[1:3]))
		if len(body) < n {
			return errors.New("bundle entry truncated")
		}
		each(MessageType(body[0]), body[3:n])
		body = body[n:]
	}
	return nil
}

// NewMessage creates a message with the given type and payload
func NewMessage(t MessageType, payload []byte) *Message {
	return &Message{
		Type:    t,
		Flags:   FlagNone,
		Payload: payload,
	}
}
