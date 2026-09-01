package network

import (
	"bytes"
	"math/rand/v2"
	"testing"
)

// TestSnapshotChunksRoundTrip covers the split and the reassembly over the sizes
// that actually occur: one that fits in a frame, one that lands exactly on a
// boundary, and one the size of a storm's world.
func TestSnapshotChunksRoundTrip(t *testing.T) {
	sizes := []int{
		1,
		SnapshotChunkBody - 1,
		SnapshotChunkBody,
		SnapshotChunkBody + 1,
		3 * SnapshotChunkBody,
		// The measured storm high water is around 176 KiB, which is three frames.
		180 << 10,
	}
	for _, size := range sizes {
		body := make([]byte, size)
		r := rand.New(rand.NewPCG(uint64(size), 0x5EED))
		for i := range body {
			body[i] = byte(r.UintN(256))
		}

		chunks, err := EncodeSnapshotChunks(4242, body)
		if err != nil {
			t.Fatalf("%d bytes: encode: %v", size, err)
		}
		want := (size + SnapshotChunkBody - 1) / SnapshotChunkBody
		if len(chunks) != want {
			t.Fatalf("%d bytes: %d chunks, want %d", size, len(chunks), want)
		}
		for i, c := range chunks {
			if len(c) > MaxPayloadSize {
				t.Fatalf("%d bytes: chunk %d is %d bytes, the frame carries at most %d",
					size, i, len(c), MaxPayloadSize)
			}
		}

		var asm SnapshotAssembly
		for i, c := range chunks {
			done, err := asm.Add(c)
			if err != nil {
				t.Fatalf("%d bytes: chunk %d: %v", size, i, err)
			}
			if done != (i == len(chunks)-1) {
				t.Fatalf("%d bytes: chunk %d reported done=%t", size, i, done)
			}
		}
		tick, got := asm.Result()
		if tick != 4242 {
			t.Fatalf("%d bytes: reassembled for tick %d", size, tick)
		}
		if !bytes.Equal(got, body) {
			t.Fatalf("%d bytes: reassembled body differs", size)
		}
	}
}

// TestSnapshotAssemblyRefusesAConfusedTransfer pins what the chunk header is for.
// A world installed from a prefix, from two interleaved captures, or from frames
// that arrived out of order is a world that looks installed and is not, which is
// the failure mode the whole capture path exists to avoid.
func TestSnapshotAssemblyRefusesAConfusedTransfer(t *testing.T) {
	body := bytes.Repeat([]byte{0xAB}, 3*SnapshotChunkBody)
	chunks, err := EncodeSnapshotChunks(7, body)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	t.Run("out of order", func(t *testing.T) {
		var asm SnapshotAssembly
		if _, err := asm.Add(chunks[0]); err != nil {
			t.Fatalf("first chunk: %v", err)
		}
		if _, err := asm.Add(chunks[2]); err == nil {
			t.Fatal("a chunk that skipped its predecessor was admitted")
		}
	})

	t.Run("second transfer", func(t *testing.T) {
		other, err := EncodeSnapshotChunks(9, body)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		var asm SnapshotAssembly
		if _, err := asm.Add(chunks[0]); err != nil {
			t.Fatalf("first chunk: %v", err)
		}
		if _, err := asm.Add(other[1]); err == nil {
			t.Fatal("a chunk from another capture was admitted into this one")
		}
	})

	t.Run("truncated frame", func(t *testing.T) {
		var asm SnapshotAssembly
		if _, err := asm.Add(chunks[0][:SnapshotChunkHeader-1]); err == nil {
			t.Fatal("a frame shorter than the chunk header was admitted")
		}
	})

	t.Run("empty body", func(t *testing.T) {
		if _, err := EncodeSnapshotChunks(1, nil); err == nil {
			t.Fatal("an empty capture was encoded")
		}
	})
}
