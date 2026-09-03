// Presented playback. One loop presents any driven stream on a live terminal: a
// recorded journal, or an authored script. The simulation runs at the stream's own
// geometry and is advanced only by that stream's driver, so the world is
// bit-identical to the headless form and the terminal supplies pacing, pan and
// playback control only.

package app

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/journal"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
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
	d, err := newReplayDriver(a, set.Records)
	if err != nil {
		return err
	}
	vlog.Info("app", "msg", "replay open",
		"records", len(set.Records), "seed", an.Seed, "speed", an.Speed)
	return (&player{a: a, src: journalSource{d}, interval: time.Duration(an.TickInterval),
		rec: parseSpeed(an.Speed), scale: engine.ScaleNormal}).run()
}

// runPresentedScript presents an authored run. Pacing is the script's own: the
// resolved interval is one tick's wall budget, and an unpaced run is presented as
// fast as the frame loop can render it.
func runPresentedScript(a *App, d *journal.ScriptDriver, path string,
	interval time.Duration, paced bool, signals <-chan os.Signal) (journal.ScriptStats, error) {

	live := a.sessionTransport() != nil
	if !paced {
		interval = time.Millisecond // the floor perTick already clamps to
	}
	vlog.Info("app", "msg", "script open", "path", path, "paced", paced, "live", live)
	p := &player{
		a: a, src: scriptSource{d}, interval: interval,
		rec: engine.ScaleNormal, scale: engine.ScaleNormal,
		live: live, signals: signals,
	}
	err := p.run()
	return reportScript(d, path), err
}

// pacedSource is the driven stream a presentation loop advances. Both drivers
// report their own counters, because "how far through" means a different thing to
// a record stream than to an action list.
type pacedSource interface {
	Step() (bool, error)
	progress() string
}

// journalSource adapts a record stream to the presentation loop.
type journalSource struct{ d *journal.ReplayDriver }

func (s journalSource) Step() (bool, error) { return s.d.Step() }

func (s journalSource) progress() string {
	st := s.d.Stats()
	return fmt.Sprintf("run %d tick %d | %d/%d rec", st.End.Run, st.End.Tick, st.Injected, st.Records)
}

type scriptSource struct{ d *journal.ScriptDriver }

func (s scriptSource) Step() (bool, error) { return s.d.Step() }

func (s scriptSource) progress() string {
	st := s.d.Stats()
	return fmt.Sprintf("run %d tick %d | %d/%d act", st.End.Run, st.End.Tick, st.Executed, st.Actions)
}

// parseSpeed resolves the recorded rate, defaulting to real time
func parseSpeed(tok string) engine.TimeScale {
	if s, ok := engine.ParseScale(tok); ok {
		return s
	}
	return engine.ScaleNormal
}

// player paces a driven stream against wall time and presents each frame
type player struct {
	a   *App
	src pacedSource

	interval time.Duration    // stream game time per tick
	rec      engine.TimeScale // rate the stream was produced at
	scale    engine.TimeScale // viewer rate, relative to the produced one

	// live marks a run attached to a session. The playback controls are refused on
	// one: they are instance-local, and half a session cannot be paused.
	live    bool
	signals <-chan os.Signal

	budget     time.Duration // wall time owed to the simulation
	step       int           // ticks granted while paused
	panX, panY int
	paused     bool
	done       bool
	err        error // the stream's own failure, returned by run
}

// run drives the presentation loop until the viewer quits. NewReplay has already
// started the replay's services through newDriven.
func (p *player) run() error {
	frameTicker := time.NewTicker(parameter.FrameUpdateInterval)
	defer frameTicker.Stop()

	events := p.a.termSvc.Events()
	sigChan := p.signals
	if sigChan == nil {
		var stopSignals func()
		sigChan, stopSignals = notifySignals()
		defer stopSignals()
	}

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
			if p.done && p.err != nil {
				return p.err
			}
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
	more, err := p.src.Step()
	if err != nil {
		vlog.Error("app", "msg", "presented run failed", "error", err.Error())
		p.done = true
		p.err = err
		p.a.ctx.SetStatusMessage("ERROR: "+err.Error(), 0, true)
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
	renderCtx := a.renderContext()
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
	if p.live {
		// Pause, step and rate are instance-local. A participant cannot stop or slow
		// only its own copy of a live session, so the viewer keeps pan and quit.
		switch ev.Rune {
		case 'q':
			return false
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
	state := "PLAY"
	switch {
	case p.done:
		state = "END"
	case p.paused:
		state = "PAUSE"
	}
	keys := "SPACE . +- hjkl 0 q"
	if p.live {
		state, keys = "LIVE", "hjkl 0 q"
	}
	p.a.ctx.SetStatusMessage(fmt.Sprintf("%s %sx | %s | %s",
		state, p.scale.String(), p.src.progress(), keys), 0, true)
}
