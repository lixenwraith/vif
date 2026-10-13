package event

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/core"
)

func TestEnvironmentEventsAreSharedAndTyped(t *testing.T) {
	EnsureRegistry()
	for _, tc := range []struct {
		eventType EventType
		payload   any
	}{
		{EventWindStart, &WindStartPayload{}},
		{EventWindCancel, nil},
		{EventDriftStart, &DriftStartPayload{}},
		{EventDriftCancel, nil},
	} {
		if got := ClassOf(tc.eventType); got != ClassShared {
			t.Fatalf("event %d class = %s, want shared", tc.eventType, got)
		}
		if !Replicated(tc.eventType, core.DomainShared) {
			t.Fatalf("event %d must be journal-replicated", tc.eventType)
		}
		if OnWire(GameEvent{Type: tc.eventType, Domain: core.DomainShared}) {
			t.Fatalf("event %d must be re-derived, not transported", tc.eventType)
		}
		if got := NewPayloadStruct(tc.eventType); reflect.TypeOf(got) != reflect.TypeOf(tc.payload) {
			t.Fatalf("event %d payload prototype = %T, want %T", tc.eventType, got, tc.payload)
		}
	}
}

func TestWindStartPayloadFrameRoundTrip(t *testing.T) {
	EnsureRegistry()
	want := &WindStartPayload{Force: 42.5, Direction: 2.25, Duration: 1750 * time.Millisecond}
	frame, encodeErr := NewWireFrame(GameEvent{
		Type: EventWindStart, Payload: want, Domain: core.DomainShared,
	})
	if encodeErr != "" {
		t.Fatalf("encode: %s", encodeErr)
	}
	et, payload, domain, err := frame.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if et != EventWindStart || domain != core.DomainShared || !reflect.DeepEqual(payload, want) {
		t.Fatalf("round trip = (%v, %#v, %v), want (%v, %#v, %v)",
			et, payload, domain, EventWindStart, want, core.DomainShared)
	}
}
