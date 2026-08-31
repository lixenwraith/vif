// Package app: journal playback.
//
// Presents a recorded run on a live terminal. The simulation runs at recorded
// geometry and is advanced only by ReplayDriver, so the world is bit-identical to
// a headless replay; the terminal supplies pacing, pan and playback control only.
// A recording wider than the terminal is clipped by the render buffer today; the
// pan offset is the seam a windowed composite replaces.
package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/journal"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// panStep is the cells a pan key shifts the presented game area
const panStep = 4

// PlayJournal replays a recorded run on the terminal. Several paths reassemble a
// rotated set.
func PlayJournal(paths ...string) error {
	event.EnsureRegistry()

	set, err := journal.Load(paths...)
	if err != nil {
		return err
	}
	if len(set.Anchors) == 0 {
		return errors.New("journal carries no anchor")
	}
	if err := set.CheckDense(); err != nil {
		vlog.Warn("app", "msg", "journal incomplete", "error", err.Error())
	}
	an := set.Anchors[0]

	cfg, err := ConfigFromAnchor(an)
	if err != nil {
		return err
	}
	cfg.AudioMuted = false // the anchor carries no mute state; a viewer wants sound
	a, err := NewReplay(cfg)
	if err != nil {
		return err
	}
	defer func() {
		if r := recover(); r != nil {
			core.HandleCrash(r) // does not return under unix
		}
		a.Close()
	}()

	if err := a.VerifyAnchor(an); err != nil {
		return err
	}
	d, err := NewReplayDriver(a, set.Records)
	if err != nil {
		return err
	}
	vlog.Info("app", "msg", "replay open",
		"records", len(set.Records), "seed", an.Seed, "speed", an.Speed)
	return (&player{a: a, d: d, interval: time.Duration(an.TickInterval),
		rec: parseSpeed(an.Speed), scale: engine.ScaleNormal}).run()
}

// parseSpeed resolves the recorded rate, defaulting to real time
func parseSpeed(tok string) engine.TimeScale {
	if s, ok := engine.ParseScale(tok); ok {
		return s
	}
	return engine.ScaleNormal
}

// player paces a ReplayDriver against wall time and presents each frame
type player struct {
	a *App
	d *ReplayDriver

	interval time.Duration    // recorded game time per tick
	rec      engine.TimeScale // rate the run was recorded at
	scale    engine.TimeScale // viewer rate, relative to the recorded one

	budget     time.Duration // wall time owed to the simulation
	step       int           // ticks granted while paused
	panX, panY int
	paused     bool
	done       bool
}

// run drives the presentation loop until the viewer quits. NewReplay has already
// started the replay's services through newDriven.
func (p *player) run() error {
	frameTicker := time.NewTicker(parameter.FrameUpdateInterval)
	defer frameTicker.Stop()

	events := p.a.termSvc.Events()
	sigChan, stopSignals := notifySignals()
	defer stopSignals()

	p.report()
	last := time.Now()

	for {
		select {
		case <-sigChan:
			return nil

		case ev := <-events:
			switch ev.Type {
			case terminal.EventResize:
				// Presentation only: the simulation keeps its recorded geometry
				p.a.orchestrator.Resize(ev.Width, ev.Height)
			case terminal.EventClosed, terminal.EventError:
				return nil
			case terminal.EventKey:
				if !p.key(ev) {
					return nil
				}
			}

		case now := <-frameTicker.C:
			p.advance(now.Sub(last))
			last = now
			p.frame()
		}
	}
}

// advance grants the simulation the ticks the elapsed wall time paid for
func (p *player) advance(elapsed time.Duration) {
	if p.done {
		return
	}
	if p.paused {
		for p.step > 0 && p.tickOnce() {
			p.step--
		}
		return
	}
	p.budget += elapsed
	per := p.perTick()
	for p.budget >= per {
		p.budget -= per
		if !p.tickOnce() {
			return
		}
	}
}

// perTick converts one recorded tick into the wall interval it should occupy
func (p *player) perTick() time.Duration {
	d := int64(p.interval) * p.rec.Den / p.rec.Num
	d = d * p.scale.Den / p.scale.Num
	return max(time.Duration(d), time.Millisecond)
}

// tickOnce advances the driver, latching the end of the stream
func (p *player) tickOnce() bool {
	more, err := p.d.Step()
	if err != nil {
		vlog.Error("app", "msg", "replay failed", "error", err.Error())
		p.done = true
		p.a.ctx.SetStatusMessage("REPLAY ERROR: "+err.Error(), 0, true)
		return false
	}
	if !more {
		p.done = true
		p.report()
		return false
	}
	return true
}

// frame renders one presented frame; the pan offset shifts the game area within
// the terminal without touching simulation geometry
func (p *player) frame() {
	a := p.a
	a.ctx.IncrementFrameNumber()

	var (
		snapTime         engine.TimeResource
		cursorX, cursorY int
		cursorValid      bool
		renderCtx        render.RenderContext
	)
	a.world.RunSafe(func() {
		snapTime.GameTime = a.ctx.TimeCtl.Now()
		snapTime.RealTime = a.ctx.TimeCtl.RealTime()
		if pos, ok := a.world.LocalCursor(); ok {
			cursorX, cursorY, cursorValid = pos.X, pos.Y, true
		}
		renderCtx = render.NewRenderContextFromGame(a.ctx, snapTime, cursorX, cursorY, cursorValid)
	})

	renderCtx.GameXOffset -= p.panX
	renderCtx.GameYOffset -= p.panY
	a.orchestrator.RenderFrame(renderCtx, a.world)
}

// key applies one playback control; false quits. Bindings are fixed rather than
// routed through the keymap: these drive the viewer, not the game.
func (p *player) key(ev terminal.Event) bool {
	if ev.Key != terminal.KeyRune {
		return true
	}
	switch ev.Rune {
	case 'q':
		return false
	case ' ':
		p.paused = !p.paused
		p.budget = 0
	case '.':
		p.paused, p.step = true, p.step+1
	case '+', '=':
		p.scale = engine.ScaleStep(p.scale, 1)
	case '-', '_':
		p.scale = engine.ScaleStep(p.scale, -1)
	case 'h':
		p.panX -= panStep
	case 'l':
		p.panX += panStep
	case 'k':
		p.panY -= panStep
	case 'j':
		p.panY += panStep
	case '0':
		p.panX, p.panY = 0, 0
	default:
		return true
	}
	p.report()
	return true
}

// report publishes playback state through the status bar the renderer already draws
func (p *player) report() {
	st := p.d.Stats()
	state := "PLAY"
	switch {
	case p.done:
		state = "END"
	case p.paused:
		state = "PAUSE"
	}
	p.a.ctx.SetStatusMessage(fmt.Sprintf(
		"%s %sx | run %d tick %d | %d/%d rec | SPACE . +- hjkl 0 q",
		state, p.scale.String(), st.End.Run, st.End.Tick, st.Injected, st.Records), 0, true)
}
