//go:build !vif_headless

package app

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/bot"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/internal/status"
)

// TestReplayViewStopsAtTheMapEdges is the scroll rule: a map the viewer's view holds
// is centred and does not move, a larger one is shown from the recorded view and
// scrolls no further than its edges, and pan pushed past an edge is not banked.
func TestReplayViewStopsAtTheMapEdges(t *testing.T) {
	t.Parallel()

	if cam, off, kept := viewAxis(0, 80, 100, 60, 7); cam != 0 || off != 20 || kept != 0 {
		t.Fatalf("a map the view holds: camera %d offset %d pan %d, want 0 20 0", cam, off, kept)
	}
	if cam, off, _ := viewAxis(50, 100, 100, 200, 0); cam != 50 || off != 0 {
		t.Fatalf("an unpanned view the recorded size: camera %d offset %d, want the recorded 50 0", cam, off)
	}
	// A 60-cell view on a 100-cell recording at camera 50 starts from its centre, 70.
	cam, _, kept := viewAxis(50, 100, 60, 200, 1000)
	if cam != 140 || kept != 70 {
		t.Fatalf("panned past the far edge: camera %d pan %d, want 140 70", cam, kept)
	}
	if cam, _, _ = viewAxis(50, 100, 60, 200, kept-panStep); cam != 140-panStep {
		t.Fatalf("one step back from the far edge: camera %d, want %d", cam, 140-panStep)
	}
	if cam, _, kept = viewAxis(50, 100, 60, 200, -1000); cam != 0 || kept != -70 {
		t.Fatalf("panned past the near edge: camera %d pan %d, want 0 -70", cam, kept)
	}
}

// TestReplayViewerCommandLineReturnsTheRecordedState is the rule the viewer's command
// line keeps: it inspects the recording and changes none of it, so the world neither
// pauses nor hears of the mode it borrowed, which is the recording's again once it closes.
func TestReplayViewerCommandLineReturnsTheRecordedState(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, fixtureSeed, 100, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	if !a.Inject(intentModeSwitch(input.ModeTargetInsert)) {
		t.Fatal("insert quit the game")
	}

	p := &player{a: a}
	press := func(ev terminal.Event) {
		t.Helper()
		ev.Type = terminal.EventKey
		if !p.key(ev) {
			t.Fatalf("key %+v quit the replay", ev)
		}
	}
	energy := func() (v int64) {
		a.World().RunSafe(func() {
			if e, ok := a.World().Components.Energy.GetPtr(a.World().Resources.Player.Entity); ok {
				v = e.Current
			}
		})
		return v
	}

	// An untouched twin of the recording, which the viewer's run must keep matching
	twin := mustHeadless(t, fixtureSeed, 100, 40)
	defer twin.Close()
	tickUntilCursor(t, twin)
	if !twin.Inject(intentModeSwitch(input.ModeTargetInsert)) {
		t.Fatal("insert quit the twin")
	}
	press(terminal.Event{Key: terminal.KeyRune, Rune: ':'})
	if p.cmd == nil || !a.Context().IsCommandMode() || a.Context().TimeCtl.IsPaused() {
		t.Fatal("':' did not open a command line that leaves the recorded clock alone")
	}
	before := energy()
	for _, r := range "energy 12345" {
		press(terminal.Event{Key: terminal.KeyRune, Rune: r})
	}
	press(terminal.Event{Key: terminal.KeyEnter})
	if energy() != before || !strings.Contains(a.Context().GetStatusMessage(), "replay") {
		t.Fatalf("a viewer's :energy ran: energy %d, was %d; bar %q", energy(), before, a.Context().GetStatusMessage())
	}
	if p.cmd != nil || a.Context().Viewer.Load() {
		t.Fatal("the command line stayed the viewer's after it closed")
	}
	if a.Context().GetMode() != core.ModeInsert || a.Context().TimeCtl.IsPaused() {
		t.Fatalf("closed into mode %d paused %t, want the recorded INSERT, running",
			a.Context().GetMode(), a.Context().TimeCtl.IsPaused())
	}
	press(terminal.Event{Key: terminal.KeyCtrlG})
	if p.cmd == nil || !a.ctx.Viewer.Load() || a.ctx.GetOverlayContent().Menu == nil {
		t.Fatal("Ctrl-G did not open the viewer's menu")
	}
	press(terminal.Event{Key: terminal.KeyCtrlG})
	if p.cmd != nil || a.ctx.Viewer.Load() || a.ctx.GetMode() != core.ModeInsert || a.ctx.TimeCtl.IsPaused() {
		t.Fatal("closing the menu failed to return the recorded mode and pause")
	}
	a.Tick(60)
	twin.Tick(60)
	if i, _, _, ok := snapshot.FirstDiff(twin.SnapshotSimulation(), a.SnapshotSimulation()); ok {
		t.Fatalf("the viewer's command line and menu changed the recorded run, at line %d:\n%s", i,
			strings.Join(snapshot.Diff(twin.SnapshotSimulation(), a.SnapshotSimulation(), 8), "\n"))
	}
}

func TestAWatchingBotOperatorCanResetAndSeatBotsWhileTheSessionKeepsTicking(t *testing.T) {
	a := mustHeadless(t, fixtureSeed, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	graph, err := loadBotGraph(a.cfg.Resources, DefaultBotGraph)
	if err != nil {
		t.Fatal(err)
	}
	d, err := bot.NewDriver(a, a.ctx, graph, a.Seed(), a.localParticipant())
	if err != nil {
		t.Fatal(err)
	}
	p := &player{a: a, src: botSource{d}, interactive: true, interval: parameter.GameUpdateInterval,
		rec: engine.ScaleNormal, scale: engine.ScaleNormal}
	press := func(key terminal.Key, char rune) {
		t.Helper()
		if !p.key(terminal.Event{Type: terminal.EventKey, Key: key, Rune: char}) {
			t.Fatal("operator quit")
		}
	}
	command := func(s string) {
		press(terminal.KeyRune, ':')
		for _, char := range s {
			press(terminal.KeyRune, char)
		}
		press(terminal.KeyEnter, 0)
	}
	before := a.Position().Run
	command("n")
	p.advance(parameter.GameUpdateInterval)
	if a.Position().Run <= before || strings.Contains(a.ctx.GetStatusMessage(), "replay") {
		t.Fatalf("new game failed: %s, position %+v", a.ctx.GetStatusMessage(), a.Position())
	}
	command("bot add 1:patrol")
	if a.HostAddr() == "" {
		t.Fatalf("bot add did not host: %s", a.ctx.GetStatusMessage())
	}
	press(terminal.KeyRune, ':')
	if p.cmd == nil || a.ctx.Viewer.Load() {
		t.Fatal("live bot controls are still a replay viewer")
	}
	beforeTick := a.Position().Tick
	for deadline := time.Now().Add(socketWait); a.world.Resources.Player.Count() != 2 && time.Now().Before(deadline); {
		p.advance(parameter.GameUpdateInterval)
		time.Sleep(parameter.GameUpdateInterval)
	}
	if a.Position().Tick <= beforeTick {
		t.Fatal("the command line stopped the session clock")
	}
	if a.world.Resources.Player.Count() != 2 {
		t.Fatalf("the requested bot did not join: %s", a.ctx.GetStatusMessage())
	}
	press(terminal.KeyEscape, 0)
}

func TestNetworkJournalPlaybackRemainsBoundedAndPausable(t *testing.T) {
	capture := journal.NewCapture()
	source, _ := playBot(t, "default", fixtureSeed, 20, capture)
	source.Close()
	a := mustHeadless(t, fixtureSeed, 120, 40)
	defer a.Close()
	end := capture.End()
	d, err := newReplayDriver(a, journal.Set{Records: capture.Records(), End: &end}.Stream())
	if err != nil {
		t.Fatal(err)
	}
	a.AttachTransport(network.NewMesh().Node(1))
	p := &player{a: a, src: journalSource{d, a.log}, interval: parameter.GameUpdateInterval,
		rec: engine.ScaleNormal, scale: engine.ScaleNormal}
	p.key(terminal.Event{Key: terminal.KeyRune, Rune: ' '})
	if !p.paused || p.live {
		t.Fatal("recorded network participant was treated as a live bot")
	}
	p.key(terminal.Event{Key: terminal.KeyRune, Rune: ' '})
	p.advance(40 * parameter.GameUpdateInterval)
	if !p.done || p.err != nil || a.Position().Tick != capture.End().Tick {
		t.Fatalf("playback end: done=%v err=%v position=%v", p.done, p.err, a.Position())
	}
	p.advance(time.Second)
	if a.Position().Tick != capture.End().Tick || !strings.HasPrefix(a.ctx.GetStatusMessage(), "END") {
		t.Fatal("finished replay advanced or lost its END status")
	}
}

// replayPlayer presents a journal as PlayJournal does, on headless copies; edit
// changes the configuration of the run that lends the terminal.
func replayPlayer(t *testing.T, rec *journal.Capture, edit func(*Config)) *player {
	t.Helper()
	cfg, err := ConfigFromAnchor(rec.Anchors()[0])
	if err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(&cfg)
	}
	end := rec.End()
	stream := journal.Set{Records: rec.Records(), Captures: rec.Captures(), Digests: rec.Digests(), End: &end}.Stream()
	a, err := NewHeadless(cfg)
	if err != nil {
		t.Fatal(err)
	}
	d, err := newReplayDriver(a, stream)
	if err != nil {
		t.Fatal(err)
	}
	shown := &replayCopy{a: a, src: journalSource{d, a.log}}
	p := &player{a: a, src: shown.src, interval: parameter.GameUpdateInterval,
		rec: engine.ScaleNormal, scale: engine.ScaleNormal}
	p.rw = newRewinder(shown, a, replayCopies(a, cfg, stream, NewHeadless))
	t.Cleanup(func() {
		p.rw.close()
		a.Close()
	})
	return p
}

// playSteps plays the presented copy n steps on, as '.' grants them.
func playSteps(p *player, n int) {
	p.paused, p.step = true, n
	for p.step > 0 && !p.done {
		p.advance(0)
	}
}

// settle runs frames until what the viewer asked for is presented and every job off
// the frame loop has returned.
func settle(t *testing.T, p *player) {
	t.Helper()
	for deadline := time.Now().Add(time.Minute); p.seek != nil || p.backs > 0 || p.rw.busy || p.rw.ready != nil; {
		if time.Now().After(deadline) {
			t.Fatalf("the replay never settled: seek %+v backs %d busy %t", p.seek, p.backs, p.rw.busy)
		}
		select {
		case apply := <-p.rw.results:
			apply()
		default:
			p.advance(0)
			time.Sleep(time.Millisecond)
		}
	}
}

// TestAReplayStepsBackOneTickAPressFromItsRing: each step back presents the tick
// before the one shown. The last ring of steps restores without replaying, and one
// further back replays from the ladder and refills the ring as it goes.
func TestAReplayStepsBackOneTickAPressFromItsRing(t *testing.T) {
	t.Parallel()
	rec := journal.NewCapture()
	source, _ := playBot(t, "default", fixtureSeed, parameter.ReplayCheckpointSteps+60, rec)
	source.Close()
	p := replayPlayer(t, rec, nil)
	const shown = parameter.ReplayCheckpointSteps + 40
	playSteps(p, shown)
	settle(t, p)
	for i := 1; i <= 2*parameter.ReplayRing+2; i++ {
		p.control(',')
		settle(t, p)
		if got, want := p.rw.shown.steps, shown-i; got != want || p.a.Position() != p.rw.trail[want] {
			t.Fatalf("press %d presents step %d at %+v, want step %d at %+v", i, got, p.a.Position(), want, p.rw.trail[want])
		}
	}
	if p.rw.every == 0 || len(p.rw.trail) != shown+1 {
		t.Fatalf("stepping back dropped the checkpoints (every %d) or rewrote the run (%d steps)", p.rw.every, len(p.rw.trail)-1)
	}
}

// TestAReplaySeekLandsOnTheFirstStepAtOrPastItsTick: a seek names a tick of the run
// shown and lands on the first step there or after, behind, ahead, and past what the
// replay has reached; a tick past its run's end lands on the next run's start.
func TestAReplaySeekLandsOnTheFirstStepAtOrPastItsTick(t *testing.T) {
	t.Parallel()
	data, err := fs.ReadFile(asset.DefaultBots, "default.toml")
	if err != nil {
		t.Fatal(err)
	}
	graph, err := bot.ParseGraph("default", data)
	if err != nil {
		t.Fatal(err)
	}
	rec := journal.NewCapture()
	a, err := NewHeadless(Config{Seed: 0xC1, Width: 120, Height: 40,
		Resources: resource.Options{Embedded: true}, Journal: true, JournalSink: rec})
	if err != nil {
		t.Fatal(err)
	}
	d, err := bot.NewDriver(a, a.ctx, graph, a.Seed(), a.localParticipant())
	if err != nil {
		t.Fatal(err)
	}
	for i := range 300 {
		if i == 150 {
			a.Reset(false)
		}
		if more, err := d.Step(); !more || err != nil {
			t.Fatalf("bot: more %v, err %v", more, err)
		}
	}
	a.Close()

	p := replayPlayer(t, rec, nil)
	lands := func(tick uint64, want event.Stamp) {
		t.Helper()
		p.goTo(tick)
		settle(t, p)
		if at := p.a.Position(); tickOrder(at, want) != 0 || !p.paused {
			t.Fatalf(":r tick %d presents %+v (paused %t), want run %d tick %d", tick, at, p.paused, want.Run, want.Tick)
		}
	}
	lands(80, event.Stamp{Tick: 80}) // ahead, past what the replay reached
	lands(30, event.Stamp{Tick: 30}) // behind
	lands(60, event.Stamp{Tick: 60}) // ahead, inside what it reached
	p.goTo(1 << 20)                  // past run 0's end
	settle(t, p)
	first := p.rw.trail[slices.IndexFunc(p.rw.trail, func(s event.Stamp) bool { return s.Run == 1 })]
	if at := p.a.Position(); tickOrder(at, first) != 0 {
		t.Fatalf("a tick past run 0's end presents %+v, want run 1's first step %+v", at, first)
	}
	lands(first.Tick+40, event.Stamp{Run: 1, Tick: first.Tick + 40})
	lands(first.Tick+10, event.Stamp{Run: 1, Tick: first.Tick + 10})
}

// TestACopyThatLeftTheRunIsNeverPresented: a copy restored from a checkpoint that
// does not reproduce its world, or that stands where the run did not, is replaced by
// one replayed from the start; no checkpoint is trusted after, and the bar does not
// claim a divergence the recording does not have.
func TestACopyThatLeftTheRunIsNeverPresented(t *testing.T) {
	t.Parallel()
	rec := journal.NewCapture()
	source, _ := playBot(t, "default", fixtureSeed, parameter.ReplayCheckpointSteps+60, rec)
	source.Close()
	const shown = parameter.ReplayCheckpointSteps + 40
	for name, spoil := range map[string]func(p *player) int{
		"digest": func(p *player) int {
			p.rw.ring[(shown-1)%parameter.ReplayRing].digest.Positions ^= 1
			p.control(',')
			return shown - 1
		},
		"trail": func(p *player) int {
			p.rw.trail[parameter.ReplayCheckpointSteps+10].Tick += 1000
			p.goTo(p.rw.trail[parameter.ReplayCheckpointSteps+20].Tick)
			return parameter.ReplayCheckpointSteps + 20
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := replayPlayer(t, rec, nil)
			playSteps(p, shown)
			settle(t, p)
			trail := slices.Clone(p.rw.trail)
			want := spoil(p)
			settle(t, p)
			if got := p.a.Position(); p.rw.shown.steps != want || got != trail[want] {
				t.Fatalf("presents step %d at %+v, want step %d at %+v", p.rw.shown.steps, got, want, trail[want])
			}
			if p.rw.every != 0 || p.rw.ladder != nil || p.rw.nearest(shown) != nil {
				t.Fatal("checkpoints are still trusted after one left the run")
			}
			p.report()
			if bar := p.a.ctx.GetStatusMessage(); strings.Contains(bar, "diverged") {
				t.Fatalf("the bar claims a divergence the recording does not have: %q", bar)
			}
		})
	}
}

// TestOnlyThePresentedReplayCopyHoldsTheFlightRecorder: a crash or race flush
// reaches the one process-wide recorder, so it belongs to the copy presented. A copy
// built off the frame loop holds none, and the run that lent the terminal gets it back.
func TestOnlyThePresentedReplayCopyHoldsTheFlightRecorder(t *testing.T) {
	const depth = 50
	capture := journal.NewCapture()
	source, _ := playBot(t, "default", fixtureSeed, 60, capture)
	source.Close()
	p := replayPlayer(t, capture, func(c *Config) { c.RecTicks = depth })
	lender := p.a
	playSteps(p, 20)
	p.control(',')
	settle(t, p)
	depthOf := func(x *App) int { return x.world.Resources.Status.RecorderDepth() }
	if p.a == lender || depthOf(p.a) != depth || depthOf(lender) != 0 || !status.RecorderActive() {
		t.Fatalf("after a step back the presented copy records %d ticks, the lender %d", depthOf(p.a), depthOf(lender))
	}
	if depthOf(p.rw.idle.a) != 0 {
		t.Fatalf("the idle copy records %d ticks", depthOf(p.rw.idle.a))
	}
	p.rw.close()
	if depthOf(lender) != depth {
		t.Fatalf("the lender records %d ticks after the copies closed, want %d", depthOf(lender), depth)
	}
}

// TestAPresentedReplayStepIsOneRecordedTick: a correction a guest wrote between two
// ticks took none of the recorded run's time. Presented as a step of its own, it
// held the view a tick each time one was installed.
func TestAPresentedReplayStepIsOneRecordedTick(t *testing.T) {
	rec := guestJournalPastItsHost(t, func(*App) {})
	cfg, err := ConfigFromAnchor(rec.Anchors()[0])
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewHeadless(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	d, err := newReplayDriver(a, journal.Set{Records: rec.Records(), Captures: rec.Captures()}.Stream())
	if err != nil {
		t.Fatal(err)
	}
	src := journalSource{d, a.log}
	for {
		from := a.Position()
		more, err := src.Step()
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
		if at := a.Position(); at.Run == from.Run && at.Tick == from.Tick {
			t.Fatalf("a presented step stood at run %d tick %d", at.Run, at.Tick)
		}
	}
	if st := d.Stats(); st.Installed < 2 {
		t.Fatalf("the guest journal installed %d worlds; it exercised no correction", st.Installed)
	}
}
