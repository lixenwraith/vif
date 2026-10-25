package main

import "testing"

// TestParseHealthReadsTheBodyTheProbeWrites pins the shape rather than an imagined
// one: the reason is free text at the end of the first line, and it appears exactly
// when the session is not ready — which is when the website most needs the record.
// A release's build is free text too.
func TestParseHealthReadsTheBodyTheProbeWrites(t *testing.T) {
	input := []byte("live=true ready=false reason=session at capacity\n" +
		"address=:7777\nbots=1\nbuild=v0.1.0 (1154b8a6)\ncapacity=2\nclock=running\nexpires_in=1m12s\n" +
		"guests=2\nmap=500x250\nphase=occupied\ntick=228\n")
	got, err := parseHealth(input)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Live || got.Ready || got.Capacity != 2 || got.Clock != "running" ||
		got.ExpiresIn != "1m12s" || got.Guests != 2 || got.Bots != 1 || got.Phase != "occupied" ||
		got.Tick != 228 || got.Map != "500x250" || got.Reason != "session at capacity" || got.Build != "v0.1.0 (1154b8a6)" {
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
