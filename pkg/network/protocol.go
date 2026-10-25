package network

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// MessageType is the kind byte that opens every frame. The session layer enumerates
// the kinds and describes them to a link in a Kinds table; the link owns the three
// it sends or packs on its own.
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

// Traits is what a link knows of one kind: the family its bytes count under, whether
// its frames travel bundled, and whether its body arrived deflated already, which
// the link passes raw past its own stream rather than deflating twice.
type Traits struct {
	Kind          ByteKind
	Bundles       bool
	Precompressed bool
}

// Kinds is the session layer's table over the kind byte, which a link reads and
// never writes: each kind's traits, and the kinds the link acts on itself. Ready
// confirms a start gate on the link that carries it; PeerLink and Join are what a
// dialer opens a stream with, which tell a door which handshake runs.
type Kinds struct {
	traits                [256]Traits
	Ready, PeerLink, Join MessageType
}

// NewKinds tabulates traits; a kind not named counts as control, travels alone and
// arrives plain.
func NewKinds(traits map[MessageType]Traits, ready, peerLink, join MessageType) Kinds {
	k := Kinds{Ready: ready, PeerLink: peerLink, Join: join}
	for i := range k.traits {
		k.traits[i].Kind = KindControl
	}
	for t, tr := range traits {
		k.traits[t] = tr
	}
	return k
}

// Traits is what the table says of t.
func (k *Kinds) Traits(t MessageType) Traits { return k.traits[t] }

// The kinds the link owns, numbered among the session's: a tick's frames for one
// peer packed into one (Bundle), and the probe and echo a port answers itself, the
// only round trip the protocol times. The transport answers a probe before it
// reaches a tick, so they measure the wire; an echo returns the probe's bytes, the
// far end's received byte count and read time, and the LinkReport it last published.
const (
	MsgLinkProbe MessageType = 0x14 // a link measurement, awaiting its echo
	MsgLinkEcho  MessageType = 0x15 // one probe answered, with the peer's report
	MsgBundle    MessageType = 0x19 // several frames for one peer as one, each [type:1][len:2][payload]
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
	if s, ok := w.(*Stream); ok && s.w != nil {
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

// DecodeWithin reads a message whose payload is at most limit bytes, refusing a
// longer one before reading any of it: a handshake frame is bounded by its purpose.
func DecodeWithin(r io.Reader, limit int) (*Message, error) {
	var header [HeaderSize]byte
	return decode(r, limit, header[:])
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
