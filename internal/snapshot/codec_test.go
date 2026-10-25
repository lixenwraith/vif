package snapshot

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"

	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	link "github.com/lixenwraith/vif/pkg/network"
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
			binary.BigEndian.PutUint32(b[6:10], link.MaxSnapshotPlainBytes+1)
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
	want := make([]cell, link.MaxSnapshotBytes/8)
	for i := range want {
		want[i] = cell{Entity: uint64(i + 1), Mask: 255, Rune: 9484}
	}
	plain, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) <= link.MaxSnapshotBytes {
		t.Fatalf("body is %d plain bytes; the test needs one past the %d-byte wire ceiling",
			len(plain), link.MaxSnapshotBytes)
	}

	body, err := EncodeJSON(want)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(body) > link.MaxSnapshotBytes {
		t.Fatalf("wire body is %d bytes, past the %d-byte ceiling", len(body), link.MaxSnapshotBytes)
	}
	var got []cell
	if err := DecodeJSON(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !slices.Equal(got, want) {
		t.Fatal("the decoded body is not the one encoded")
	}
}

// TestACaptureTravelsAsSectionsFromCachedBlobs: a capture round-trips through the
// sections codec, a store from the blob size up travels as its own deflate and goes
// as the bytes made before while its rows hold, a store below it rides the grouped
// part, a store that moved is deflated again alone, and a damaged body is refused.
func TestACaptureTravelsAsSectionsFromCachedBlobs(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	var cap SharedCapture
	cap.Header.Tick = 7
	world := reflect.ValueOf(&cap.World).Elem()
	for i := range world.NumField() {
		f := world.Field(i)
		if f.Kind() != reflect.Slice {
			continue
		}
		n := 2
		if name := world.Type().Field(i).Name; name == "Positions" || name == "Wall" {
			n = parameter.SnapshotSectionBlobBytes // a row's columns are three bytes at least
		}
		f.Set(reflect.MakeSlice(f.Type(), n, n))
		for k := range n {
			f.Index(k).Field(0).SetUint(uint64(k + 1))
			fillLeaves(f.Index(k).Field(1), r)
		}
	}
	var enc Encoder
	first, err := enc.EncodeCapture(cap)
	if err != nil {
		t.Fatal(err)
	}
	if !isSections(first) {
		t.Fatalf("a capture left in codec %d, want the sections codec", first[5])
	}
	back, err := DecodeCapture(first)
	if got, want := mustJSON(back), mustJSON(cap); err != nil || string(got) != string(want) {
		t.Fatalf("the decoded capture differs from the one encoded (%v)", err)
	}
	wall, glyph := slices.Index(engine.SharedWorldStoreNames, "wall"), slices.Index(engine.SharedWorldStoreNames, "glyph")
	if enc.blobs[glyph].blob != nil {
		t.Fatal("a store below the blob size was deflated on its own")
	}
	walls, positions := &enc.blobs[wall].blob[0], &enc.blobs[0].blob[0]
	cap.Header.Tick = 8
	cap.World.Wall[0].Value.BlockMask ^= 1
	second, err := enc.EncodeCapture(cap)
	if err != nil {
		t.Fatal(err)
	}
	if &enc.blobs[wall].blob[0] == walls {
		t.Fatal("a moved store was sent from its old blob")
	}
	if &enc.blobs[0].blob[0] != positions {
		t.Fatal("an unchanged store was deflated again")
	}
	back, err = DecodeCapture(second)
	if got, want := mustJSON(back), mustJSON(cap); err != nil || string(got) != string(want) {
		t.Fatalf("the moved capture did not travel (%v)", err)
	}
	if third, _ := enc.EncodeCapture(cap); !bytes.Equal(second, third) {
		t.Fatal("an unchanged capture encoded to other bytes")
	}
	for _, mutate := range []struct {
		name string
		fn   func([]byte) []byte
	}{
		{"codec", func(b []byte) []byte { b[5] = snapshotCodecFlate; return b }},
		{"truncated", func(b []byte) []byte { return b[:len(b)-3] }},
		{"a part's plain size", func(b []byte) []byte { b[snapshotWireHeader+1+1+len(partRest)] += 1; return b }},
	} {
		bad := mutate.fn(append([]byte(nil), second...))
		if _, err := DecodeCapture(bad); err == nil {
			t.Fatalf("%s: a damaged body decoded", mutate.name)
		}
	}
}
