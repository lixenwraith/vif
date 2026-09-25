package snapshot

import (
	"encoding/binary"
	"encoding/json"
	"slices"
	"testing"

	"github.com/lixenwraith/vif/internal/network"
)

func TestSnapshotWireEnvelopeRoundTripsAndIsBounded(t *testing.T) {
	type sample struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	want := sample{Name: "storm", Count: 492}
	body, err := EncodeJSON(want)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) < snapshotWireHeader || string(body[:4]) != string(snapshotWireMagic[:]) {
		t.Fatalf("wire envelope = %x", body)
	}
	var got sample
	if err := DecodeJSON(body, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("decoded = %#v, want %#v", got, want)
	}

	for _, mutate := range []struct {
		name string
		fn   func([]byte)
	}{
		{"magic", func(b []byte) { b[0] ^= 0xff }},
		{"version", func(b []byte) { b[4]++ }},
		{"codec", func(b []byte) { b[5]++ }},
		{"plain size", func(b []byte) {
			binary.BigEndian.PutUint32(b[6:10], network.MaxSnapshotPlainBytes+1)
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			bad := append([]byte(nil), body...)
			mutate.fn(bad)
			if err := DecodeJSON(bad, &got); err == nil {
				t.Fatal("corrupt envelope decoded")
			}
		})
	}
	if err := DecodeJSON(body[:len(body)-1], &got); err == nil {
		t.Fatal("truncated envelope decoded")
	}
}

// TestACaptureExpandingPastTheWireCeilingTravels pins the split between the two
// ceilings. A walled map's capture compresses better than 20:1, so its plain body
// is past what may travel while the bytes that travel are nowhere near it. One
// ceiling for both refuses the transfer at the encoder, before a byte is sent.
func TestACaptureExpandingPastTheWireCeilingTravels(t *testing.T) {
	type cell struct {
		Entity uint64 `json:"e"`
		Mask   int    `json:"m"`
		Rune   int    `json:"r"`
	}
	// Distinct entities, so the body is a real store rather than one value repeated.
	want := make([]cell, network.MaxSnapshotBytes/8)
	for i := range want {
		want[i] = cell{Entity: uint64(i + 1), Mask: 255, Rune: 9484}
	}
	plain, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) <= network.MaxSnapshotBytes {
		t.Fatalf("body is %d plain bytes; the test needs one past the %d-byte wire ceiling",
			len(plain), network.MaxSnapshotBytes)
	}

	body, err := EncodeJSON(want)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(body) > network.MaxSnapshotBytes {
		t.Fatalf("wire body is %d bytes, past the %d-byte ceiling", len(body), network.MaxSnapshotBytes)
	}
	var got []cell
	if err := DecodeJSON(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !slices.Equal(got, want) {
		t.Fatal("the decoded body is not the one encoded")
	}
}
