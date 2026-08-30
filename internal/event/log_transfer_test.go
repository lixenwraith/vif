package event_test

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
)

// TestSessionLogSplitsAndRoundTrips pins the catch-up transfer. A log is unbounded
// and a framed message is not, so the split has to be lossless in both directions:
// a record dropped or reordered here is a replay that reproduces a different world.
func TestSessionLogSplitsAndRoundTrips(t *testing.T) {
	event.EnsureRegistry()

	const count = 500
	want := make([]event.JournalRecord, count)
	for i := range want {
		want[i] = event.JournalRecord{
			Type:   event.EventCursorMoveRequest,
			Origin: event.OriginInput,
			Domain: core.DomainPlayer,
			Payload: "entity = 42\nx = " + string(rune('0'+i%10)) +
				"\ny = 7\n",
			JSeq: uint64(i), Seq: uint64(i * 3),
			Run: 0, Tick: uint64(i / 2), Boundary: uint64(i % 2),
		}
	}

	chunks, err := event.EncodeSessionLog(want, 4096)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("500 records fit in %d chunk(s) of 4 KiB; the split is not being exercised", len(chunks))
	}

	var got []event.JournalRecord
	for i, body := range chunks {
		if len(body) > 4096 {
			t.Fatalf("chunk %d is %d bytes, over its 4096-byte budget", i, len(body))
		}
		chunk, records, err := event.DecodeSessionLogChunk(body)
		if err != nil {
			t.Fatalf("decode chunk %d: %v", i, err)
		}
		if int(chunk.Seq) != i || int(chunk.Total) != len(chunks) {
			t.Fatalf("chunk %d reports seq %d of %d, want %d of %d", i, chunk.Seq, chunk.Total, i, len(chunks))
		}
		if final := i == len(chunks)-1; chunk.Final != final {
			t.Fatalf("chunk %d final = %t, want %t", i, chunk.Final, final)
		}
		got = append(got, records...)
	}

	if len(got) != len(want) {
		t.Fatalf("round trip returned %d records, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("record %d round tripped as %#v, want %#v", i, got[i], want[i])
		}
	}
}

// TestSessionLogChunksFitOneFrame keeps the split tied to what the transport can
// actually carry: the header's length field is 16 bits, so a chunk plus its header
// has to stay inside that.
func TestSessionLogChunksFitOneFrame(t *testing.T) {
	event.EnsureRegistry()

	records := make([]event.JournalRecord, 4000)
	for i := range records {
		records[i] = event.JournalRecord{
			Type: event.EventCursorMoveRequest, Origin: event.OriginSystem,
			Domain: core.DomainShared, JSeq: uint64(i), Tick: uint64(i),
		}
	}
	budget := network.MaxPayloadSize - network.HeaderSize
	chunks, err := event.EncodeSessionLog(records, budget)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	for i, body := range chunks {
		if len(body)+network.HeaderSize > network.MaxPayloadSize {
			t.Fatalf("chunk %d frames to %d bytes, over the wire maximum", i, len(body)+network.HeaderSize)
		}
	}

	// An empty log still produces one chunk: the joiner has to be told the transfer
	// finished, and "no records" is an answer rather than an absence of one.
	empty, err := event.EncodeSessionLog(nil, budget)
	if err != nil {
		t.Fatalf("encode empty: %v", err)
	}
	if len(empty) != 1 {
		t.Fatalf("empty log encoded to %d chunks, want 1", len(empty))
	}
	chunk, records, err := event.DecodeSessionLogChunk(empty[0])
	if err != nil || !chunk.Final || len(records) != 0 {
		t.Fatalf("empty chunk = %#v, %d records, err %v; want one final empty chunk", chunk, len(records), err)
	}
}
