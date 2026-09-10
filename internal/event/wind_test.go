package event

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
)

func TestWindEventsAreSharedAndTyped(t *testing.T) {
	EnsureRegistry()
	if got := ClassOf(EventWindStart); got != ClassShared {
		t.Fatalf("EventWindStart class = %s, want shared", got)
	}
	if got := ClassOf(EventWindCancel); got != ClassShared {
		t.Fatalf("EventWindCancel class = %s, want shared", got)
	}
	for _, eventType := range []EventType{EventWindStart, EventWindCancel} {
		if !Replicated(eventType, core.DomainShared) {
			t.Fatalf("event %d must be journal-replicated", eventType)
		}
		if OnWire(GameEvent{Type: eventType, Domain: core.DomainShared}) {
			t.Fatalf("event %d must be re-derived, not transported", eventType)
		}
	}
	if got := NewPayloadStruct(EventWindStart); reflect.TypeOf(got) != reflect.TypeOf(&WindStartPayload{}) {
		t.Fatalf("EventWindStart payload prototype = %T, want *WindStartPayload", got)
	}
	if got := NewPayloadStruct(EventWindCancel); got != nil {
		t.Fatalf("EventWindCancel payload prototype = %T, want nil", got)
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
