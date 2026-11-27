package network

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// SnapshotChunkHeader precedes each frame of a capture in transit:
// [Run:8][Tick:8][Index:4][Count:4][Total:4]. A capture's size is a function of
// the world, so it is the message that has to be split; the header names the whole
// transfer — the run and tick it describes, which piece this is, how many there are
// and the body's length — so a receiver sizes its buffer once and orders transfers.
const SnapshotChunkHeader = 28

// SnapshotChunkBody is the payload each chunk carries. The frame header is already
// accounted for by MaxPayloadSize.
const SnapshotChunkBody = MaxPayloadSize - SnapshotChunkHeader

// MaxSnapshotBytes bounds a transfer either end handles at all, the bytes on the
// wire: four times the 1.1 MiB the largest world measured, MaxMapCells walled, as
// headroom for a world that grows. The defense is snapshotReserve: a receiver
// allocates for the bytes that arrived rather than the ones a sender names, so what
// one peer can make a receiver hold is bounded by what it sends.
const MaxSnapshotBytes = 4 << 20

// MaxSnapshotPlainBytes bounds the body a capture expands to: a walled map's capture
// compresses past 20:1, so a transfer well inside the wire ceiling expands past it,
// to 24.3 MiB at worst, and this is the next power of two. It is the decompression
// bound, so it is a ratio of the wire's; flate's own worst case is 1032 to one.
const MaxSnapshotPlainBytes = 8 * MaxSnapshotBytes

// snapshotReserve is the most a receiver reserves up front for a transfer it has
// only seen the first chunk of. An ordinary world's capture fits inside it and is
// allocated once; a walled map's is several times it and grows by append, which
// costs a few copies on a path that runs once per keyframe and removes the last
// place a peer could name a number and have it allocated.
const snapshotReserve = 64 << 10

// EncodeSnapshotChunks splits an encoded capture into wire frames. run and tick
// order transfers on an assembly that sees several; a join's sees one, and passes
// zero.
func EncodeSnapshotChunks(run, tick uint64, body []byte) ([][]byte, error) {
	return EncodeSnapshotChunksOf(run, tick, body, SnapshotChunkBody)
}

// EncodeSnapshotChunksOf splits body into frames carrying at most size bytes of it.
func EncodeSnapshotChunksOf(run, tick uint64, body []byte, size int) ([][]byte, error) {
	if len(body) == 0 {
		return nil, errors.New("snapshot: empty body")
	}
	if len(body) > MaxSnapshotBytes {
		return nil, fmt.Errorf("snapshot: %d bytes exceeds the %d-byte ceiling", len(body), MaxSnapshotBytes)
	}
	size = min(max(size, 1), SnapshotChunkBody)
	count := (len(body) + size - 1) / size
	out := make([][]byte, 0, count)
	for i := range count {
		lo := i * size
		hi := min(lo+size, len(body))
		frame := make([]byte, SnapshotChunkHeader+hi-lo)
		binary.BigEndian.PutUint64(frame[0:8], run)
		binary.BigEndian.PutUint64(frame[8:16], tick)
		binary.BigEndian.PutUint32(frame[16:20], uint32(i))
		binary.BigEndian.PutUint32(frame[20:24], uint32(count))
		binary.BigEndian.PutUint32(frame[24:28], uint32(len(body)))
		copy(frame[SnapshotChunkHeader:], body[lo:hi])
		out = append(out, frame)
	}
	return out, nil
}

// SnapshotAssembly reassembles a chunked capture or correction; the zero value is
// ready. A join's capture comes on one stream, so a chunk out of order is a confused
// sender (Add). A correction comes over a mesh by several paths (AddChunk): a chunk
// taken already, one of a tick assembled whole, and a newer transfer, which supersedes,
// are expected. Transfers order by run, then tick: a reset restarts the tick count.
type SnapshotAssembly struct {
	run     uint64
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

// AddChunk admits one chunk and reports whether it was new to this assembly and
// whether the transfer is complete. The first is what a relay forwards on, as the
// artifact flood terminates: a copy arriving by another path is neither taken nor
// forwarded again, so each node handles each chunk once whatever the topology.
func (s *SnapshotAssembly) AddChunk(frame []byte) (admitted, done bool, err error) {
	if len(frame) < SnapshotChunkHeader {
		return false, false, fmt.Errorf("snapshot chunk: %d bytes, want at least %d", len(frame), SnapshotChunkHeader)
	}
	run := binary.BigEndian.Uint64(frame[0:8])
	tick := binary.BigEndian.Uint64(frame[8:16])
	index := binary.BigEndian.Uint32(frame[16:20])
	count := binary.BigEndian.Uint32(frame[20:24])
	total := binary.BigEndian.Uint32(frame[24:28])
	payload := frame[SnapshotChunkHeader:]
	newer := run > s.run || (run == s.run && tick > s.tick)
	older := run < s.run || (run == s.run && tick < s.tick)

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
	if s.started && newer {
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
		s.run, s.tick, s.count, s.total, s.started = run, tick, count, total, true
		s.body = make([]byte, 0, min(int(total), snapshotReserve))
		older = false
	}
	// Already taken, or moved past. Two paths can carry differently shaped bodies
	// for the same tick — a delta down one and the keyframe down another — and once
	// either is whole the other describes a world this node already holds.
	if older || (tick == s.tick && s.next == s.count) {
		return false, false, nil
	}
	if run != s.run || tick != s.tick || count != s.count || total != s.total {
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
