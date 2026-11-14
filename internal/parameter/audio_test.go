//go:build !vif_headless && !vif_noaudio

package parameter

import (
	"testing"
	"time"

	"github.com/lixenwraith/audio"
)

// TestAudioBufferBoundsAreTheMixers: [audio] buffer_ms takes the periods the mixer
// runs at, which a build without audio cannot ask it, so the bounds kept here are
// the mixer's own.
func TestAudioBufferBoundsAreTheMixers(t *testing.T) {
	for _, c := range []struct {
		name  string
		ms    int
		mixer time.Duration
	}{
		{"min", AudioBufferMsMin, audio.AudioBufferMin},
		{"max", AudioBufferMsMax, audio.AudioBufferMax},
		{"step", AudioBufferMsStep, audio.AudioBufferStep},
	} {
		if time.Duration(c.ms)*time.Millisecond != c.mixer {
			t.Errorf("AudioBufferMs %s is %d ms, the mixer's %v", c.name, c.ms, c.mixer)
		}
	}
}
