package main

import "testing"

// TestParseHealthReadsTheBodyTheProbeWrites pins the shape rather than an imagined
// one: the reason is free text at the end of the first line, and it appears exactly
// when the session is not ready — which is when the website most needs the record.
func TestParseHealthReadsTheBodyTheProbeWrites(t *testing.T) {
	input := []byte("live=true ready=false reason=session at capacity\n" +
		"address=:7777\ncapacity=1\nclock=running\nexpires_in=1m12s\n" +
		"guests=1\nphase=occupied\ntick=228\n")
	got, err := parseHealth(input)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Live || got.Ready || got.Capacity != 1 || got.Clock != "running" ||
		got.ExpiresIn != "1m12s" || got.Guests != 1 || got.Phase != "occupied" ||
		got.Tick != 228 || got.Reason != "session at capacity" {
		t.Fatalf("unexpected health: %+v", got)
	}
}

func TestParseHealthRequiresLiveAndReady(t *testing.T) {
	if _, err := parseHealth([]byte("phase=waiting\n")); err == nil {
		t.Fatal("expected missing required fields to fail")
	}
}

func TestParseHealthRejectsMalformedValues(t *testing.T) {
	if _, err := parseHealth([]byte("live=yes\nready=true\n")); err == nil {
		t.Fatal("expected malformed boolean to fail")
	}
}
