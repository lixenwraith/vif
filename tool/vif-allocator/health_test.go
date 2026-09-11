package main

import "testing"

func TestParseHealth(t *testing.T) {
	input := []byte("live=true ready=false\ncapacity=4\nclock=paused\nexpires_in=1m12s\nguests=0\nphase=vacant\ntick=239\nreason=session at capacity\n")
	got, err := parseHealth(input)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Live || got.Ready || got.Capacity != 4 || got.Clock != "paused" ||
		got.ExpiresIn != "1m12s" || got.Guests != 0 || got.Phase != "vacant" ||
		got.Tick != 239 || got.Reason != "session at capacity" {
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
