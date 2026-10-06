//go:build !vif_headless

package app

import (
	"cmp"
	"fmt"
	"os"
	"runtime"
	"slices"
	"sync"
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
	d, err := replayDriver(a, set)
	if err != nil {
		return err
	}
	a.log.Info("journal", "msg", "replay opened",
		"records", len(set.Records), "digests", len(set.Digests), "seed", an.Seed, "speed", an.Speed)
	p := &player{a: a, src: journalSource{d, a.log}, interval: time.Duration(an.TickInterval),
		rec: parseSpeed(an.Speed), scale: engine.ScaleNormal}
	p.rebuild = func() (*App, pacedSource, error) {
		// A copy replays ticks the log already holds; it writes once presented.
		rc := cfg
		rc.borrow, rc.log = &a.presentationState, vlog.NewLog("")
		rc.log.Mute(true)
		twin, err := NewReplay(rc)
		if err != nil {
			return nil, nil, err
		}
		td, err := replayDriver(twin, set)
		if err != nil {
			twin.Close()
			return nil, nil, err
		}
		return twin, journalSource{td, twin.log}, nil
	}
	p.trail, p.slots = []event.Stamp{a.Position()}, make(chan struct{}, max(1, runtime.GOMAXPROCS(0)-1))
	p.every = parameter.ReplayCheckpointSteps
	a.ctx.ReplaySeek = p.seekLater
	defer p.closeRebuilt()
	return p.run()
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

// journalSource adapts a record stream to the presentation loop.
type journalSource struct {
	d   *journal.ReplayDriver
	log *vlog.Log
}

// Step advances one recorded tick. A world written between two ticks, or a group
// landing on one, took none of the recorded run's time, so it shares the next tick's
// rather than holding the view a tick of its own. It logs the first digest the
// replay does not reproduce; the bar keeps showing it.
func (s journalSource) Step() (bool, error) {
	from := s.d.Stats()
	more, err := s.d.Step()
	for at := s.d.Stats().End; more && err == nil && at.Run == from.End.Run && at.Tick == from.End.Tick; at = s.d.Stats().End {
		more, err = s.d.Step()
	}
	if v := s.d.Stats().Diverged; v != nil && from.Diverged == nil {
		s.log.Warn("journal", "msg", "replay diverged", "tick", v.At.Tick, "run", v.At.Run, "error", v.Error())
	}
	return more, err
}

func (s journalSource) progress() string {
	st := s.d.Stats()
	at := time.Duration(st.End.Tick) * parameter.GameUpdateInterval / time.Second
	out := fmt.Sprintf("run %d tick %d %d:%02d:%02d | %d/%d rec",
		st.End.Run, st.End.Tick, at/3600, at/60%60, at%60, st.Injected, st.Records)
	if v := st.Diverged; v != nil {
		out += fmt.Sprintf(" | diverged by tick %d", v.At.Tick)
	}
	return out
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

	// rebuild makes a fresh copy of the presented run for stepping back: a world
	// cannot be rewound, so copies replay the stream from its start off the frame
	// loop and trail the presented one a tick apart. Nil for a stream that cannot
	// be rebuilt.
	rebuild func() (*App, pacedSource, error)
	trail   []event.Stamp // position at each step count, from the copy before any step
	spares  []*rebuilt    // copies parked one, two, ... ticks behind the presented one
	// checkpoints hold the presented run's whole state every `every` steps, by step
	// count; a copy starts from the nearest one behind its target. every is zero
	// once one has left the run.
	checkpoints []*checkpoint
	every       int
	backs       int           // step backs pressed and not yet presented
	seek        *rebuilt      // a copy replaying to a tick behind that the viewer named
	ahead       *event.Stamp  // a tick ahead that the viewer named, played to unpaced
	resume      bool          // play on once the seek lands, as a restart does
	pending     func()        // a :replay the router took, applied once it has returned
	slots       chan struct{} // copies step on all cores but the one the presented copy keeps
	copies      sync.WaitGroup
}

// rebuilt is a copy of the presented run replaying its stream on its own
// goroutine up to a target step count, where it parks until the target moves.
type rebuilt struct {
	a       *App
	src     pacedSource
	from    *checkpoint         // restored before replaying on; nil replays from the start
	left    *journal.Divergence // the first digest a restored copy did not reproduce
	w, h    int                 // the terminal its renderers were laid out for
	slots   chan struct{}
	running *sync.WaitGroup
	mu      sync.Mutex
	at      int // steps replayed
	target  int
	err     error
	cancel  bool
	drop    bool // close the copy on stopping
	exited  bool
	wake    chan struct{}
	done    chan struct{}
}

// run replays toward the target and parks on reaching it.
func (r *rebuilt) run() {
	defer r.running.Done()
	defer close(r.done)
	defer r.exit()
	if r.from != nil {
		var err error
		r.slots <- struct{}{}
		err = r.a.restore(r.from, r.src.(journalSource).d)
		<-r.slots
		if err != nil {
			r.mu.Lock()
			r.err = err
			r.mu.Unlock()
			return
		}
	}
	for {
		r.mu.Lock()
		at, target, cancel := r.at, r.target, r.cancel
		r.mu.Unlock()
		switch {
		case cancel:
			return
		case at < target:
			var err error
			r.slots <- struct{}{}
			_, err = r.src.Step()
			<-r.slots
			r.mu.Lock()
			r.at, r.err = r.at+1, err
			if r.from != nil && r.left == nil {
				r.left = r.src.(journalSource).d.Stats().Diverged
			}
			r.mu.Unlock()
			if err != nil {
				return
			}
		default:
			<-r.wake
		}
	}
}

// exit closes a copy stopped while it ran; stop closes one stopped after.
func (r *rebuilt) exit() {
	r.mu.Lock()
	r.exited = true
	drop := r.drop
	r.mu.Unlock()
	if drop {
		r.a.Close()
	}
}

// aim moves the target, reporting false when the copy has replayed past it or failed.
func (r *rebuilt) aim(target int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.at > target || r.err != nil {
		return false
	}
	if r.target != target {
		r.target = target
		r.signal()
	}
	return true
}

// parked reports a copy standing at its target, or the error that stopped it.
func (r *rebuilt) parked() (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.at == r.target && r.err == nil, r.err
}

func (r *rebuilt) signal() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// halt ends the replay of a parked copy and hands the copy to the caller.
func (r *rebuilt) halt() {
	r.mu.Lock()
	r.cancel = true
	r.mu.Unlock()
	r.signal()
	<-r.done
}

// stop ends the replay and closes the copy without waiting out a step in flight,
// which waits on the frame loop that calls it.
func (r *rebuilt) stop() {
	r.mu.Lock()
	r.cancel, r.drop = true, true
	exited := r.exited
	r.mu.Unlock()
	r.signal()
	if exited {
		r.a.Close()
	}
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

		case now := <-frameTicker.C:
			p.advance(now.Sub(last))
			p.keep()
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
	p.land()
	if p.done {
		return
	}
	if p.ahead != nil {
		// A seek ahead plays unpaced for most of a frame, so the view keeps drawing
		for end := time.Now().Add(parameter.FrameUpdateInterval * 3 / 4); time.Now().Before(end); {
			if at := p.a.Position(); at.Run != p.ahead.Run || at.Tick >= p.ahead.Tick || !p.tickOnce() {
				p.ahead = nil
				p.holdMixer()
				p.report()
				break
			}
		}
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

// tickOnce advances the driver, latching the end of the stream
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
	if p.rebuild != nil {
		p.trail = append(p.trail, p.a.Position())
		p.checkpoint()
	}
	return true
}

// checkpoint keeps the presented run's state on the step cadence. Past the cap,
// every other one is dropped and the spacing doubles.
func (p *player) checkpoint() {
	steps := len(p.trail) - 1
	if p.every == 0 || steps%p.every != 0 {
		return
	}
	i, found := slices.BinarySearchFunc(p.checkpoints, steps, func(c *checkpoint, s int) int { return cmp.Compare(c.steps, s) })
	if found {
		return
	}
	var (
		c   *checkpoint
		err error
	)
	p.a.world.RunSafe(func() { c, err = p.a.checkpointLocked(p.src.(journalSource).d) })
	if err != nil {
		p.a.log.Warn("journal", "msg", "replay checkpoint failed", "error", err.Error())
		p.every, p.checkpoints = 0, nil
		return
	}
	c.steps = steps
	p.checkpoints = slices.Insert(p.checkpoints, i, c)
	if len(p.checkpoints) > parameter.ReplayCheckpoints {
		p.every *= 2
		p.checkpoints = slices.DeleteFunc(p.checkpoints, func(c *checkpoint) bool { return c.steps%p.every != 0 })
	}
}

// closer reports a checkpoint far enough past where a copy stands, short of its
// target, that a fresh copy restored from it arrives sooner. A fresh copy is built
// on the frame loop, so one inside the ladder's depth is not: the ladder crosses
// every checkpoint the presented run passes, and rebuilding it there stalled 1x.
func (p *player) closer(r *rebuilt, target int) bool {
	c := p.nearest(target)
	r.mu.Lock()
	defer r.mu.Unlock()
	return c != nil && c.steps > r.at+parameter.ReplayBackSpares
}

// nearest is the latest checkpoint at or before step count target, nil when none.
func (p *player) nearest(target int) *checkpoint {
	i, _ := slices.BinarySearchFunc(p.checkpoints, target+1, func(c *checkpoint, s int) int { return cmp.Compare(c.steps, s) })
	if i == 0 {
		return nil
	}
	return p.checkpoints[i-1]
}

// strayed reports a copy restored from a checkpoint that failed a digest the
// presented run reproduced, and stops using checkpoints: one carried the world short.
func (p *player) strayed(r *rebuilt) bool {
	r.mu.Lock()
	left := r.left
	r.mu.Unlock()
	if left == nil || p.every == 0 {
		return false
	}
	if shown := p.src.(journalSource).d.Stats().Diverged; shown != nil &&
		(shown.At.Run < left.At.Run || shown.At.Run == left.At.Run && shown.At.Tick <= left.At.Tick) {
		return false // the recorded run is not reproduced there from the start either
	}
	p.a.log.Warn("journal", "msg", "replay checkpoint left the run", "tick", left.At.Tick, "error", left.Error())
	p.every, p.checkpoints = 0, nil
	return true
}

// backTargets is the step counts that leave the presented run one, two, ... up to
// n ticks before the tick it shows, nearest first.
func (p *player) backTargets(n int) []int {
	var out []int
	for s := len(p.trail) - 1; p.rebuild != nil && len(out) < n; {
		at, m := p.trail[s], s-1
		for m >= 0 && !(p.trail[m].Run < at.Run || p.trail[m].Run == at.Run && p.trail[m].Tick < at.Tick) {
			m--
		}
		if m < 0 {
			break
		}
		out, s = append(out, m), m
	}
	return out
}

// keep moves the spares to the ticks behind the presented one, starting a copy for
// each the ladder lacks and replacing one that has replayed past its tick.
func (p *player) keep() {
	if p.rebuild == nil || p.seek != nil || p.ahead != nil {
		return // a seek has the cores; the ladder follows where it lands
	}
	want := p.backTargets(parameter.ReplayBackSpares)
	p.dropSpares(len(p.trail) - 1)
	for len(p.spares) > len(want) {
		p.spares[len(p.spares)-1].stop()
		p.spares = p.spares[:len(p.spares)-1]
	}
	for i, r := range p.spares {
		if !p.strayed(r) && !p.closer(r, want[i]) && r.aim(want[i]) {
			continue
		}
		if _, err := r.parked(); err != nil {
			p.fail(err)
			return
		}
		n := p.spawn(want[i])
		if n == nil {
			return // fail stopped every spare, r with them
		}
		r.stop()
		p.spares[i] = n
	}
	for len(p.spares) < len(want) {
		r := p.spawn(want[len(p.spares)])
		if r == nil {
			return
		}
		p.spares = append(p.spares, r)
	}
}

// spawn builds a copy and replays it toward target on its own goroutine.
func (p *player) spawn(target int) *rebuilt {
	a, src, err := p.rebuild()
	if err != nil {
		p.fail(err)
		return nil
	}
	r := &rebuilt{a: a, src: src, w: p.termW, h: p.termH, slots: p.slots, running: &p.copies, target: target, wake: make(chan struct{}, 1), done: make(chan struct{})}
	if r.from = p.nearest(target); r.from != nil {
		r.at = r.from.steps
	}
	p.copies.Add(1)
	core.Go(r.run)
	return r
}

// land presents a copy standing where the viewer asked: a seek's, else the
// nearest spare for a pending step back. One a frame, so each tick stepped through
// is drawn.
func (p *player) land() {
	r := p.seek
	if r == nil && p.backs > 0 && len(p.spares) > 0 {
		r = p.spares[0]
	}
	if r == nil {
		return
	}
	if r == p.seek && p.strayed(r) {
		r.stop()
		p.seek = p.spawn(r.target)
		return
	}
	if ok, err := r.parked(); err != nil {
		p.fail(err)
		return
	} else if !ok {
		return
	}
	if r == p.seek {
		p.seek, p.paused, p.resume = nil, !p.resume, false
	} else {
		p.spares, p.backs = slices.Delete(p.spares, 0, 1), p.backs-1
	}
	p.adopt(r)
}

// adopt presents a parked copy, carrying the viewer's HUD and speakers over so they
// continue from it; the log follows the presented copy.
func (p *player) adopt(r *rebuilt) {
	r.halt()
	old, twin := p.a, r.a
	from := old.Position()
	twin.ctx.ClearOverlayPins()
	for _, key := range old.ctx.OverlayPins() {
		twin.ctx.ToggleOverlayPin(key)
	}
	twin.ctx.OverlayHUD.Store(old.ctx.OverlayHUD.Load())
	twin.ctx.SetPresentationSize(p.termW, p.termH)
	if r.w != p.termW || r.h != p.termH {
		twin.orchestrator.Resize(p.termW, p.termH) // a full repaint; only for a size it was not built for
	}
	system.HandOverSound(twin.world, old.world)
	twin.ctx.ReplaySeek = p.seekLater
	old.log.Mute(true)
	twin.log.Mute(false)
	p.a, p.src, p.trail = twin, r.src, p.trail[:r.target+1]
	p.done, p.err, p.step, p.budget = false, nil, 0, 0
	if old.cfg.borrow != nil {
		old.Close()
	}
	p.logSeek(from, twin.Position())
	p.holdMixer()
	p.keep()
	p.report()
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
	p.goBack(0, true)
}

// goTo moves the presented run to a tick of the run it shows: ahead by playing to
// it unpaced, behind through a copy replayed to the last step at it.
func (p *player) goTo(tick uint64) {
	p.cancelMoves()
	p.backs, p.paused = 0, true
	at := p.a.Position()
	switch {
	case tick > at.Tick && !p.done:
		p.ahead = &event.Stamp{Run: at.Run, Tick: tick}
		p.logSeek(at, *p.ahead)
	case tick < at.Tick:
		m := len(p.trail) - 1
		for m > 0 && p.trail[m].Run == at.Run && p.trail[m].Tick > tick {
			m--
		}
		p.goBack(m, false)
	}
	p.holdMixer()
}

// goBack presents the run at step count m through a spare already parked there,
// else a copy replayed from the start.
func (p *player) goBack(m int, resume bool) {
	p.cancelMoves()
	if p.rebuild == nil {
		return // a copy failed; nothing replays behind the presented one
	}
	p.resume, p.paused = resume, true
	if i := slices.IndexFunc(p.spares, func(r *rebuilt) bool { return r.target == m }); i >= 0 {
		p.seek, p.spares = p.spares[i], slices.Delete(p.spares, i, i+1)
	} else {
		p.seek = p.spawn(m)
	}
	p.dropSpares(m)
}

// dropSpares stops the spares at or past step count m, which nothing will present.
func (p *player) dropSpares(m int) {
	p.spares = slices.DeleteFunc(p.spares, func(r *rebuilt) bool {
		if r.target < m {
			return false
		}
		r.stop()
		return true
	})
}

// cancelMoves drops a seek the viewer asked for and has not reached.
func (p *player) cancelMoves() {
	if p.seek != nil {
		p.seek.stop()
	}
	p.seek, p.ahead, p.resume = nil, nil, false
}

// fail gives up stepping back once a copy could not be built or replayed.
func (p *player) fail(err error) {
	p.a.log.Warn("journal", "msg", "replay step back failed", "error", err.Error())
	p.stopSpares()
	p.cancelMoves()
	p.rebuild, p.backs = nil, 0
	p.a.ctx.SetStatusMessage("Step back failed: "+err.Error(), 0, true)
}

func (p *player) stopSpares() {
	for _, r := range p.spares {
		r.stop()
	}
	p.spares = nil
}

// closeRebuilt closes the spares, and the presented copy if it was rebuilt, before
// the run that lent them its terminal.
func (p *player) closeRebuilt() {
	p.stopSpares()
	p.cancelMoves()
	p.copies.Wait()
	if p.a.cfg.borrow != nil {
		p.a.Close()
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
		if p.done && p.rebuild != nil {
			p.restart()
			break
		}
		p.cancelMoves()
		p.paused = !p.paused
		p.budget, p.backs = 0, 0
		p.holdMixer()
	case '.':
		p.cancelMoves()
		p.paused, p.step, p.backs = true, p.step+1, 0
		p.holdMixer()
	case ',':
		// Each press is one tick a frame presents, as '.' is, once a copy stands there
		p.cancelMoves()
		p.paused, p.step = true, 0
		p.holdMixer()
		if len(p.backTargets(p.backs+1)) > p.backs {
			p.backs++
		}
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

// holdMixer fades the mixer out while the viewer pauses or steps and once the
// stream has ended, whatever the recording's own pause says.
func (p *player) holdMixer() { p.a.holdMixer(p.paused || p.done || p.ahead != nil) }

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
	case p.seek != nil || p.ahead != nil:
		state = "SEEK"
	case p.done:
		state = "END"
	case p.backs > 0:
		state = "BACK" // waiting on a copy replaying from the start of the stream
	case p.paused:
		state = "PAUSE"
	}
	// The keys are on :help rather than on a bar the recording's messages share
	keys := ":h for keys"
	if p.live && !p.done {
		state, keys = "LIVE", "hjkl 0 q"
		if p.interactive {
			keys = ":h for keys"
		}
	}
	p.a.ctx.SetStatusMessage(fmt.Sprintf("%s %sx | %s | %s",
		state, p.scale.String(), p.src.progress(), keys), 0, true)
}
