//go:build !vif_headless

package app

import (
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
)

// TestFrameFloorNamesMultiplexers: tmux's own variable, or a TERM that names tmux
// or screen (as one reached over ssh does), floors the terminal's frames; any
// other terminal draws at the run's rate.
func TestFrameFloorNamesMultiplexers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		env  map[string]string
		want time.Duration
	}{
		{"tmux", map[string]string{"TMUX": "/tmp/tmux-1000/default,1234,0", "TERM": "xterm-256color"}, parameter.MultiplexerFrameInterval},
		{"tmux over ssh", map[string]string{"TERM": "tmux-256color"}, parameter.MultiplexerFrameInterval},
		{"screen", map[string]string{"TERM": "screen-256color"}, parameter.MultiplexerFrameInterval},
		{"alacritty", map[string]string{"TERM": "alacritty"}, 0},
		{"xterm", map[string]string{"TERM": "xterm-256color"}, 0},
		{"no TERM", map[string]string{}, 0},
	}
	for _, c := range cases {
		if got := frameFloor(func(k string) string { return c.env[k] }); got != c.want {
			t.Errorf("%s: floor %v, want %v", c.name, got, c.want)
		}
	}
}

// flooredPresentation is a screen that keeps up with frames no faster than floor
type flooredPresentation struct {
	*framePresentation
	floor time.Duration
}

func (p flooredPresentation) FrameFloor() time.Duration { return p.floor }

// TestFrameIntervalHoldsToThePresentationsFloor: a presentation's floor slows the
// frame loop, but never past a frame a tick, which a tick at real time waits for;
// one without a floor leaves the context's interval.
func TestFrameIntervalHoldsToThePresentationsFloor(t *testing.T) {
	t.Parallel()
	interval := func(pres Presentation, tick time.Duration) time.Duration {
		a, err := newSessionApp(Config{Mode: ModePlay, Resources: resource.Options{Embedded: true},
			Host: Host{Presentation: pres}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(a.Close)
		a.ctx.SetTickInterval(tick)
		return a.frameInterval()
	}
	tick := parameter.GameUpdateInterval
	if got := interval(&framePresentation{w: 120, h: 40}, tick); got != parameter.FrameUpdateInterval {
		t.Errorf("unfloored: frames every %v, want %v", got, parameter.FrameUpdateInterval)
	}
	floored := flooredPresentation{&framePresentation{w: 120, h: 40}, parameter.MultiplexerFrameInterval}
	if got := interval(floored, tick); got != parameter.MultiplexerFrameInterval {
		t.Errorf("floored: frames every %v, want %v", got, parameter.MultiplexerFrameInterval)
	}
	short := parameter.TickIntervalMin
	if got := interval(floored, short); got != short {
		t.Errorf("floored at a %v tick: frames every %v, want one a tick", short, got)
	}
}
