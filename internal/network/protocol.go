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
	// the startup offer; authentication; and 0x26, which carried the retired
	// replay-the-session-from-tick-zero join and is reserved for the authoritative
	// state snapshot that replaces it.
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
