//go:build (linux || freebsd) && !vif_headless && !vif_nogui && !vif_noaudio

package main

import "testing"

// With no sound server answering, the pulse speakers open nothing: a nil
// interface, not a nil stream inside one, so the next speakers or a player is tried
func TestSpeakersAreNilWhereNoServerAnswers(t *testing.T) {
	t.Setenv("PULSE_SERVER", "unix:"+t.TempDir()+"/none")
	if s, err := pulseSpeakers(false); s != nil || err == nil {
		t.Fatalf("pulseSpeakers() = %T, %v with no server at $PULSE_SERVER, want nil and an error", s, err)
	}
}
