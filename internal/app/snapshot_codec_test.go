package app

import (
	"encoding/binary"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/network"
)

func TestSnapshotWireEnvelopeRoundTripsAndIsBounded(t *testing.T) {
	type sample struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	want := sample{Name: "storm", Count: 492}
	body, err := encodeSnapshotJSON(want)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) < snapshotWireHeader || string(body[:4]) != string(snapshotWireMagic[:]) {
		t.Fatalf("wire envelope = %x", body)
	}
	var got sample
	if err := decodeSnapshotJSON(body, &got); err != nil {
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
			binary.BigEndian.PutUint32(b[6:10], network.MaxSnapshotBytes+1)
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			bad := append([]byte(nil), body...)
			mutate.fn(bad)
			if err := decodeSnapshotJSON(bad, &got); err == nil {
				t.Fatal("corrupt envelope decoded")
			}
		})
	}
	if err := decodeSnapshotJSON(body[:len(body)-1], &got); err == nil {
		t.Fatal("truncated envelope decoded")
	}
}
