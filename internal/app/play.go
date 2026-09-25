//go:build !vif_headless

package app

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/vlog"
)

// panStep is the cells a pan key shifts the presented game area
const panStep = 4

// PlayJournal replays a recorded run on the terminal. Several paths reassemble a
// rotated set. The anchor names the scenario; roots says where to look for it, so
// a journal recorded against a scenario that is not installed replays under the
// same -config-dir the run used. The journal carries no mute state: muted is the
// viewer's, as -mute is the player's.
func PlayJournal(roots resource.Options, muted bool, paths ...string) error {
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
	cfg.Resources.Dir = roots.Dir
	cfg.AudioMuted = muted
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

	budget       time.Duration // wall time owed to the simulation
	step         int           // ticks granted while paused
	panX, panY   int           // view offset from the recorded one, as the map edges allow
	termW, termH int           // the viewer's terminal, which the recording need not fit
	paused       bool
	done         bool
	cmd          *viewerCommand // open while the viewer holds the command line or an overlay
	err          error          // the stream's own failure, returned by run
}

// viewerCommand is the recorded player's operator state a viewer's command line
// borrows, handed back when it closes: the mode the bar and ping bounds show, and
// the pause APM admission and the mixer follow.
type viewerCommand struct {
	mode   core.GameMode
	paused bool
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

	p.termW, p.termH = p.a.term.Size()
	p.a.ctx.SetPresentationSize(p.termW, p.termH)
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
				p.termW, p.termH = ev.Width, ev.Height
				p.a.orchestrator.Resize(ev.Width, ev.Height)
				p.a.ctx.SetPresentationSize(ev.Width, ev.Height)
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

// advance grants the simulation the ticks the elapsed wall time paid for. Nothing
// plays while the viewer holds the command line, as the game pauses for it.
func (p *player) advance(elapsed time.Duration) {
	if p.done || p.cmd != nil || p.paused {
		p.a.world.Resources.Prof.Hold() // a profiler window spans played time only
	}
	if p.done || p.cmd != nil {
		return
	}
	if p.paused {
		stepped := p.step > 0
		for p.step > 0 && p.tickOnce() {
			p.step--
		}
		if stepped {
			p.a.holdMixer(true) // a recorded unpause inside the step released it
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

// frame renders one presented frame, laid out for the viewer's terminal rather than
// the recorded one: the simulation keeps its geometry and only the view moves.
func (p *player) frame() {
	a := p.a
	a.ctx.IncrementFrameNumber()
	rc := a.renderContext()
	w := max(p.termW-rc.GameXOffset, 1)
	h := max(p.termH-rc.GameYOffset-parameter.BottomMargin, 1)
	rc.CameraX, rc.MapOffsetX, p.panX = viewAxis(rc.CameraX, rc.ViewportWidth, w, rc.MapWidth, p.panX)
	rc.CameraY, rc.MapOffsetY, p.panY = viewAxis(rc.CameraY, rc.ViewportHeight, h, rc.MapHeight, p.panY)
	rc.ViewportWidth, rc.ViewportHeight = w, h
	rc.ScreenWidth, rc.ScreenHeight = p.termW, p.termH
	a.orchestrator.RenderFrame(rc, a.world)
}

// viewAxis places the viewer's view on one axis as the game would at its size: a
// map the view holds is centred and does not scroll; a larger one is shown from the
// recorded view's centre, moved by pan and stopped at the map's edges. The pan kept
// is what the edges allowed, so a key back the other way moves the view at once.
func viewAxis(camera, recorded, size, mapSize, pan int) (cam, offset, kept int) {
	if mapSize <= size {
		return 0, (size - mapSize) / 2, 0
	}
	from := camera + (recorded-size)/2
	cam = max(0, min(from+pan, mapSize-size))
	return cam, 0, cam - from
}

// key applies one viewer key; false quits. Playback bindings are fixed rather than
// routed through the keymap: these drive the viewer, not the game. Any other key is
// offered to the keymap for the game bindings a viewer owns.
func (p *player) key(ev terminal.Event) bool {
	if p.cmd != nil {
		// The viewer's command line or overlay, parsed as the game parses it
		if intent := p.a.inputMachine.Process(ev); intent != nil {
			return p.command(intent)
		}
		return true
	}
	if ev.Key != terminal.KeyRune {
		return p.offer(ev)
	}
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
	case ' ', '.', '+', '=', '-', '_':
		// Pause, step and rate are instance-local. A participant cannot stop or slow
		// only its own copy of a live session, so the viewer keeps pan and quit.
		if p.live {
			return true
		}
		p.control(ev.Rune)
	default:
		return p.offer(ev)
	}
	p.report()
	return true
}

// control applies one pause, step or rate key
func (p *player) control(r rune) {
	switch r {
	case ' ':
		p.paused = !p.paused
		p.budget = 0
		p.a.holdMixer(p.paused)
	case '.':
		p.paused, p.step = true, p.step+1
		p.a.holdMixer(true)
	case '+', '=':
		p.scale = engine.ScaleStep(p.scale, 1)
	case '-', '_':
		p.scale = engine.ScaleStep(p.scale, -1)
	}
}

// offer parses a key as the game would, in NORMAL, and keeps only what a viewer
// owns: its speakers, its exit and, off a live session, the command line.
// Everything else the keymap makes of a key is the recorded player's to do.
func (p *player) offer(ev terminal.Event) bool {
	p.a.inputMachine.SetMode(input.ModeNormal)
	intent := p.a.inputMachine.Process(ev)
	if intent == nil {
		return true
	}
	switch intent.Type {
	case input.IntentQuit:
		return false
	case input.IntentToggleAudioCycle:
		return p.route(intent)
	case input.IntentModeSwitch:
		if intent.ModeTarget == input.ModeTargetCommand && !p.live {
			a := p.a
			p.cmd = &viewerCommand{mode: a.ctx.GetMode(), paused: a.ctx.TimeCtl.IsPaused()}
			a.ctx.Viewer.Store(true)
			return p.command(intent)
		}
	}
	return true
}

// command routes one intent of the viewer's command session and hands the borrowed
// state back once the router has returned to NORMAL, so playback resumes in the
// mode and pause it recorded.
func (p *player) command(intent *input.Intent) bool {
	a := p.a
	if !p.route(intent) {
		return false
	}
	if a.ctx.IsCommandMode() || a.ctx.IsOverlayMode() {
		return true
	}
	c := p.cmd
	p.cmd = nil
	a.ctx.Viewer.Store(false)
	a.world.RunSafe(func() {
		if a.ctx.GetMode() != c.mode {
			a.ctx.RequestMode(c.mode)
		}
		if a.ctx.TimeCtl.IsPaused() != c.paused {
			a.ctx.SetPaused(c.paused)
		}
	})
	a.Settle()
	a.holdMixer(p.paused)
	return true
}

// route applies one viewer intent through the game's router and settles it at
// once, since nothing else dispatches between a replay's ticks. The viewer is
// out-of-band control rather than the recorded player, so none of it is effort.
func (p *player) route(intent *input.Intent) bool {
	a := p.a
	cont := true
	a.world.RunSafe(func() {
		a.world.WithOrigin(event.OriginDebug, func() { cont = a.router.Handle(intent) })
	})
	a.Settle()
	return cont
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
	// The keys are on :help rather than on a bar the recording's messages share
	keys := ":h for keys"
	if p.live {
		state, keys = "LIVE", "hjkl 0 q"
	}
	p.a.ctx.SetStatusMessage(fmt.Sprintf("%s %sx | %s | %s",
		state, p.scale.String(), p.src.progress(), keys), 0, true)
}
