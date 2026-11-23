//go:build (linux || freebsd) && !vif_headless && !vif_nogui && !vif_noaudio

package main

import "testing"

// With no sound server answering, a window gets no speakers of its own: a nil
// interface, not a nil stream inside one, so the engine detects a backend as before
func TestSpeakersAreNilWhereNoServerAnswers(t *testing.T) {
	t.Setenv("PULSE_SERVER", "unix:"+t.TempDir()+"/none")
	if s := speakers(); s != nil {
		t.Fatalf("speakers() = %T with no server at $PULSE_SERVER, want nil", s)
	}
}
