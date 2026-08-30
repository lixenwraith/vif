package network

import (
	"encoding/binary"
	"errors"
	"io"
)

// MessageType identifies the semantic meaning of a message
type MessageType uint8

const (
	// Control messages (reliable, ordered)
	MsgHeartbeat  MessageType = 0x01
	MsgConnect    MessageType = 0x02
	MsgDisconnect MessageType = 0x03
	MsgAck        MessageType = 0x04

	// Game messages. 0x10 is reserved: raw participant input is deliberately not a
	// message kind — a peer sends the resolved D-3 artifact, never the keystroke.
	MsgStateSync MessageType = 0x11 // Owner-authored cursor state (D-13)
	MsgEvent     MessageType = 0x12 // One closed barrier production epoch

	// Coordination
	MsgPeerList   MessageType = 0x20 // Server sends peer roster
	MsgRoleAssign MessageType = 0x21 // Coordinator assignment
	MsgJoinOffer  MessageType = 0x22 // Host offers the session anchor and roster assignment
	MsgJoinReply  MessageType = 0x23 // Joiner accepts or rejects the offered identity
	MsgStart      MessageType = 0x24 // Host releases both participants into tick zero
	MsgReady      MessageType = 0x25 // Joiner confirms it received the start gate

	// Future: auth
	MsgAuthRequest  MessageType = 0x30
	MsgAuthResponse MessageType = 0x31
)

// Header precedes every message on the wire
// Fixed 12 bytes: [Type:1][Flags:1][Seq:4][Ack:4][Len:2]
const HeaderSize = 12

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
	if payloadLen > 65535 {
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

// NewAckMessage creates an acknowledgment for a received sequence
func NewAckMessage(ackSeq uint32) *Message {
	return &Message{
		Type: MsgAck,
		Ack:  ackSeq,
	}
}
