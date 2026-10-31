//go:build !vif_headless

package app

import (
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/paths"
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

// TestTmuxFrameCapTurnsTheFloorOff: inside tmux the terminal keeps the floor
// unless the run's [presentation] tmux_frame_cap is false, which the embedded
// vif.toml leaves true.
func TestTmuxFrameCapTurnsTheFloorOff(t *testing.T) {
	t.Setenv(multiplexerEnv, "/tmp/tmux-1000/default,1234,0")
	if s, err := paths.ParseSettings(nil); err != nil || !s.Presentation.TmuxFrameCap {
		t.Fatalf("embedded tmux_frame_cap = %v, %v; want true", s.Presentation.TmuxFrameCap, err)
	}
	if got := newTerminal(Config{}).FrameFloor(); got != parameter.MultiplexerFrameInterval {
		t.Errorf("capped: floor %v, want %v", got, parameter.MultiplexerFrameInterval)
	}
	if got := newTerminal(Config{NoTmuxFrameCap: true}).FrameFloor(); got != 0 {
		t.Errorf("uncapped: floor %v, want none", got)
	}
}

// flooredPresentation is a screen that keeps up with frames no faster than floor
type flooredPresentation struct {
	*framePresentation
	floor time.Duration
}

func (p flooredPresentation) FrameFloor() time.Duration { return p.floor }

// TestFrameIntervalHoldsToThePresentationsFloor: a presentation's floor slows the
// frame loop, and the player's max_fps takes its place, faster or slower; neither
// draws faster than the context's interval or slower than a frame a tick, which a
// tick at real time waits for.
func TestFrameIntervalHoldsToThePresentationsFloor(t *testing.T) {
	t.Parallel()
	interval := func(pres Presentation, tick time.Duration, fps int32) time.Duration {
		a, err := newSessionApp(Config{Mode: ModePlay, Resources: resource.Options{Embedded: true},
			Host: Host{Presentation: pres}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(a.Close)
		a.ctx.SetTickInterval(tick)
		a.ctx.MaxFPS.Store(fps)
		return a.frameInterval()
	}
	tick, short := parameter.GameUpdateInterval, parameter.TickIntervalMin
	plain := &framePresentation{w: 120, h: 40}
	floored := flooredPresentation{&framePresentation{w: 120, h: 40}, parameter.MultiplexerFrameInterval}
	for _, c := range []struct {
		name string
		pres Presentation
		tick time.Duration
		fps  int32
		want time.Duration
	}{
		{"unfloored", plain, tick, 0, parameter.FrameUpdateInterval},
		{"floored", floored, tick, 0, parameter.MultiplexerFrameInterval},
		{"floored at a short tick", floored, short, 0, short},
		{"max fps 30", plain, tick, 30, time.Second / 30},
		{"max fps 60 over the floor", floored, tick, 60, parameter.FrameUpdateInterval},
		{"max fps past the run's rate", plain, tick, int32(parameter.FrameRateMax), parameter.FrameUpdateInterval},
		{"max fps under a frame a tick", plain, tick, int32(parameter.FrameRateMin), tick},
	} {
		if got := interval(c.pres, c.tick, c.fps); got != c.want {
			t.Errorf("%s: frames every %v, want %v", c.name, got, c.want)
		}
	}
}
