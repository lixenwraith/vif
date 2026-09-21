package network

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

// SnapshotChunkHeader precedes each frame of a capture in transit:
// [Tick:8][Index:4][Count:4][Total:4].
//
// A capture is the one message in this protocol whose size is a function of the
// world rather than of the format, so it is the one that has to be split: the frame
// header's length field is 16 bits and a storm's world does not fit in it. The
// chunk header names the whole transfer rather than just this piece — the tick it
// describes, which piece this is, how many there are, and how many bytes the
// reassembled body must come to — so a receiver can size its buffer once, reject a
// transfer that changes its mind mid-stream, and say which of the two it was.
const SnapshotChunkHeader = 20

// SnapshotChunkBody is the payload each chunk carries. The frame header is already
// accounted for by MaxPayloadSize.
const SnapshotChunkBody = MaxPayloadSize - SnapshotChunkHeader

// MaxSnapshotBytes bounds a transfer either end will handle at all: the bytes on
// the wire, after the codec has compressed them. The largest world the engine
// holds — MaxMapCells walled, which is more than any scenario builds — measures
// 1.1 MiB here, so this keeps four times that as headroom for a world that grows.
//
// The defence is snapshotReserve below: a receiver allocates for the bytes that
// have arrived rather than for the bytes a sender says are coming, so the memory
// one peer can make a receiver hold is bounded by what that peer actually sends.
const MaxSnapshotBytes = 4 << 20

// MaxSnapshotPlainBytes bounds the body a capture expands to, which is a separate
// quantity from the one above: captures of a walled map compress better than 20:1,
// so a transfer well inside the wire ceiling still expands past it. That worst case
// measures 24.3 MiB, and this is the next power of two.
//
// It is the decompression bound, so it is stated as a ratio rather than a size: a
// peer may still send only MaxSnapshotBytes, and this is how far a receiver will
// let those bytes expand. Eight to one is what the codec achieves on a real world;
// flate's own worst case is 1032 to one.
const MaxSnapshotPlainBytes = 8 * MaxSnapshotBytes

// snapshotReserve is the most a receiver reserves up front for a transfer it has
// only seen the first chunk of. An ordinary world's capture fits inside it and is
// allocated once; a walled map's is several times it and grows by append, which
// costs a few copies on a path that runs once per keyframe and removes the last
// place a peer could name a number and have it allocated.
const snapshotReserve = 64 << 10

// EncodeSnapshotChunks splits an encoded capture into wire frames.
func EncodeSnapshotChunks(tick uint64, body []byte) ([][]byte, error) {
	if len(body) == 0 {
		return nil, errors.New("snapshot: empty body")
	}
	if len(body) > MaxSnapshotBytes {
		return nil, fmt.Errorf("snapshot: %d bytes exceeds the %d-byte ceiling", len(body), MaxSnapshotBytes)
	}
	count := (len(body) + SnapshotChunkBody - 1) / SnapshotChunkBody
	out := make([][]byte, 0, count)
	for i := range count {
		lo := i * SnapshotChunkBody
		hi := min(lo+SnapshotChunkBody, len(body))
		frame := make([]byte, SnapshotChunkHeader+hi-lo)
		binary.BigEndian.PutUint64(frame[0:8], tick)
		binary.BigEndian.PutUint32(frame[8:12], uint32(i))
		binary.BigEndian.PutUint32(frame[12:16], uint32(count))
		binary.BigEndian.PutUint32(frame[16:20], uint32(len(body)))
		copy(frame[SnapshotChunkHeader:], body[lo:hi])
		out = append(out, frame)
	}
	return out, nil
}

// SnapshotAssembly reassembles a chunked capture or correction. The zero value is
// ready to use.
//
// It admits chunks in order, and what it does with the ones that are not in order
// depends on which of two situations it is in. During a join a capture arrives on
// one stream, before the participant sending it is admitted to anything that could
// reorder it, so anything out of order is a confused sender: tolerating it silently
// would let two captures interleave into one body that hashes as neither.
//
// A correction arrives mid-session on a mesh, where the same tick reaches a node by
// several paths and a newer transfer can start before an older one finishes. Three
// out-of-order cases are therefore *expected* rather than wrong: a chunk of the
// transfer in progress that this node has already taken, any chunk naming a tick it
// has already assembled whole — the paths carry differently shaped bodies for one
// tick — and a chunk of a *newer* transfer, which supersedes the one in progress
// because a correction carries no information the one after it lacks. Everything
// else is still an error.
type SnapshotAssembly struct {
	tick    uint64
	count   uint32
	next    uint32
	total   uint32
	body    []byte
	started bool
}

// Add admits one chunk and reports whether the transfer is complete. It is the
// join's form: a chunk this assembly cannot take in order is an error.
func (s *SnapshotAssembly) Add(frame []byte) (done bool, err error) {
	admitted, done, err := s.AddChunk(frame)
	if err == nil && !admitted && !done {
		return false, errors.New("snapshot chunk: out of the transfer's order")
	}
	return done, err
}

// AddChunk admits one chunk and reports whether it was new to this assembly as well
// as whether the transfer is complete.
//
// The first return is what a relay forwards on. It is the same termination argument
// the artifact flood uses: a node forwards only what it admitted, a second copy
// arriving by another path is recognised and neither taken nor forwarded again, so
// each node handles each chunk exactly once whatever the topology.
func (s *SnapshotAssembly) AddChunk(frame []byte) (admitted, done bool, err error) {
	if len(frame) < SnapshotChunkHeader {
		return false, false, fmt.Errorf("snapshot chunk: %d bytes, want at least %d", len(frame), SnapshotChunkHeader)
	}
	tick := binary.BigEndian.Uint64(frame[0:8])
	index := binary.BigEndian.Uint32(frame[8:12])
	count := binary.BigEndian.Uint32(frame[12:16])
	total := binary.BigEndian.Uint32(frame[16:20])
	payload := frame[SnapshotChunkHeader:]

	switch {
	case count == 0:
		return false, false, errors.New("snapshot chunk: names a zero-chunk transfer")
	case total == 0 || total > MaxSnapshotBytes:
		return false, false, fmt.Errorf("snapshot chunk: names a %d-byte body", total)
	case index >= count:
		return false, false, fmt.Errorf("snapshot chunk %d of %d is past the end", index, count)
	}
	// A newer transfer supersedes whatever is half-assembled, and must start at its
	// own first chunk: joining one in the middle would build a body from two.
	if s.started && tick > s.tick {
		if index != 0 {
			return false, false, nil
		}
		*s = SnapshotAssembly{}
	}
	if !s.started {
		if index != 0 {
			// A transfer already in flight when this receiver started listening.
			// There is no body to build from its middle and nothing to report: the
			// next one starts at its own first chunk.
			return false, false, nil
		}
		s.tick, s.count, s.total, s.started = tick, count, total, true
		s.body = make([]byte, 0, min(int(total), snapshotReserve))
	}
	// Already taken, or moved past. Two paths can carry differently shaped bodies
	// for the same tick — a delta down one and the keyframe down another — and once
	// either is whole the other describes a world this node already holds.
	if tick < s.tick || (tick == s.tick && s.next == s.count) {
		return false, false, nil
	}
	if tick != s.tick || count != s.count || total != s.total {
		return false, false, fmt.Errorf("snapshot chunk %d: transfer changed to tick %d, %d chunks, %d bytes",
			index, tick, count, total)
	}
	if index < s.next {
		return false, false, nil // a second copy of a chunk already taken
	}
	if index != s.next {
		return false, false, fmt.Errorf("snapshot chunk %d arrived out of order, expected %d", index, s.next)
	}
	if uint32(len(s.body)+len(payload)) > s.total {
		return false, false, errors.New("snapshot chunks overrun the body length they declared")
	}
	s.body = append(s.body, payload...)
	s.next++
	if s.next < s.count {
		return true, false, nil
	}
	if uint32(len(s.body)) != s.total {
		return false, false, fmt.Errorf("snapshot reassembled to %d bytes, declared %d", len(s.body), s.total)
	}
	return true, true, nil
}

// Result returns the reassembled body and the tick it describes.
func (s *SnapshotAssembly) Result() (uint64, []byte) { return s.tick, s.body }

// Tick names the transfer in progress, zero before the first chunk.
func (s *SnapshotAssembly) Tick() uint64 { return s.tick }

// writeSnapshot sends a whole capture down a raw handshake stream.
func writeSnapshot(conn net.Conn, timeout time.Duration, tick uint64, body []byte) error {
	chunks, err := EncodeSnapshotChunks(tick, body)
	if err != nil {
		return err
	}
	for _, c := range chunks {
		if timeout > 0 {
			_ = conn.SetWriteDeadline(time.Now().Add(timeout))
		}
		if err := NewMessage(MsgStateSnapshot, c).Encode(conn); err != nil {
			_ = conn.SetWriteDeadline(time.Time{})
			return err
		}
	}
	_ = conn.SetWriteDeadline(time.Time{})
	return nil
}

// readSnapshot reads a whole capture from a raw handshake stream, holding the
// session traffic that arrives alongside it.
//
// The interleaving is by design rather than by accident. A mid-run host admits a
// participant before it reads the world for it, so the epochs it produces during
// the transfer reach that participant instead of falling into the gap; they arrive
// on this stream, between chunks, and hold keeps them for the barrier to receive
// once the world they apply to exists. A heartbeat is skipped the same way and
// carries nothing. Anything else is a protocol error, because the next message on
// this stream decides what the joiner does with the world it just received.
func readSnapshot(p *PendingJoin, timeout time.Duration) (uint64, []byte, error) {
	return readChunked(p, MsgStateSnapshot, timeout)
}

// readChunked reassembles one chunked transfer off the handshake stream, holding
// the session traffic that arrives beside it. want names the kind: a capture and a
// scenario are the two messages whose size is a function of their content, and
// they share the framing because they are the same problem.
func readChunked(p *PendingJoin, want MessageType, timeout time.Duration) (uint64, []byte, error) {
	var asm SnapshotAssembly
	for {
		if timeout > 0 {
			_ = p.conn.SetReadDeadline(time.Now().Add(timeout))
		}
		msg, err := Decode(p.conn)
		if err != nil {
			_ = p.conn.SetReadDeadline(time.Time{})
			return 0, nil, err
		}
		if p.hold(msg) {
			continue
		}
		if msg.Type == MsgJoinReply {
			// The coordinator refused mid-transfer. Its reason is worth more than
			// the surprise at the message kind, and it is the only thing the
			// dialer can act on.
			_ = p.conn.SetReadDeadline(time.Time{})
			return 0, nil, refusalFrom(msg)
		}
		if msg.Type != want {
			_ = p.conn.SetReadDeadline(time.Time{})
			return 0, nil, fmt.Errorf("join transfer: got message %#x, want %#x", msg.Type, want)
		}
		done, err := asm.Add(msg.Payload)
		if err != nil {
			_ = p.conn.SetReadDeadline(time.Time{})
			return 0, nil, err
		}
		if done {
			_ = p.conn.SetReadDeadline(time.Time{})
			tick, body := asm.Result()
			return tick, body, nil
		}
	}
}
