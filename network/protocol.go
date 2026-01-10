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

	// Game messages
	MsgInput     MessageType = 0x10 // Player keystroke
	MsgStateSync MessageType = 0x11 // Full/delta state snapshot
	MsgEvent     MessageType = 0x12 // Game event broadcast

	// Coordination
	MsgPeerList   MessageType = 0x20 // Server sends peer roster
	MsgRoleAssign MessageType = 0x21 // Coordinator assignment

	// Future: auth
	MsgAuthRequest  MessageType = 0x30
	MsgAuthResponse MessageType = 0x31
)

// HeaderEntity precedes every message on the wire
// Fixed 12 bytes: [Type:1][Flags:1][Seq:4][Ack:4][Len:2]
const HeaderSize = 12

// HeaderEntity flags
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

	if _, err := w.Write(header); err != nil {
		return err
	}

	if payloadLen > 0 {
		if _, err := w.Write(m.Payload); err != nil {
			return err
		}
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