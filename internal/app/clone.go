package app

import (
	"cmp"
	"fmt"
	"slices"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/content"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/service"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/internal/vlog"
)

// checkpoint is a replay copy's whole state where its driver stood: what a fresh
// copy restores to continue from there instead of replaying the stream before it.
type checkpoint struct {
	steps     int                 // the driver steps it was read after
	at        event.Stamp         // where the copy stood
	digest    event.JournalDigest // its world, which a restore must reproduce
	world     *engine.WorldCopy
	queue     event.QueueCopy
	scheduler engine.SchedulerCopy
	systems   []snapshot.SystemStateRecord
	local     map[string]any
	toggles   map[string]bool
	status    snapshot.StatusState
	content   content.CursorState
	clock     time.Duration
	paused    bool
	scale     engine.TimeScale
	width     int
	height    int
	driver    journal.Cursor
}

// checkpointLocked reads a replay copy and where its driver stands. Caller MUST
// hold updateMutex.
func (a *App) checkpointLocked(d *journal.ReplayDriver) (*checkpoint, error) {
	systems, err := a.captureSystemStatesLocked()
	if err != nil {
		return nil, err
	}
	local, toggles := map[string]any{}, map[string]bool{}
	for _, sys := range a.world.Systems() {
		if cp, ok := sys.(engine.StateCopier); ok {
			local[sys.Name()] = cp.CopyState()
		}
		if t, ok := sys.(engine.Toggled); ok {
			toggles[sys.Name()] = t.Enabled()
		}
	}
	// A copier carries the whole of its system; LoadShared would re-derive it as a joiner.
	systems = slices.DeleteFunc(systems, func(r snapshot.SystemStateRecord) bool {
		_, copied := local[r.System]
		return copied
	})
	c := &checkpoint{
		world:     a.world.CopyOut(),
		queue:     a.world.Resources.Event.Queue.CopyOut(),
		scheduler: a.scheduler.CopyOut(),
		systems:   systems,
		status:    a.statusCellsLocked(a.statusKeysLocked(func(string) bool { return true })),
		paused:    a.ctx.TimeCtl.IsPaused(),
		scale:     a.ctx.TimeCtl.Scale(),
		width:     a.ctx.Width,
		height:    a.ctx.Height,
		local:     local,
		toggles:   toggles,
		content:   service.MustGet[*service.ContentService](a.hub, "content").CursorState(),
		driver:    d.Cursor(),
		at:        a.Position(),
		digest:    a.journalDigestLocked(),
	}
	if mc, ok := a.ctx.TimeCtl.Clock().(*engine.ManualClock); ok {
		c.clock = mc.Elapsed()
	}
	return c, nil
}

// restore settles a fresh replay copy's boot and places it where c was read. A copy
// that does not then stand where c's did, on the digested classes of its world, is
// refused: a checkpoint that carried the run short is caught before anything plays.
func (a *App) restore(c *checkpoint, d *journal.ReplayDriver) (err error) {
	a.Settle()
	a.world.RunSafe(func() {
		if err = a.restoreLocked(c, d); err != nil {
			return
		}
		if at, got := a.Position(), a.journalDigestLocked(); at != c.at || got != c.digest {
			err = fmt.Errorf("restore at step %d: stands at %+v with digest %+v, the checkpoint at %+v with %+v",
				c.steps, at, got, c.at, c.digest)
		}
	})
	return err
}

// restoreLocked places a fresh replay copy, built from the same journal, where c
// was read, and its driver with it. Caller MUST hold updateMutex.
func (a *App) restoreLocked(c *checkpoint, d *journal.ReplayDriver) error {
	a.world.CopyIn(c.world)
	if unknown := c.world.LoadStreams(a.world.Resources.Rand); len(unknown) > 0 {
		return fmt.Errorf("restore: RNG streams this copy does not issue: %v", unknown)
	}
	savers := a.sharedStateSaversLocked()
	for _, rec := range c.systems {
		if err := savers[rec.System].LoadShared(rec.Data); err != nil {
			return fmt.Errorf("restore %s: %w", rec.System, err)
		}
	}
	for _, sys := range a.world.Systems() {
		if cp, ok := sys.(engine.StateCopier); ok {
			if err := cp.RestoreState(c.local[sys.Name()]); err != nil {
				return fmt.Errorf("restore %s: %w", sys.Name(), err)
			}
		}
	}
	if err := a.scheduler.CopyIn(c.scheduler); err != nil {
		return err
	}
	if c.scheduler.ResetPending() {
		select {
		case a.ctx.ResetChan <- struct{}{}:
		default:
		}
	}
	// After the FSM import, whose re-derived toggles are queued and dropped here
	a.world.Resources.Event.Queue.CopyIn(c.queue)
	for _, sys := range a.world.Systems() {
		if t, ok := sys.(engine.Toggled); ok {
			t.SetEnabled(c.toggles[sys.Name()])
		}
	}
	if mc, ok := a.ctx.TimeCtl.Clock().(*engine.ManualClock); ok {
		mc.SetElapsed(c.clock)
	}
	a.ctx.TimeCtl.SetPaused(c.paused)
	a.ctx.TimeCtl.SetScale(c.scale)
	a.ctx.Width, a.ctx.Height = c.width, c.height
	service.MustGet[*service.ContentService](a.hub, "content").SetCursorState(c.content)
	a.installStatusLocked(c.status)
	d.Resume(c.driver)
	return nil
}

// journalSource adapts a record stream to the presentation loop.
type journalSource struct {
	d   *journal.ReplayDriver
	log *vlog.Log
}

// Step advances one recorded tick. A world written between two ticks, or a group
// landing on one, took none of the recorded run's time, so it shares the next tick's
// rather than holding the view a tick of its own.
func (s journalSource) Step() (bool, error) {
	from := s.d.Stats()
	more, err := s.d.Step()
	for at := s.d.Stats().End; more && err == nil && at.Run == from.End.Run && at.Tick == from.End.Tick; at = s.d.Stats().End {
		more, err = s.d.Step()
	}
	return more, err
}

func (s journalSource) progress() string {
	st := s.d.Stats()
	at := time.Duration(st.End.Tick) * parameter.GameUpdateInterval / time.Second
	return fmt.Sprintf("run %d tick %d %d:%02d:%02d | %d/%d rec",
		st.End.Run, st.End.Tick, at/3600, at/60%60, at%60, st.Injected, st.Records)
}

// replayCopy is one copy of a journal's run: fresh at step zero, positioned by a
// move, or presented. Whoever holds it steps it; a job hands it back with its batch.
type replayCopy struct {
	a     *App
	src   journalSource
	steps int   // steps taken; a restored copy starts at its checkpoint's
	end   bool  // a step found the stream's end
	err   error // a step failed there, and nothing plays past it
}

// stepPlan bounds one run of a copy's steps: at most n (negative for no bound),
// until it has taken step or stands at or past to, or until deadline or cancel.
type stepPlan struct {
	n        int
	step     int
	to       *event.Stamp
	limit    int // the step the stream failed after, which ends it; -1 none
	deadline time.Time
	cancel   *atomic.Bool

	known      []event.Stamp // where the run stood after the copy's next steps
	divergedAt int           // the step the run left its digests at, -1 none
	ringAll    bool          // keep every step in the ring, as paced play does
	have       [parameter.ReplayRing]int
	every      int // ladder spacing, 0 once checkpoints are off
	nextLadder int
}

// batch is what one run of steps found.
type batch struct {
	stamps   []event.Stamp // where the copy stood after each step
	diverged *journal.Divergence
	at       int // the step that diverged, -1 none
	saved    []*checkpoint
	reached  bool
	mismatch error // a step left the run; stepping stopped before it counted
	lost     error // a checkpoint could not be taken
}

// tickOrder orders positions by run and tick; a boundary moves with the viewer's
// own settles as well as the run's, so it is never compared.
func tickOrder(a, b event.Stamp) int {
	return cmp.Or(cmp.Compare(a.Run, b.Run), cmp.Compare(a.Tick, b.Tick))
}

// advance steps c as p says. It touches only c and what p holds, so it can run off
// the frame loop. Every step the run has already taken must stand and diverge where
// the run did; one that does not stops the batch before it counts.
func (c *replayCopy) advance(p stepPlan) (b batch) {
	b.at = -1
	from, latched := c.steps, c.src.d.Stats().Diverged != nil
	for {
		switch {
		case c.end || c.err != nil:
			return
		case p.step >= 0 && c.steps >= p.step, p.to != nil && tickOrder(c.a.Position(), *p.to) >= 0:
			b.reached = true
			return
		case p.limit >= 0 && c.steps >= p.limit:
			c.end = true
			return
		case p.n >= 0 && len(b.stamps) >= p.n, p.cancel != nil && p.cancel.Load(),
			len(b.stamps) > 0 && !p.deadline.IsZero() && time.Now().After(p.deadline):
			return
		}
		more, err := c.src.Step()
		k, i := c.steps+1, c.steps-from
		covered := i < len(p.known)
		if covered && (err != nil || !more) {
			b.mismatch = fmt.Errorf("step %d stopped where the run went on: %v", k, err)
			return
		}
		if err != nil {
			c.err = err
			return
		}
		if !more {
			c.end = true
			return
		}
		at, v := c.a.Position(), c.src.d.Stats().Diverged
		if covered && (tickOrder(at, p.known[i]) != 0 || (v != nil) != (p.divergedAt >= 0 && k >= p.divergedAt)) {
			b.mismatch = fmt.Errorf("step %d stood at run %d tick %d, diverged %t; the run stood at run %d tick %d",
				k, at.Run, at.Tick, v != nil, p.known[i].Run, p.known[i].Tick)
			return
		}
		if !covered && v != nil && !latched {
			b.diverged, b.at = v, k
		}
		latched, c.steps = v != nil, k
		b.stamps = append(b.stamps, at)
		ladder := p.every > 0 && k == p.nextLadder
		ring := p.every > 0 && p.have[k%len(p.have)] != k && (p.ringAll ||
			p.step >= 0 && k+len(p.have) > p.step ||
			p.to != nil && at.Run == p.to.Run && at.Tick+uint64(len(p.have)) > p.to.Tick)
		if ladder {
			p.nextLadder += p.every
		}
		if !ladder && !ring {
			continue
		}
		var ck *checkpoint
		c.a.world.RunSafe(func() { ck, err = c.a.checkpointLocked(c.src.d) })
		if err != nil {
			b.lost, p.every = err, 0
			continue
		}
		ck.steps = k
		b.saved = append(b.saved, ck)
	}
}

// rewinder is how a journal replay goes back: where the run stood after each step,
// its checkpoints, one fresh idle copy, and one job at a time off the frame loop that
// builds a copy or moves one. The frame loop owns every field; a job owns only the
// copy it was handed, and returns it through results.
type rewinder struct {
	build   func() (*App, pacedSource, error) // nil once one failed: nothing replays behind
	lost    error                             // why build is nil
	lender  *App                              // the run copies borrow the terminal from
	results chan func()
	busy    bool        // a build or a move is running
	lent    bool        // a batch of the presented copy's steps is running
	shown   *replayCopy // nothing but a batch touches it while lent

	// trail is where the run stood after each step, as far as any copy went. It only
	// grows, so a job reads its tail while the loop appends; a cut copies it.
	trail      []event.Stamp
	divergedAt int // the step that first failed a written digest, -1 none
	diverged   *journal.Divergence
	fail       error // the stream failed after step failedAt, where it ends
	failedAt   int

	ring   [parameter.ReplayRing]*checkpoint // the last steps taken, at step % len
	ladder []*checkpoint                     // every `every` steps, thinned past the cap
	every  int                               // 0 once a checkpoint failed or left the run

	idle, ready   *replayCopy // fresh for the next move; a move's copy, adopted next frame
	want, running *move
	retired       []*App // closed by the next build, off the frame loop
	closing       bool
}

// move positions a fresh copy at step count step.
type move struct {
	step   int
	cancel atomic.Bool
}

func newRewinder(shown *replayCopy, lender *App, build func() (*App, pacedSource, error)) *rewinder {
	rw := &rewinder{build: build, lender: lender, shown: shown, results: make(chan func(), 2),
		trail: []event.Stamp{shown.a.Position()}, divergedAt: -1, failedAt: -1, every: parameter.ReplayCheckpointSteps}
	rw.pump()
	return rw
}

// plan is what a copy at its step must reproduce and which of its steps to keep.
func (rw *rewinder) plan(c *replayCopy) stepPlan {
	p := stepPlan{n: -1, step: -1, limit: rw.failedAt, divergedAt: rw.divergedAt, every: rw.every}
	if c.steps+1 < len(rw.trail) {
		p.known = rw.trail[c.steps+1:]
	}
	for i, ck := range rw.ring {
		p.have[i] = -1
		if ck != nil {
			p.have[i] = ck.steps
		}
	}
	if p.every > 0 {
		p.nextLadder = (max(c.steps, len(rw.trail)-1)/p.every + 1) * p.every
	}
	return p
}

// lend steps the presented copy as p says on its own goroutine, so an install or a
// checkpoint never holds the frame loop; then runs on the loop once it is handed back.
func (rw *rewinder) lend(p stepPlan, then func(batch)) {
	c, from := rw.shown, rw.shown.steps
	rw.lent = true
	core.Go(func() {
		b := c.advance(p)
		rw.results <- func() {
			rw.lent = false
			rw.learn(from, b)
			then(b)
		}
	})
}

// learn takes what a copy's batch found from step from: the steps the run had not
// reached, its first divergence, the checkpoints kept, and a copy that left the run.
func (rw *rewinder) learn(from int, b batch) {
	for i, at := range b.stamps {
		if from+1+i == len(rw.trail) {
			rw.trail = append(rw.trail, at)
		}
	}
	if rw.divergedAt < 0 && b.at >= 0 {
		rw.divergedAt, rw.diverged = b.at, b.diverged
		rw.shown.a.log.Warn("journal", "msg", "replay diverged", "tick", b.diverged.At.Tick,
			"run", b.diverged.At.Run, "error", b.diverged.Error())
	}
	for _, ck := range b.saved {
		rw.store(ck)
	}
	if b.lost != nil {
		rw.drop("replay checkpoint failed", b.lost)
	}
	if b.mismatch != nil {
		cut := from + 1 + len(b.stamps)
		rw.trail = slices.Clone(rw.trail[:min(cut, len(rw.trail))])
		if rw.divergedAt >= cut {
			rw.divergedAt, rw.diverged = -1, nil
		}
		rw.drop("replay checkpoint left the run", b.mismatch)
	}
}

// store keeps a checkpoint in the ring, and on the ladder at its cadence. Past the
// cap, every other ladder entry is dropped and the spacing doubles.
func (rw *rewinder) store(c *checkpoint) {
	if rw.every == 0 {
		return
	}
	rw.ring[c.steps%len(rw.ring)] = c
	if c.steps%rw.every != 0 {
		return
	}
	i, found := slices.BinarySearchFunc(rw.ladder, c.steps, func(x *checkpoint, s int) int { return cmp.Compare(x.steps, s) })
	if found {
		return
	}
	rw.ladder = slices.Insert(rw.ladder, i, c)
	if len(rw.ladder) > parameter.ReplayCheckpoints {
		rw.every *= 2
		rw.ladder = slices.DeleteFunc(rw.ladder, func(x *checkpoint) bool { return x.steps%rw.every != 0 })
	}
}

// drop stops checkpointing: one failed, or a copy restored from one left the run, so
// none is trusted and moves replay from the start.
func (rw *rewinder) drop(msg string, err error) {
	if rw.every == 0 {
		return
	}
	rw.shown.a.log.Warn("journal", "msg", msg, "error", err.Error())
	rw.every, rw.ladder, rw.ring = 0, nil, [parameter.ReplayRing]*checkpoint{}
}

// nearest is the latest checkpoint at or before step count m, nil when none.
func (rw *rewinder) nearest(m int) *checkpoint {
	var best *checkpoint
	for _, c := range rw.ring {
		if c != nil && c.steps <= m && (best == nil || c.steps > best.steps) {
			best = c
		}
	}
	i, _ := slices.BinarySearchFunc(rw.ladder, m+1, func(x *checkpoint, s int) int { return cmp.Compare(x.steps, s) })
	if i > 0 && (best == nil || rw.ladder[i-1].steps > best.steps) {
		best = rw.ladder[i-1]
	}
	return best
}

// stepAt is the first step standing at or past to, and whether the run has reached it.
func (rw *rewinder) stepAt(to event.Stamp) (int, bool) {
	i, _ := slices.BinarySearchFunc(rw.trail, to, tickOrder)
	if i == len(rw.trail) {
		return len(rw.trail) - 1, false
	}
	return i, true
}

// pump starts the next job when none runs: a wanted move once an idle copy is
// ready, else the idle copy's build.
func (rw *rewinder) pump() {
	if rw.busy || rw.closing || rw.build == nil {
		return
	}
	if mv := rw.want; mv != nil && rw.idle != nil {
		c, ck := rw.idle, rw.nearest(mv.step)
		rw.want, rw.idle, rw.running, rw.busy = nil, nil, mv, true
		if ck != nil {
			c.steps = ck.steps
		}
		p := rw.plan(c)
		p.step, p.cancel = mv.step, &mv.cancel
		core.Go(func() {
			var (
				err error
				b   batch
			)
			if ck != nil {
				err = c.a.restore(ck, c.src.d)
			}
			if err == nil {
				b = c.advance(p)
			}
			rw.results <- func() { rw.moved(c, mv, ck, err, b) }
		})
		return
	}
	if rw.idle != nil {
		return
	}
	retired := rw.retired
	rw.retired, rw.busy = nil, true
	build := rw.build
	core.Go(func() {
		for _, a := range retired {
			a.Close()
		}
		a, src, err := build()
		rw.results <- func() { rw.built(a, src, err) }
	})
}

func (rw *rewinder) built(a *App, src pacedSource, err error) {
	rw.busy = false
	switch {
	case err != nil:
		rw.shown.a.log.Warn("journal", "msg", "replay step back failed", "error", err.Error())
		rw.build, rw.lost, rw.want = nil, err, nil
	case rw.closing:
		rw.retired = append(rw.retired, a)
	default:
		rw.idle = &replayCopy{a: a, src: src.(journalSource)}
	}
	rw.pump()
}

// moved takes a move's copy back: presented next frame, or replaced by a move from
// the start when it could not be trusted.
func (rw *rewinder) moved(c *replayCopy, mv *move, ck *checkpoint, err error, b batch) {
	rw.busy, rw.running = false, nil
	from := 0
	if ck != nil {
		from = ck.steps
	}
	if err != nil {
		rw.drop("replay checkpoint left the run", err)
	} else {
		rw.learn(from, b)
	}
	switch {
	case mv.cancel.Load() || rw.closing:
		rw.retired = append(rw.retired, c.a)
	case err != nil || b.mismatch != nil:
		rw.retired = append(rw.retired, c.a)
		if rw.want == nil {
			rw.want = &move{step: mv.step}
		}
	default:
		rw.ready = c
	}
	rw.pump()
}

// cancel drops a move the viewer no longer wants, and a copy one positioned.
func (rw *rewinder) cancel() {
	rw.want = nil
	if rw.running != nil {
		rw.running.cancel.Store(true)
	}
	if rw.ready != nil {
		rw.retired = append(rw.retired, rw.ready.a)
		rw.ready = nil
	}
}

// close waits out the job in flight and closes every copy, handing the flight
// recorder back to the run that lent the terminal first.
func (rw *rewinder) close() {
	if rw.closing {
		return
	}
	rw.closing = true
	rw.cancel()
	for rw.busy || rw.lent {
		(<-rw.results)()
	}
	for _, c := range []*replayCopy{rw.idle, rw.ready} {
		if c != nil {
			rw.retired = append(rw.retired, c.a)
		}
	}
	rw.idle, rw.ready = nil, nil
	if a := rw.shown.a; a != rw.lender {
		rw.lender.world.Resources.Status.TakeRecorder(a.world.Resources.Status)
		rw.retired = append(rw.retired, a)
	}
	for _, a := range rw.retired {
		a.Close()
	}
	rw.retired = nil
}
