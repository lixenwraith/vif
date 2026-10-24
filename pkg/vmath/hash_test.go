package vmath

import "testing"

// TestHash64IsFixedAndFoldsSequencesApart: the sum of a given input is a promise
// every platform and process keeps, bytes and a string fold alike, and two
// sequences folded in turn differ from their concatenation.
func TestHash64IsFixedAndFoldsSequencesApart(t *testing.T) {
	const want uint64 = 0xdcf2559ee7d0f0c9
	if got := NewHash64().String("vif/manifest/row/1\x00").Word(7).Bytes([]byte("a row's bytes")).Sum(); got != want {
		t.Fatalf("sum %#x, want %#x", got, want)
	}
	long := "0123456789abcdef0"
	if NewHash64().String(long) != NewHash64().Bytes([]byte(long)) {
		t.Fatal("a string folds unlike its bytes")
	}
	if NewHash64().Bytes([]byte("ab")).Bytes([]byte("c")).Sum() == NewHash64().Bytes([]byte("abc")).Sum() {
		t.Fatal("a boundary between two sequences folds like none")
	}
	if NewHash64().Word(0).Sum() == NewHash64().Sum() || NewHash64().Bytes(nil).Sum() == NewHash64().Sum() {
		t.Fatal("an empty fold leaves the hash as it was")
	}
}

var hashRow = []byte(`{"x":117,"y":42,"precise_x":117.21875,"precise_y":42.5,"vel_x":-0.75,"vel_y":0.125,"mass":1.5,"radius":0.5,"force_x":0,"force_y":0,"drag":0.02,"max_speed":4,"bounce":0.3,"phase":17}`)

func BenchmarkHash64Row(b *testing.B) {
	b.SetBytes(int64(len(hashRow)))
	var sink uint64
	for b.Loop() {
		sink += NewHash64().Bytes(hashRow).Sum()
	}
	sinkI = int64(sink)
}

func BenchmarkFNVRow(b *testing.B) {
	b.SetBytes(int64(len(hashRow)))
	var sink uint64
	for b.Loop() {
		h := FNVOffset64
		for _, c := range hashRow {
			h ^= uint64(c)
			h *= FNVPrime64
		}
		sink += h
	}
	sinkI = int64(sink)
}
