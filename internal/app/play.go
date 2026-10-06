//go:build !vif_headless

package app

import (
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
	"github.com/lixenwraith/vif/internal/system"
	"github.com/lixenwraith/vif/internal/vlog"
)

// panStep is the cells a pan key shifts the presented game area
const panStep = 4

// PlayJournal replays a recorded run on the terminal; several paths reassemble a
// rotated set. The anchor decides everything simulated; viewer supplies what is the
// watcher's: where the scenario it names is found, as the run's -config-dir did,
// and the speakers, colour and music recording.
func PlayJournal(viewer Config, paths ...string) error {
	set, cfg, err := loadJournal(viewer, paths)
	if err != nil {
		return err
	}
	if err := set.CheckDense(); err != nil {
		vlog.Warn("journal", "msg", "journal incomplete", "error", err.Error())
	}
	an := set.Anchors[0]
	cfg.AudioMuted, cfg.AudioBackend, cfg.AudioBuffer = viewer.AudioMuted, viewer.AudioBackend, viewer.AudioBuffer
	cfg.MusicWAV, cfg.ColorMode, cfg.ColorModeSet = viewer.MusicWAV, viewer.ColorMode, viewer.ColorModeSet
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
	a.recordMusic()
	stream := set.Stream() // every copy replays this one
	d, err := newReplayDriver(a, stream)
	if err != nil {
		return err
	}
	a.log.Info("journal", "msg", "replay opened",
		"records", len(set.Records), "digests", len(set.Digests), "seed", an.Seed, "speed", an.Speed)
	shown := &replayCopy{a: a, src: journalSource{d, a.log}}
	p := &player{a: a, src: shown.src, interval: time.Duration(an.TickInterval),
		rec: parseSpeed(an.Speed), scale: engine.ScaleNormal}
	p.rw = newRewinder(shown, a, replayCopies(a, cfg, stream, NewReplay))
	a.ctx.ReplaySeek = p.seekLater
	defer p.rw.close()
	return p.run()
}

// replayCopies builds copies of lender's run for going back. A copy replays ticks
// the log already holds, so it writes once presented, and holds no flight recorder:
// the process has one, and adopt hands it to the copy presented.
func replayCopies(lender *App, cfg Config, stream *journal.Stream, newApp func(Config) (*App, error)) func() (*App, pacedSource, error) {
	return func() (*App, pacedSource, error) {
		rc := cfg
		rc.borrow, rc.log, rc.RecTicks = &lender.presentationState, vlog.NewLog(""), -1
		rc.log.Mute(true)
		twin, err := newApp(rc)
		if err != nil {
			return nil, nil, err
		}
		td, err := newReplayDriver(twin, stream)
		if err != nil {
			twin.Close()
			return nil, nil, err
		}
		return twin, journalSource{td, twin.log}, nil
	}
}

// runPresented presents a driven run, a script or a bot. Pacing is the run's own:
// the resolved interval is one tick's wall budget, and an unpaced run is presented
// as fast as the frame loop can render it.
func runPresented(a *App, src pacedSource, kind, name string,
	interval time.Duration, paced bool, signals <-chan os.Signal) error {

	live := a.sessionTransport() != nil
	if !paced {
		interval = time.Millisecond // the floor perTick already clamps to
	}
	a.log.Info("app", "msg", kind+" opened", "name", name, "paced", paced, "live", live)
	p := &player{
		a: a, src: src, interval: interval,
		rec: engine.ScaleNormal, scale: engine.ScaleNormal,
		live: live, interactive: kind == "bot", signals: signals,
	}
	return p.run()
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

	interactive bool // live bot control, separate from journal inspection
	// Playback controls cannot stop one participant's clock in a live session.
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
	pending      func()         // a :replay the router took, applied once it has returned

	// rw goes back in a journal replay, where a world cannot be rewound; nil for a
	// stream that cannot be rebuilt.
	rw    *rewinder
	seek  *seekState // where the viewer asked to go and the presented run has not reached
	backs int        // step backs pressed and not yet presented
}

// seekState is where the viewer asked to go: a step the run has reached, or a
// position past it, which the presented copy plays to or a move off it reaches.
type seekState struct {
	step   int          // -1 when to names it
	to     *event.Stamp // a position past the steps the run has reached
	resume bool         // play on once there, as a restart does
	hidden bool         // a copy off the frame loop is being moved there
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
	var results chan func()
	if p.rw != nil {
		results = p.rw.results
	}

	p.termW, p.termH = p.a.term.Size()
	p.a.ctx.SetPresentationSize(p.termW, p.termH)
	p.report()
	last := time.Now()

	for {
		if p.a.dismissed.Load() {
			return nil
		}
		select {
		case <-sigChan:
			return nil

		case ev := <-events:
			if !p.event(ev) {
				return nil
			}

		case apply := <-results:
			apply()

		case now := <-frameTicker.C:
			p.advance(now.Sub(last))
			p.frame()
			last = now
			if p.done && p.err != nil {
				return p.err
			}
		}
	}
}

// event applies one terminal event; false ends the presentation.
func (p *player) event(ev terminal.Event) bool {
	switch ev.Type {
	case terminal.EventResize:
		// Presentation only: the simulation keeps its recorded geometry
		p.termW, p.termH = ev.Width, ev.Height
		p.a.orchestrator.Resize(ev.Width, ev.Height)
		p.a.ctx.SetPresentationSize(ev.Width, ev.Height)
	case terminal.EventClosed, terminal.EventError:
		return false
	case terminal.EventKey:
		return p.key(ev)
	}
	return true
}

// A live bot's command line borrows its input without stopping the session clock.
func (p *player) advance(elapsed time.Duration) {
	p.live = p.interactive && p.a.sessionTransport() != nil
	if p.interactive && p.live {
		p.paused, p.scale = false, engine.ScaleNormal
		if p.a.cfg.TimeScaleSpec == "" {
			p.interval = parameter.GameUpdateInterval
		}
	}
	commandHeld := p.cmd != nil && !(p.interactive && p.live)
	if p.done || commandHeld || p.paused {
		p.a.world.Resources.Prof.Hold() // a profiler window spans played time only
	}
	if commandHeld {
		return
	}
	if p.rw != nil {
		p.advanceReplay(elapsed)
		return
	}
	if p.done {
		return
	}
	if p.paused {
		stepped := p.step > 0
		for p.step > 0 && p.tickOnce() {
			p.step--
		}
		if stepped {
			p.holdMixer() // a recorded unpause inside the step released it
			p.report()    // the key reported the tick before the step
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

// tickOnce advances a bot's or script's stream, latching its end
func (p *player) tickOnce() bool {
	// Keep the session clock running while the operator borrows the bot's router.
	if p.cmd != nil && p.interactive {
		p.a.Tick(1)
		return true
	}
	more, err := p.src.Step()
	if err != nil {
		p.a.log.Error("app", "msg", "presented run failed", "error", err.Error())
		p.done = true
		p.err = err
		p.a.ctx.SetStatusMessage("ERROR: "+err.Error(), 0, true)
		return false
	}
	if !more {
		p.done = true
		p.holdMixer()
		p.report()
		return false
	}
	return true
}

// advanceReplay presents a move's copy once it stands where the viewer asked, else
// plays the presented copy: to a seek's target unpaced for most of a frame, the
// steps granted while paused, or at the run's pace.
func (p *player) advanceReplay(elapsed time.Duration) {
	rw := p.rw
	if c := rw.ready; c != nil {
		rw.ready = nil
		p.adopt(c)
		return
	}
	pl := rw.plan(rw.shown)
	pl.deadline = time.Now().Add(parameter.FrameUpdateInterval * 3 / 4)
	switch s := p.seek; {
	case s != nil && s.hidden:
		if rw.build == nil {
			p.seek, p.backs = nil, 0
			p.a.ctx.SetStatusMessage("Step back failed: "+rw.lost.Error(), 0, true)
		}
	case s != nil:
		pl.step, pl.to = s.step, s.to
		if b := p.play(pl); b.reached || p.done {
			p.seek, p.paused = nil, !s.resume
			p.holdMixer()
			p.report()
		}
	case p.done:
	case p.paused:
		if p.step > 0 {
			pl.n, pl.ringAll = p.step, true
			p.step -= len(p.play(pl).stamps)
			p.holdMixer() // a recorded unpause inside the step released it
			p.report()    // the key reported the tick before the step
		}
	default:
		p.budget += elapsed
		per := p.perTick()
		if n := int(p.budget / per); n > 0 {
			// Owed time a frame could not play is not banked: a run slower than its
			// rate plays as fast as it can rather than in bursts.
			pl.n, pl.ringAll = n, true
			p.budget = min(p.budget-time.Duration(len(p.play(pl).stamps))*per, per)
		}
	}
}

// play steps the presented copy as pl says and takes what it found. A copy that
// left the run is replaced, from the start, at the last step it agreed on.
func (p *player) play(pl stepPlan) batch {
	c := p.rw.shown
	from := c.steps
	b := c.advance(pl)
	p.rw.learn(from, b)
	switch {
	case b.mismatch != nil:
		p.cancelSeek()
		p.paused = true
		p.moveOff(c.steps, false)
	case c.err != nil:
		p.a.log.Error("app", "msg", "presented run failed", "error", c.err.Error())
		p.done, p.err = true, c.err
		p.a.ctx.SetStatusMessage("ERROR: "+c.err.Error(), 0, true)
	case c.end:
		p.done = true
		p.holdMixer()
		p.report()
	}
	return b
}

// adopt presents a copy a move positioned, carrying the viewer's HUD, speakers and
// flight recorder over so they continue from it; the log follows the presented copy.
func (p *player) adopt(c *replayCopy) {
	rw, old, twin := p.rw, p.a, c.a
	from := old.Position()
	twin.ctx.ClearOverlayPins()
	for _, key := range old.ctx.OverlayPins() {
		twin.ctx.ToggleOverlayPin(key)
	}
	twin.ctx.OverlayHUD.Store(old.ctx.OverlayHUD.Load())
	twin.ctx.SetPresentationSize(p.termW, p.termH)
	if o := twin.orchestrator; o != nil {
		if w, h := o.Size(); w != p.termW || h != p.termH {
			o.Resize(p.termW, p.termH) // a full repaint; only for a size it was not built for
		}
	}
	system.HandOverSound(twin.world, old.world)
	twin.world.Resources.Status.TakeRecorder(old.world.Resources.Status)
	twin.ctx.ReplaySeek = p.seekLater
	old.log.Mute(true)
	twin.log.Mute(false)
	p.a, p.src, rw.shown = twin, c.src, c
	if old != rw.lender {
		rw.retired = append(rw.retired, old)
	}
	resume := p.seek != nil && p.seek.resume
	p.seek, p.paused = nil, !resume
	p.done, p.err, p.step, p.budget = c.end || c.err != nil, c.err, 0, 0
	p.logSeek(from, twin.Position())
	if p.backs > 0 {
		if p.backs--; p.backs > 0 {
			p.back()
		}
	}
	p.holdMixer()
	p.report()
	rw.pump()
}

// logSeek records a jump of the presented tick, so ticks a log restates after
// going back read as the replay they are.
func (p *player) logSeek(from, to event.Stamp) {
	p.a.log.Info("journal", "msg", "replay moved", "run", to.Run, "tick", to.Tick,
		"from_run", from.Run, "from_tick", from.Tick, "delta", int64(to.Tick)-int64(from.Tick))
}

// seekLater takes :replay from the router, which holds the world lock a move would
// swap out, so the move runs once the command line has closed.
func (p *player) seekLater(tick uint64, restart bool) {
	p.pending = func() {
		if restart {
			p.restart()
		} else {
			p.goTo(tick)
		}
	}
}

// restart plays the recording again from its start.
func (p *player) restart() {
	p.backs = 0
	p.toStep(0, true)
}

// goTo moves the presented run to the first step at or past a tick of the run it shows.
func (p *player) goTo(tick uint64) {
	p.backs = 0
	p.seekTo(event.Stamp{Run: p.a.Position().Run, Tick: tick}, false)
	p.holdMixer()
}

// seekTo moves to the first step standing at or past to: through toStep when the
// run has reached it, by playing the presented copy on when it has not.
func (p *player) seekTo(to event.Stamp, resume bool) {
	if m, known := p.rw.stepAt(to); known {
		p.toStep(m, resume)
		return
	}
	p.cancelSeek()
	p.paused = true
	if c := p.rw.shown; !c.end && c.err == nil {
		p.seek = &seekState{step: -1, to: &to, resume: resume}
		p.logSeek(p.a.Position(), to)
	}
}

// toStep moves the presented run to step count m. The presented copy plays there
// when it stands before m and no checkpoint is more than a ring nearer; otherwise a
// copy off the frame loop restores the nearest checkpoint and replays there.
func (p *player) toStep(m int, resume bool) {
	rw, c := p.rw, p.rw.shown
	p.cancelSeek()
	p.paused = true
	switch ck := rw.nearest(m); {
	case m == c.steps:
		p.paused = !resume
	case m > c.steps && !c.end && c.err == nil && (ck == nil || ck.steps <= c.steps+parameter.ReplayRing):
		p.seek = &seekState{step: m, resume: resume}
		p.logSeek(p.a.Position(), rw.trail[m])
	default:
		p.moveOff(m, resume)
	}
}

// moveOff positions a copy off the frame loop at step count m, presented once there.
func (p *player) moveOff(m int, resume bool) {
	rw := p.rw
	if rw.build == nil {
		p.backs = 0
		p.a.ctx.SetStatusMessage("Step back failed: "+rw.lost.Error(), 0, true)
		return
	}
	p.seek = &seekState{step: m, resume: resume, hidden: true}
	rw.want = &move{step: m}
	rw.pump()
}

// back presents the step before the one shown.
func (p *player) back() {
	if m := p.rw.shown.steps - 1; m >= 0 {
		p.toStep(m, false)
		return
	}
	p.backs = 0
}

// cancelSeek drops a move the viewer asked for and has not reached.
func (p *player) cancelSeek() {
	p.seek = nil
	if p.rw != nil {
		p.rw.cancel()
	}
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
	p.live = p.interactive && p.a.sessionTransport() != nil
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
	case ' ', '.', ',', '+', '=', '-', '_':
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
		if p.done && p.rw != nil && p.rw.build != nil {
			p.restart()
			break
		}
		p.cancelSeek()
		p.paused = !p.paused
		p.budget, p.backs = 0, 0
		p.holdMixer()
	case '.':
		p.cancelSeek()
		p.paused, p.step, p.backs = true, p.step+1, 0
		p.holdMixer()
	case ',':
		// Each press is one tick a frame presents, as '.' is, once a copy stands there
		p.paused, p.step = true, 0
		switch {
		case p.rw == nil:
		case p.backs > 0:
			if p.backs < p.rw.shown.steps {
				p.backs++
			}
		default:
			p.backs = 1
			p.back()
		}
		p.holdMixer()
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
	case input.IntentModeSwitch, input.IntentConfigMenu:
		if (intent.Type == input.IntentConfigMenu || intent.ModeTarget == input.ModeTargetCommand) && (p.interactive || !p.live) {
			a := p.a
			p.cmd = &viewerCommand{mode: a.ctx.GetMode(), paused: a.ctx.TimeCtl.IsPaused()}
			a.ctx.Viewer.Store(!p.interactive)
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
	// Handed back as the viewer, so it announces nothing the recording did not
	a.world.RunSafe(func() {
		if a.ctx.GetMode() != c.mode {
			a.ctx.RequestMode(c.mode)
		}
		if a.ctx.TimeCtl.IsPaused() != c.paused {
			a.ctx.SetPaused(c.paused)
		}
	})
	a.ctx.Viewer.Store(false)
	a.Settle()
	p.holdMixer()
	if move := p.pending; move != nil {
		p.pending = nil
		move()
	}
	return true
}

// holdMixer fades the mixer out while the viewer pauses, steps or seeks and once the
// stream has ended, whatever the recording's own pause says.
func (p *player) holdMixer() { p.a.holdMixer(p.paused || p.done || p.seek != nil) }

// route applies one viewer intent through the game's router and settles it at
// once, since nothing else dispatches between a replay's ticks. The viewer is
// out-of-band control rather than the recorded player, so none of it is effort.
func (p *player) route(intent *input.Intent) bool {
	a := p.a
	if p.interactive {
		return a.Inject(intent)
	}
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
	case p.backs > 0:
		state = "BACK" // a copy is being moved to the step before
	case p.seek != nil:
		state = "SEEK"
	case p.done:
		state = "END"
	case p.paused:
		state = "PAUSE"
	}
	progress := p.src.progress()
	if rw := p.rw; rw != nil && rw.divergedAt >= 0 && rw.shown.steps >= rw.divergedAt {
		progress += fmt.Sprintf(" | diverged by tick %d", rw.diverged.At.Tick)
	}
	// The keys are on :help rather than on a bar the recording's messages share
	keys := ":h for keys"
	if p.live && !p.done {
		state, keys = "LIVE", "hjkl 0 q"
		if p.interactive {
			keys = ":h for keys"
		}
	}
	p.a.ctx.SetStatusMessage(fmt.Sprintf("%s %sx | %s | %s", state, p.scale.String(), progress, keys), 0, true)
}
