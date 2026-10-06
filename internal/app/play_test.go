//go:build !vif_headless

package app

import (
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/bot"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/snapshot"
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
	d, err := newReplayDriver(a, capture.Records(), nil)
	if err != nil {
		t.Fatal(err)
	}
	d.FinishAt(capture.End())
	a.AttachTransport(replayPort{id: 1})
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

// TestAReplayKeepsItsTrailingCopiesAcrossACheckpoint: at 1x the copies trailing the
// presented run cross each checkpoint it passes a few steps behind it. Rebuilding
// them there put an App's construction on the frame loop for each tick of the ladder.
func TestAReplayKeepsItsTrailingCopiesAcrossACheckpoint(t *testing.T) {
	capture := journal.NewCapture()
	source, _ := playBot(t, "default", fixtureSeed, 3*parameter.ReplayCheckpointSteps, capture)
	source.Close()
	cfg, err := ConfigFromAnchor(capture.Anchors()[0])
	if err != nil {
		t.Fatal(err)
	}
	end := capture.End()
	set := journal.Set{Anchors: capture.Anchors(), Records: capture.Records(), Digests: capture.Digests(), End: &end}
	a, err := NewHeadless(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	d, err := replayDriver(a, set)
	if err != nil {
		t.Fatal(err)
	}
	built := 0
	p := &player{a: a, src: journalSource{d, a.log}, every: parameter.ReplayCheckpointSteps,
		trail: []event.Stamp{a.Position()}, slots: make(chan struct{}, 1)}
	p.rebuild = func() (*App, pacedSource, error) {
		built++
		twin, err := NewHeadless(cfg)
		if err != nil {
			return nil, nil, err
		}
		td, err := replayDriver(twin, set)
		return twin, journalSource{td, twin.log}, err
	}
	defer p.closeRebuilt()
	for p.tickOnce() {
		p.keep()
		// At 1x the copies park between the presented ticks
		for _, r := range p.spares {
			for ok, err := r.parked(); !ok && err == nil; ok, err = r.parked() {
				time.Sleep(time.Millisecond)
			}
		}
	}
	if len(p.checkpoints) < 2 || built != parameter.ReplayBackSpares {
		t.Fatalf("across %d checkpoints the ladder of %d was built %d times",
			len(p.checkpoints), parameter.ReplayBackSpares, built)
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
	d, err := newReplayDriver(a, rec.Records(), rec.Captures())
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
