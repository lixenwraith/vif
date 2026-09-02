package network

import (
	"encoding/binary"
	"errors"
	"io"
)

// MessageType identifies the semantic meaning of a message
type MessageType uint8

// Live message types are the ones a session actually exchanges. The reserved codes
// hold their place in the numbering for capabilities the transport does not have
// yet; nothing sends or accepts them, and NetworkSystem counts an unrecognised type
// as a drop rather than translating it.
const (
	// Control
	MsgHeartbeat MessageType = 0x01 // Live: keeps a silent stream inside its read deadline

	// Game. 0x10 is reserved and stays so: raw participant input is not a message
	// kind — a peer sends the resolved D-3 artifact, never the keystroke.
	MsgStateSync   MessageType = 0x11 // Live: one cursor's owner-authored state (D-13)
	MsgEvent       MessageType = 0x12 // Live: one closed barrier production epoch
	MsgStateDigest MessageType = 0x13 // Live: periodic shared-world parity probe

	// MsgLinkProbe and MsgLinkEcho are the round trip. Nothing else in this
	// protocol makes one — every other measurement is one-directional — so the
	// cadence had nothing but a constant to be chosen from until these existed.
	//
	// They are answered inside the transport, before the frame reaches a tick, so
	// what they measure is the wire rather than this instance's scheduling. An
	// echo carries back the probe's own bytes untouched, the bytes the far end has
	// received on the link, and the opaque LinkReport the far end's world last
	// published — which is the only game state that ever travels on them.
	MsgLinkProbe MessageType = 0x14 // Live: a link measurement, awaiting its echo
	MsgLinkEcho  MessageType = 0x15 // Live: one probe answered, with the peer's report

	// MsgStateSnapshot carries one chunk of an authoritative shared-world capture
	// (D-19). It is the only message whose total size is a function of the world
	// rather than of the format, so it is the only one that is split; see
	// snapshot.go for the chunk header.
	MsgStateSnapshot MessageType = 0x26 // Live: one chunk of a shared-world capture

	// MsgStateCorrection carries one chunk of a periodic authoritative correction:
	// either a whole capture or a delta against the last one the host sent whole.
	// It is the same chunking as MsgStateSnapshot and a different message because
	// it arrives at a different moment — a capture is part of a handshake, on a
	// stream nothing else owns yet, and a correction arrives mid-session on a port
	// that is also carrying epochs, syncs and digests.
	MsgStateCorrection MessageType = 0x27 // Live: one chunk of an authoritative correction

	// The Phase 6 selective-correction exchange. A manifest is a compact index over
	// the same capture a correction would carry; a request is a receiver's answer to
	// one, naming the pages it could not reproduce; a shard set is the repair. All
	// three are separate message kinds rather than shapes of MsgStateCorrection
	// because they travel in different directions and are handled at different
	// moments — a manifest is broadcast on the cadence, a request is unicast back to
	// the authority, and a repair is unicast to the peer that asked.
	//
	// None of the three is chunked: each is bounded to one transport frame by
	// construction (see parameter.SnapshotShardBytesMax), and a repair too wide for
	// one frame is not a repair — the host answers it with a keyframe, which is
	// chunked, self-sufficient and already part of the protocol.
	MsgStateManifest MessageType = 0x28 // Live: a correction manifest, root and section hashes
	MsgStateRequest  MessageType = 0x29 // Live: one receiver's answer to a manifest
	MsgStateShard    MessageType = 0x2A // Live: the pages one request asked for

	// Membership. A departure is observed only by a direct neighbour, so a neighbour
	// that is not the coordinator forwards a notice rather than acting on it.
	MsgDisconnect MessageType = 0x03 // Live: a participant's link was lost

	// Coordination, in the order the startup handshake runs them
	MsgJoinOffer MessageType = 0x22 // Live: host offers the session anchor and roster assignment
	MsgJoinReply MessageType = 0x23 // Live: joiner accepts or rejects the offered identity
	MsgStart     MessageType = 0x24 // Live: host releases the participants into tick zero
	MsgReady     MessageType = 0x25 // Live: joiner confirms it received the start gate

	// Reserved, unused: explicit connect and acknowledgement control, which the
	// stream's own lifecycle carries today; roster and coordinator assignment beyond
	// the startup offer; and authentication. 0x26 is no longer among them: it
	// carried the retired replay-the-session-from-tick-zero join and now carries the
	// authoritative state snapshot that replaced it. Neither is 0x27, which carries
	// the periodic correction that snapshot became once the host was the authority,
	// nor 0x14/0x15, which carry the round trip Phase 5 added, nor 0x28..0x2A,
	// which carry Phase 6's manifest, request and repair.
	MsgConnect      MessageType = 0x02
	MsgAck          MessageType = 0x04
	MsgPeerList     MessageType = 0x20
	MsgRoleAssign   MessageType = 0x21
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
	FlagNone       uint8 = 0x00
	FlagNeedAck    uint8 = 0x01 // Sender expects acknowledgment
	FlagCompressed uint8 = 0x02 // Payload is compressed (future)
)

// Message represents a framed network message
type Message struct {
	Type    MessageType
	Flags   uint8
	Seq     uint32 // Sender's sequence number
	Ack     uint32 // Last received sequence from peer
	Payload []byte
}

// Encode writes the message to a writer with length prefix
func (m *Message) Encode(w io.Writer) error {
	payloadLen := len(m.Payload)
	if payloadLen > MaxPayloadSize {
		return errors.New("payload exceeds maximum size")
	}

	header := make([]byte, HeaderSize)
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
	header := make([]byte, HeaderSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}

	payloadLen := binary.BigEndian.Uint16(header[10:12])

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

// NewMessage creates a message with the given type and payload
func NewMessage(t MessageType, payload []byte) *Message {
	return &Message{
		Type:    t,
		Flags:   FlagNone,
		Payload: payload,
	}
}
