package network

import (
	"encoding/binary"
	"testing"
)

// chunkHeaderFor builds a chunk header that declares a transfer of total bytes and
// carries payload of its own. It is written by hand rather than through
// EncodeSnapshotChunks because the point is a chunk whose declared length and
// delivered length disagree, which the encoder cannot produce.
func chunkHeaderFor(tick uint64, index, count, total uint32, payload []byte) []byte {
	frame := make([]byte, SnapshotChunkHeader+len(payload))
	binary.BigEndian.PutUint64(frame[0:8], tick)
	binary.BigEndian.PutUint32(frame[8:12], index)
	binary.BigEndian.PutUint32(frame[12:16], count)
	binary.BigEndian.PutUint32(frame[16:20], total)
	copy(frame[SnapshotChunkHeader:], payload)
	return frame
}

// TestAssemblyAllocatesForBytesThatArrived is the defence, and it is a property of
// the receiver rather than of the ceiling.
//
// A declared length is a claim by whoever sent the chunk. Reserving for it let one
// twenty-byte header make a receiver hold the whole ceiling, once per source. What
// bounds it now is that the buffer grows with the bytes that actually arrive, so a
// peer must send what it wants a receiver to hold.
func TestAssemblyAllocatesForBytesThatArrived(t *testing.T) {
	t.Parallel()
	var asm SnapshotAssembly

	// One first chunk of a transfer declaring the largest body the protocol allows.
	admitted, done, err := asm.AddChunk(chunkHeaderFor(9, 0, 4096, MaxSnapshotBytes, []byte("first")))
	if err != nil || !admitted || done {
		t.Fatalf("first chunk: admitted=%v done=%v err=%v", admitted, done, err)
	}
	if got := cap(asm.body); got > snapshotReserve {
		t.Fatalf("reserved %d bytes for %d bytes of payload; the reservation ceiling is %d",
			got, len("first"), snapshotReserve)
	}
}

// TestAssemblyRefusesABodyBeyondTheCeiling keeps the sanity bound honest. It is
// three orders of magnitude above a measured capture of this world, so reaching it
// is not a large world, it is a number.
func TestAssemblyRefusesABodyBeyondTheCeiling(t *testing.T) {
	t.Parallel()
	var asm SnapshotAssembly
	if _, _, err := asm.AddChunk(chunkHeaderFor(9, 0, 1, MaxSnapshotBytes+1, []byte("x"))); err == nil {
		t.Fatal("a body past the ceiling was admitted")
	}
}

// TestAssemblyStillReassemblesARealTransfer is the non-vacuous half: a bound that
// broke an ordinary capture would be worse than the thing it was defending against.
func TestAssemblyStillReassemblesARealTransfer(t *testing.T) {
	t.Parallel()
	body := make([]byte, 3*SnapshotChunkBody+17)
	for i := range body {
		body[i] = byte(i)
	}
	chunks, err := EncodeSnapshotChunks(42, body)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var asm SnapshotAssembly
	for i, c := range chunks {
		done, err := asm.Add(c)
		if err != nil {
			t.Fatalf("chunk %d: %v", i, err)
		}
		if done != (i == len(chunks)-1) {
			t.Fatalf("chunk %d reported done=%v", i, done)
		}
	}
	tick, got := asm.Result()
	if tick != 42 || string(got) != string(body) {
		t.Fatalf("reassembled tick %d and %d bytes, want 42 and %d", tick, len(got), len(body))
	}
}
