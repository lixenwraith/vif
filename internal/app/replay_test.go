package app

import (
	"errors"
	"flag"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// fixtureSeed pins the perturbation test so CI is reproducible
const fixtureSeed = 0x5eed1e55

// scriptConfig builds a hermetic headless config: the embedded resources pin the
// FSM and corpus, so a run does not depend on cwd or any external config root
func scriptConfig(seed uint64) Config {
	return Config{Mode: ModeHeadless, Resources: resource.Options{Embedded: true}, Seed: seed}
}

func intentMotion(op input.MotionOp, count int) *input.Intent {
	return &input.Intent{Type: input.IntentMotion, Motion: op, Count: count}
}
func intentModeSwitch(target input.ModeTarget) *input.Intent {
	return &input.Intent{Type: input.IntentModeSwitch, ModeTarget: target, Count: 1}
}
func intentTextChar(c rune) *input.Intent {
	return &input.Intent{Type: input.IntentTextChar, Char: c, Count: 1}
}
func intentEscape() *input.Intent { return &input.Intent{Type: input.IntentEscape, Count: 1} }
func intentSpecial(op input.SpecialOp, count int) *input.Intent {
	return &input.Intent{Type: input.IntentSpecial, Special: op, Count: count}
}
func intentOverlayClose() *input.Intent {
	return &input.Intent{Type: input.IntentOverlayClose, Count: 1}
}
func intentMarkerShow(op input.MotionOp) *input.Intent {
	return &input.Intent{Type: input.IntentMotionMarkerShow, Motion: op, Count: 1}
}

// intentCommandBody types cmd into command mode and executes it. The leading colon
// is the mode switch, not part of the text ExecuteCommand parses.
func intentCommandBody(cmd string) []*input.Intent {
	out := make([]*input.Intent, 0, len(cmd)+1)
	for _, c := range cmd {
		out = append(out, intentTextChar(c))
	}
	return append(out, &input.Intent{Type: input.IntentTextConfirm, Count: 1})
}

// scriptRunner drives intents at tick boundaries and tracks the tick count.
// One Inject per boundary is a convention, not a requirement: TestReplaySplitSettle
// establishes that settle granularity inside a boundary is not observable.
type scriptRunner struct {
	t     *testing.T
	a     *App
	ticks int
}

func newScriptRunner(t *testing.T, a *App) *scriptRunner {
	t.Helper()
	r := &scriptRunner{t: t, a: a}
	r.step(1) // warmup: the tick-1 APM fold commits an empty bucket
	return r
}

func (r *scriptRunner) step(ticks int, intents ...*input.Intent) {
	r.t.Helper()
	if len(intents) > 0 && !r.a.Inject(intents...) {
		r.t.Fatal("script intent quit the game")
	}
	r.a.Tick(ticks)
	// Read the counter rather than accumulate: a reset re-bases it, and the
	// journal stamps ticks per run
	r.ticks = int(r.a.World().Resources.Game.State.GetGameTicks())
}

// done reports the tick total. No APM cap any more: admission moved to the event
// stream, so a fold reproduces like anything else.
func (r *scriptRunner) done() int { return r.ticks }

// runScript drives the gameplay sequence: motion, insert typing, delete, auto-fire,
// a command round trip, and Visual with an active shield — the only state that gives
// the player ping bounds a non-zero value to reproduce.
func runScript(t *testing.T, a *App) int {
	t.Helper()
	r := newScriptRunner(t, a)

	r.step(1, intentMotion(input.MotionRight, 5))       // EventCursorMoveRequest
	r.step(1, intentModeSwitch(input.ModeTargetInsert)) // EventModeChanged
	r.step(1, intentTextChar('i'), intentTextChar('f')) // pooled CharacterTypedPayload
	r.step(1, intentEscape())                           // EventModeChanged, back to Normal
	r.step(1, intentSpecial(input.SpecialDeleteChar, 2))

	// Auto-fire on its own boundary: InputTick settles what it emits, so sharing a
	// boundary with Inject would split one settle into two
	if !a.InputTick() {
		t.Fatal("script macro intent quit the game")
	}
	r.step(1)

	// Command mode: opens paused, so the next tick executes under a pause the
	// manual clock records but does not enforce
	r.step(1, intentModeSwitch(input.ModeTargetCommand))
	if !a.Context().TimeCtl.IsPaused() {
		t.Fatal("command mode did not pause: the script no longer covers paused-tick replay")
	}
	// EnergySystem.Update pushes EventShieldActivate and the tick's closing settle
	// applies it
	r.step(1, intentCommandBody("energy 500")...)

	r.step(1, intentModeSwitch(input.ModeTargetVisual))
	player := a.World().Resources.Player.Entity
	ping, ok := a.World().Components.Ping.GetComponent(player)
	if !ok || !ping.BoundsActive {
		t.Fatal("visual step did not activate ping bounds: the script no longer covers bounds replay")
	}
	// MotionMarkerSystem is the simulation-side consumer of ping bounds
	r.step(1, intentMarkerShow(input.MotionColoredGlyphRight))
	r.step(1, intentMotion(input.MotionDown, 3)) // bounds-scaled step, not a single row
	r.step(1, intentEscape())                    // bounds inactive again
	r.step(1, intentMotion(input.MotionRight, 2))

	return r.done()
}

// runOverlayScript drives the telemetry overlay through command mode and closes it.
// Every mode change and pause change on this path is an event; the overlay's own
// state (content, scroll, selection, pins) is view state and appears in no snapshot.
func runOverlayScript(t *testing.T, a *App) int {
	t.Helper()
	r := newScriptRunner(t, a)

	r.step(1, intentModeSwitch(input.ModeTargetCommand))
	r.step(1, intentCommandBody("t")...) // EventMetaTelemetryRequest, RequestMode(Overlay), pause
	if a.Context().GetMode() != core.ModeOverlay {
		t.Fatalf("mode %d after :t, want overlay", a.Context().GetMode())
	}
	if !a.Context().IsOverlayActive() {
		t.Fatal(":t built no overlay content")
	}
	r.step(1, intentOverlayClose())
	if a.Context().GetMode() != core.ModeNormal {
		t.Fatalf("mode %d after overlay close, want normal", a.Context().GetMode())
	}
	r.step(1, intentMotion(input.MotionRight, 3))

	return r.done()
}

// journalRun drives a script under a capture sink, returning the capture, the
// simulation snapshot, the drawn seed and the end position
func journalRun(t *testing.T, script func(*testing.T, *App) int) (*journal.Capture, []string, uint64, event.Stamp) {
	t.Helper()

	cap := journal.NewCapture()
	cfg := scriptConfig(0) // drawn seed: the anchor is its only channel to the replay
	cfg.Journal, cfg.JournalSink = true, cap

	a, err := NewHeadless(cfg)
	if err != nil {
		t.Fatalf("source run: %v", err)
	}
	script(t, a)
	snap, seed, end := a.SnapshotSimulation(), a.Seed(), a.Position()
	a.Close()

	if seed == 0 {
		t.Fatal("run drew no seed")
	}
	if err := cap.CheckDense(); err != nil {
		t.Fatal(err)
	}
	if len(cap.Records()) == 0 {
		t.Fatal("no records captured: the script produced no journaled events")
	}
	return cap, snap, seed, end
}

// replayInto rebuilds from the anchor, verifies it, replays and compares. Returns the
// divergence rather than failing, so a negative control can require one.
// root is where the replay looks for the scenario the anchor names, matching what
// PlayJournal takes from -config-dir. Empty for a run on the embedded scenario.
func replayInto(root string, anchors []event.JournalAnchor, recs []event.JournalRecord,
	want []string, end event.Stamp) error {

	if len(anchors) == 0 {
		return errors.New("no anchor captured")
	}
	rcfg, err := ConfigFromAnchor(anchors[0])
	if err != nil {
		return fmt.Errorf("config from anchor: %w", err)
	}
	rcfg.Resources.Dir = root
	rep, err := NewHeadless(rcfg)
	if err != nil {
		return fmt.Errorf("replay run: %w", err)
	}
	defer rep.Close()

	if err := rep.VerifyAnchor(anchors[0]); err != nil {
		return fmt.Errorf("verify anchor: %w", err)
	}

	st, err := rep.Replay(recs)
	if err != nil {
		return fmt.Errorf("replay: %w (%+v)", err, st)
	}
	if st.End.Run != end.Run {
		return fmt.Errorf("replay ended in run %d, source ended in run %d", st.End.Run, end.Run)
	}
	switch rest := int(end.Tick) - int(st.End.Tick); {
	case rest > 0:
		rep.Tick(rest)
	case rest < 0:
		return fmt.Errorf("replay ran %d ticks in run %d, source ran %d",
			st.End.Tick, st.End.Run, end.Tick)
	}

	got := rep.SnapshotSimulation()
	if i, _, _, ok := snapshot.FirstDiff(want, got); ok {
		return fmt.Errorf("diverged at line %d (%+v):\n%s",
			i, st, strings.Join(snapshot.Diff(want, got, 8), "\n"))
	}
	return nil
}

func replayAndCompare(t *testing.T, script func(*testing.T, *App) int) {
	t.Helper()
	cap, want, seed, end := journalRun(t, script)
	if a := cap.Anchors(); len(a) > 0 && a[0].Seed != seed {
		t.Fatalf("anchor seed %d, run seed %d", a[0].Seed, seed)
	}
	if err := replayInto("", cap.Anchors(), cap.Records(), want, end); err != nil {
		t.Fatal(err)
	}
}

// TestJournalDoesNotPerturb drives one fixed-seed sequence twice, journaled and
// not, and asserts the snapshots match and the capture is dense
func TestJournalDoesNotPerturb(t *testing.T) {
	t.Parallel()
	plain, err := NewHeadless(scriptConfig(fixtureSeed))
	if err != nil {
		t.Fatalf("plain run: %v", err)
	}
	runScript(t, plain)
	want := plain.Snapshot()
	plain.Close()

	cap := journal.NewCapture()
	cfg := scriptConfig(fixtureSeed)
	cfg.Journal, cfg.JournalSink = true, cap

	journaled, err := NewHeadless(cfg)
	if err != nil {
		t.Fatalf("journaled run: %v", err)
	}
	runScript(t, journaled)
	got := journaled.Snapshot()
	emitted, encodeFailed := journaled.JournalStats()
	journaled.Close()

	if i, x, y, ok := snapshot.FirstDiff(want, got); ok {
		t.Fatalf("journaling perturbed the run at line %d:\n  plain     %s\n  journaled %s", i, x, y)
	}
	if encodeFailed != 0 {
		t.Fatalf("encode failures: %d", encodeFailed)
	}
	if err := cap.CheckDense(); err != nil {
		t.Fatal(err)
	}
	if n := uint64(len(cap.Records())); n != emitted {
		t.Fatalf("sink saw %d records, journal emitted %d", n, emitted)
	}
}

// TestReplayReproducesRecordedRuns drives one authored script per record-stream shape
// and requires each to reproduce bit-identically: gameplay for mode, pause, command
// origin and ping bounds; overlay for the debug path, which is event-driven
// throughout; split-settle for the multi-settle tick boundaries App.Loop produces;
// and reset for the run boundaries a run-blind driver would reject outright.
func TestReplayReproducesRecordedRuns(t *testing.T) {
	t.Parallel()
	for name, script := range map[string]func(*testing.T, *App) int{
		"gameplay":     runScript,
		"overlay":      runOverlayScript,
		"split-settle": runSplitSettleScript,
		"reset":        runResetScript,
		"defeat":       runDefeatScript,
		"hold":         runHoldScript,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			replayAndCompare(t, script)
		})
	}
}

// TestAGuestJournalReplaysFromItsJoin records a guest from its join until after its
// host has left, then replays the journal with no host: the joined world, each
// correction written, the owner syncs and the session's end all come from records,
// and so does the pause the guest could take only once its session had ended.
func TestAGuestJournalReplaysFromItsJoin(t *testing.T) {
	rec, want := guestJournalPastItsHost(t)

	// Compared only on ticks nothing was recorded on: there the live guest had not
	// yet consumed what the replay injects after the tick.
	busy := map[event.Stamp]bool{}
	for _, r := range rec.Records() {
		busy[event.Stamp{Run: r.Run, Tick: r.Tick}] = true
	}
	for _, c := range rec.Captures() {
		busy[event.Stamp{Run: c.Run, Tick: c.Tick}] = true
	}
	cfg, err := ConfigFromAnchor(rec.Anchors()[0])
	if err != nil {
		t.Fatalf("config from anchor: %v", err)
	}
	rep, err := NewHeadless(cfg)
	if err != nil {
		t.Fatalf("replay app: %v", err)
	}
	defer rep.Close()
	d, err := newReplayDriver(rep, journal.Set{Records: rec.Records(), Captures: rec.Captures()}.Stream())
	if err != nil {
		t.Fatalf("replay driver: %v", err)
	}
	compared := 0
	for {
		more, err := d.Step()
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if !more {
			break
		}
		p := rep.Position()
		at := event.Stamp{Run: p.Run, Tick: p.Tick}
		w, ok := want[at]
		if !ok || busy[at] {
			continue
		}
		compared++
		got := guestSurface(rep)
		if i, _, _, ok := snapshot.FirstDiff(w, got); ok {
			t.Fatalf("diverged at tick %d at line %d:\n%s\nrecords on the tick before:\n%s", p.Tick, i,
				strings.Join(snapshot.Diff(w, got, 12), "\n"), strings.Join(recordsAt(rec.Records(), p.Run, p.Tick-1), "\n"))
		}
	}
	if st := d.Stats(); st.Installed < 2 || compared < 100 {
		t.Fatalf("replay installed %d worlds and compared %d ticks: the run exercised too little", st.Installed, compared)
	}
}

// guestJournal is recorded once for every test that reads it back.
var guestJournal struct {
	once     sync.Once
	rec      *journal.Capture
	surfaces map[event.Stamp][]string
}

// guestJournalPastItsHost is a guest's journal from its join until after its host
// has left, with one of its crossings late at the host, so it holds a correction
// written between two ticks, and a pause the guest takes alone; and the guest's
// surface after every tick.
func guestJournalPastItsHost(t *testing.T) (*journal.Capture, map[event.Stamp][]string) {
	t.Helper()
	guestJournal.once.Do(func() { guestJournal.rec, guestJournal.surfaces = recordGuestPastItsHost(t) })
	if guestJournal.rec == nil {
		t.Fatal("the shared guest journal did not record")
	}
	return guestJournal.rec, guestJournal.surfaces
}

func recordGuestPastItsHost(t *testing.T) (*journal.Capture, map[event.Stamp][]string) {
	t.Helper()
	const seed, hostLeaves = 0x3017, 400
	host := mustHeadless(t, seed, 120, 40)
	defer host.Close()
	tickUntilCursor(t, host)
	host.Tick(240)
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("host: %v", err)
	}
	stop := tickInBackground(host)
	rec := journal.NewCapture()
	guest, _ := mustSocketJoiner(t, host.HostAddr(), seed, 120, 40, func(c *Config) {
		c.Journal, c.JournalSink = true, rec
	})
	stop()
	waitForRosterPair(t, host, guest)

	surfaces := map[event.Stamp][]string{}
	rng := vmath.NewFastRand(3)
	motions := []input.MotionOp{input.MotionLeft, input.MotionRight, input.MotionUp, input.MotionDown}
	for i := range hostLeaves + 300 {
		if i == hostLeaves {
			host.Close()
		}
		if i == hostLeaves/4 {
			// One guest crossing late at the host by construction, whatever lead the
			// guest has adopted: the host is that lead and a delay ahead when the
			// crossing is made, so it applies it where it lands and the guest has a
			// correction to install from the record.
			ahead := int(guest.World().Resources.Network.BarrierDelayTicks) + parameter.NetworkBarrierDelayTicks
			written := len(rec.Captures())
			host.Tick(ahead)
			inject(t, guest, &input.Intent{Type: input.IntentFireMain, Count: 1}, intentMotion(input.MotionRight, 2))
			guest.Tick(ahead)
			// The correction crosses the socket on goroutines this loop does not pace,
			// so the host stays until the guest has written it, paced as
			// waitForRosterPair paces its ticks, however loaded the machine.
			deadline := time.Now().Add(socketWait) // [wall] a link bound, not a game one
			for len(rec.Captures()) == written {
				if !time.Now().Before(deadline) {
					t.Fatal("the guest wrote no correction before its host left")
				}
				host.Tick(1)
				guest.Tick(1)
				guest.ApplyPendingCorrections()
				time.Sleep(joinTestTickInterval / 5)
			}
		}
		if i == hostLeaves+150 || i == hostLeaves+200 {
			guest.Context().PushEventOrigin(event.EventGamePauseRequest,
				&event.GamePausePayload{Paused: i == hostLeaves+150}, event.OriginDebug)
			guest.Settle()
		}
		if rng.Intn(3) == 0 {
			inject(t, guest, intentMotion(motions[rng.Intn(4)], 1+rng.Intn(3)))
		}
		if rng.Intn(5) == 0 {
			inject(t, guest, &input.Intent{Type: input.IntentFireMain, Count: 1})
		}
		if i < hostLeaves {
			if rng.Intn(3) == 0 {
				inject(t, host, intentMotion(motions[rng.Intn(4)], 1+rng.Intn(3)))
			}
			host.Tick(1)
		}
		guest.Tick(1)
		guest.ApplyPendingCorrections()
		p := guest.Position()
		surfaces[event.Stamp{Run: p.Run, Tick: p.Tick}] = guestSurface(guest)
		if i == hostLeaves+150 && !guest.Context().TimeCtl.IsPaused() {
			t.Fatal("the guest was refused a pause after its session ended")
		}
	}
	if guest.World().PredictsShared() {
		t.Fatal("the guest still predicts after its host left")
	}
	guest.Close() // finishes the journal before any reader sees it
	return rec, surfaces
}

// TestAReplayNamesTheFirstDigestItDoesNotReproduce: a replay reproduces every digest
// its run wrote, and one that does not names that tick and the last that matched.
func TestAReplayNamesTheFirstDigestItDoesNotReproduce(t *testing.T) {
	t.Parallel()
	rec := journal.NewCapture()
	live, _ := playBot(t, "default", 0xD16E, 400, rec)
	live.Close()
	replay := func(digests []event.JournalDigest) journal.ReplayStats {
		cfg, err := ConfigFromAnchor(rec.Anchors()[0])
		if err != nil {
			t.Fatal(err)
		}
		a, err := NewHeadless(cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		end := rec.End()
		d, err := newReplayDriver(a, journal.Set{Records: rec.Records(), Captures: rec.Captures(),
			Digests: digests, End: &end}.Stream())
		if err != nil {
			t.Fatal(err)
		}
		if err := d.RunAll(); err != nil {
			t.Fatal(err)
		}
		return d.Stats()
	}
	digests := rec.Digests()
	if st := replay(digests); st.Diverged != nil || st.Digests != len(digests) || len(digests) < 10 {
		t.Fatalf("reproduced %d of %d digests: %v", st.Digests, len(digests), st.Diverged)
	}
	digests[7].Combat++
	v := replay(digests).Diverged
	if v == nil || v.At.Tick != digests[7].Tick || v.Since == nil || v.Since.Tick != digests[6].Tick ||
		!slices.Equal(v.Classes, []string{"combat"}) {
		t.Fatalf("divergence %+v, want tick %d combat after %d", v, digests[7].Tick, digests[6].Tick)
	}
}

// TestAJournalStartedMidRunReplaysFromItsWorld: :journal start opens a journal where
// the run stands without touching the shared world, the journal alone replays what
// the run played next, and a stop and a second start do it again.
func TestAJournalStartedMidRunReplaysFromItsWorld(t *testing.T) {
	t.Parallel()
	rng := vmath.NewFastRand(5)
	motions := []input.MotionOp{input.MotionLeft, input.MotionRight, input.MotionUp, input.MotionDown}
	a := mustHeadless(t, 0x7E5, 120, 40)
	defer a.Close()
	drive := func(ticks int, want map[event.Stamp][]string) {
		for range ticks {
			if rng.Intn(3) == 0 {
				inject(t, a, intentMotion(motions[rng.Intn(4)], 1+rng.Intn(3)))
			}
			if rng.Intn(5) == 0 {
				inject(t, a, &input.Intent{Type: input.IntentFireMain, Count: 1})
			}
			a.Tick(1)
			if want != nil {
				p := a.Position()
				want[event.Stamp{Run: p.Run, Tick: p.Tick}] = a.SnapshotSimulation()
			}
		}
	}
	for round, lead := range []int{12, 300} {
		drive(lead, nil)
		rec, want := journal.NewCapture(), map[event.Stamp][]string{}
		a.cfg.JournalSink = rec
		shared := a.SnapshotShared()
		if _, err := a.openJournal(); err != nil {
			t.Fatalf("round %d: open: %v", round, err)
		}
		if i, _, _, ok := snapshot.FirstDiff(shared, a.SnapshotShared()); ok {
			t.Fatalf("round %d: the opening moved the shared world at line %d:\n%s", round, i,
				strings.Join(snapshot.Diff(shared, a.SnapshotShared(), 12), "\n"))
		}
		drive(400, want)
		var err error
		a.World().RunSafe(func() { err = a.stopJournalLocked(-1) })
		a.serveJournal()
		if err != nil {
			t.Fatalf("round %d: stop: %v", round, err)
		}
		replayMatches(t, rec, want, func(rep *App) []string { return rep.SnapshotSimulation() })
	}
}

// TestAJournalOpenedOnAHoldReplaysItsEnd: an opening records a hold standing as it
// does the pause, so its replay stands held too and the :pause that ends the run's
// hold ends the replay's, telling its systems as the run's were told.
func TestAJournalOpenedOnAHoldReplaysItsEnd(t *testing.T) {
	t.Parallel()
	rng := vmath.NewFastRand(6)
	motions := []input.MotionOp{input.MotionLeft, input.MotionRight, input.MotionUp, input.MotionDown}
	a := mustHeadless(t, 0x7E6, 120, 40)
	defer a.Close()
	pause := func() {
		inject(t, a, intentModeSwitch(input.ModeTargetCommand))
		inject(t, a, intentCommandBody("pause")...)
	}
	a.Tick(60)
	pause()
	if !a.Context().TimeCtl.Held() {
		t.Fatal(":pause did not hold")
	}
	rec, want := journal.NewCapture(), map[event.Stamp][]string{}
	a.cfg.JournalSink = rec
	if _, err := a.openJournal(); err != nil {
		t.Fatalf("open: %v", err)
	}
	pause()
	for range 200 {
		if rng.Intn(3) == 0 {
			inject(t, a, intentMotion(motions[rng.Intn(4)], 1+rng.Intn(3)))
		}
		a.Tick(1)
		p := a.Position()
		want[event.Stamp{Run: p.Run, Tick: p.Tick}] = a.SnapshotSimulation()
	}
	var err error
	a.World().RunSafe(func() { err = a.stopJournalLocked(-1) })
	a.serveJournal()
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	replayMatches(t, rec, want, func(rep *App) []string { return rep.SnapshotSimulation() })
}

// TestANewGameThatWaitsReplaysItsStart: a :n the wait holds is journaled with it, so
// a replay stands where the run stood, input dropped as the run dropped it, until
// the press the run recorded starts the replay's game where the run's started.
func TestANewGameThatWaitsReplaysItsStart(t *testing.T) {
	t.Parallel()
	rng := vmath.NewFastRand(13)
	motions := []input.MotionOp{input.MotionLeft, input.MotionRight, input.MotionUp, input.MotionDown}
	a := mustHeadless(t, 0x7E7, 120, 40)
	defer a.Close()
	a.Context().WaitStart.Store(true)
	rec, want := journal.NewCapture(), map[event.Stamp][]string{}
	a.cfg.JournalSink = rec
	if _, err := a.openJournal(); err != nil {
		t.Fatalf("open: %v", err)
	}
	play := func(ticks int) {
		for range ticks {
			if rng.Intn(3) == 0 {
				inject(t, a, intentMotion(motions[rng.Intn(4)], 1+rng.Intn(3)))
			}
			a.Tick(1)
			p := a.Position()
			want[event.Stamp{Run: p.Run, Tick: p.Tick}] = a.SnapshotSimulation()
		}
	}
	play(60)
	inject(t, a, intentModeSwitch(input.ModeTargetCommand))
	inject(t, a, intentCommandBody("n")...)
	play(20)
	if p := a.Position(); !a.Context().TimeCtl.Holds(engine.HoldStart) || p.Tick != 0 {
		t.Fatalf("waiting %v at %+v after :n, want the new game waiting at its first tick",
			a.Context().TimeCtl.Holds(engine.HoldStart), p)
	}
	if !startPress(a) {
		t.Fatal("the press did not start the new game")
	}
	play(200)
	var err error
	a.World().RunSafe(func() { err = a.stopJournalLocked(-1) })
	a.serveJournal()
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	replayMatches(t, rec, want, func(rep *App) []string { return rep.SnapshotSimulation() })
}

// TestAJournalOpenedMidRunDoesNotPerturb drives one input sequence twice, opening a
// journal mid-run on one of them, and requires the two to stand alike at the opening
// and on every tick after: the opening carries the run's own side, so nothing a
// fresh boot would have decided for it shows, then or later.
func TestAJournalOpenedMidRunDoesNotPerturb(t *testing.T) {
	t.Parallel()
	plain, journaled := mustHeadless(t, 0x7E5, 120, 40), mustHeadless(t, 0x7E5, 120, 40)
	defer plain.Close()
	defer journaled.Close()
	rng := vmath.NewFastRand(9)
	motions := []input.MotionOp{input.MotionLeft, input.MotionRight, input.MotionUp, input.MotionDown}
	alike := func(when string) {
		t.Helper()
		want, got := plain.SnapshotSimulation(), journaled.SnapshotSimulation()
		if i, _, _, ok := snapshot.FirstDiff(want, got); ok {
			t.Fatalf("%s the runs differ at line %d:\n%s", when, i, strings.Join(snapshot.Diff(want, got, 12), "\n"))
		}
		for id := range uint8(component.MaxTargetGroups) {
			if want, got := plain.World().Resources.Target.GetGroup(id), journaled.World().Resources.Target.GetGroup(id); want != got {
				t.Fatalf("%s target group %d differs: want %+v, got %+v", when, id, want, got)
			}
		}
	}
	drive := func(ticks int, check bool) {
		for range ticks {
			var in []*input.Intent
			switch rng.Intn(6) {
			case 0, 1:
				in = append(in, intentMotion(motions[rng.Intn(4)], 1+rng.Intn(3)))
			case 2:
				in = append(in, &input.Intent{Type: input.IntentFireMain, Count: 1})
			case 3:
				in = append(in, intentModeSwitch(input.ModeTargetInsert), intentTextChar(rune('a'+rng.Intn(26))), intentEscape())
			}
			for _, a := range []*App{plain, journaled} {
				inject(t, a, in...)
				a.Tick(1)
			}
			if check {
				alike(fmt.Sprintf("at tick %d", plain.Position().Tick))
			}
		}
	}
	drive(900, false)
	// A group an update set and no anchor holds, which only the run itself remembers
	for _, a := range []*App{plain, journaled} {
		a.World().RunSafe(func() {
			a.World().PushEventDomain(event.EventTargetGroupUpdate,
				&event.TargetGroupUpdatePayload{GroupID: 3, Type: component.TargetPosition, PosX: 7, PosY: 5}, core.DomainShared)
		})
		a.Tick(1)
	}
	alike("before the opening")
	journaled.cfg.JournalSink = journal.NewCapture()
	if _, err := journaled.openJournal(); err != nil {
		t.Fatalf("open: %v", err)
	}
	alike("at the opening")
	drive(400, true)
}

// replayMatches replays rec on a fresh run, which must reproduce every digest rec
// holds and, on each tick nothing was recorded on, the surface want holds for it.
func replayMatches(t *testing.T, rec *journal.Capture, want map[event.Stamp][]string, surface func(*App) []string) {
	t.Helper()
	busy := map[event.Stamp]bool{}
	for _, r := range rec.Records() {
		busy[event.Stamp{Run: r.Run, Tick: r.Tick}] = true
	}
	for _, c := range rec.Captures() {
		busy[event.Stamp{Run: c.Run, Tick: c.Tick}] = true
	}
	cfg, err := ConfigFromAnchor(rec.Anchors()[0])
	if err != nil {
		t.Fatalf("config from anchor: %v", err)
	}
	rep, err := NewHeadless(cfg)
	if err != nil {
		t.Fatalf("replay app: %v", err)
	}
	defer rep.Close()
	end := rec.End()
	d, err := newReplayDriver(rep, journal.Set{Records: rec.Records(), Captures: rec.Captures(),
		Digests: rec.Digests(), End: &end}.Stream())
	if err != nil {
		t.Fatalf("replay driver: %v", err)
	}
	compared := 0
	for more := true; more && d.Stats().Diverged == nil; {
		if more, err = d.Step(); err != nil {
			t.Fatalf("replay: %v", err)
		}
		p := rep.Position()
		at := event.Stamp{Run: p.Run, Tick: p.Tick}
		if w, ok := want[at]; ok && !busy[at] {
			compared++
			if got := surface(rep); !slices.Equal(w, got) {
				i, _, _, _ := snapshot.FirstDiff(w, got)
				t.Fatalf("diverged at tick %d at line %d:\n%s", p.Tick, i, strings.Join(snapshot.Diff(w, got, 12), "\n"))
			}
		}
	}
	if st := d.Stats(); st.Diverged != nil || st.Digests == 0 || compared < 50 {
		t.Fatalf("reproduced %d digests and compared %d ticks: %v", st.Digests, compared, st.Diverged)
	}
}

// TestASessionJournalsMidRunOnEitherSide: a host and its guest each open a journal
// where the session stands, and each journal replays from the world it opened on.
func TestASessionJournalsMidRunOnEitherSide(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	const seed = 0x3017
	host := mustHeadless(t, seed, 120, 40)
	defer host.Close()
	tickUntilCursor(t, host)
	host.Tick(240)
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("host: %v", err)
	}
	stop := tickInBackground(host)
	guest, _ := mustSocketJoiner(t, host.HostAddr(), seed, 120, 40)
	stop()
	waitForRosterPair(t, host, guest)

	rng := vmath.NewFastRand(9)
	motions := []input.MotionOp{input.MotionLeft, input.MotionRight, input.MotionUp, input.MotionDown}
	hostWant, guestWant := map[event.Stamp][]string{}, map[event.Stamp][]string{}
	play := func(ticks int, record bool) {
		for range ticks {
			for _, a := range []*App{host, guest} {
				if rng.Intn(3) == 0 {
					inject(t, a, intentMotion(motions[rng.Intn(4)], 1+rng.Intn(3)))
				}
				if rng.Intn(5) == 0 {
					inject(t, a, &input.Intent{Type: input.IntentFireMain, Count: 1})
				}
			}
			host.Tick(1)
			guest.Tick(1)
			guest.ApplyPendingCorrections()
			if record {
				for a, want := range map[*App]map[event.Stamp][]string{host: hostWant, guest: guestWant} {
					p := a.Position()
					want[event.Stamp{Run: p.Run, Tick: p.Tick}] = guestSurface(a)
				}
			}
		}
	}
	play(60, false)
	hostRec, guestRec := journal.NewCapture(), journal.NewCapture()
	host.cfg.JournalSink, guest.cfg.JournalSink = hostRec, guestRec
	for _, a := range []*App{host, guest} {
		if _, err := a.openJournal(); err != nil {
			t.Fatalf("open: %v", err)
		}
	}
	play(400, true)
	for _, a := range []*App{host, guest} {
		a.World().RunSafe(func() { _ = a.stopJournalLocked(-1) })
		a.serveJournal()
	}
	replayMatches(t, hostRec, hostWant, guestSurface)
	replayMatches(t, guestRec, guestWant, guestSurface)
}

// TestAJournalReplaysAcrossALeave: leaving writes the world as opening a session does
// and removes those left behind in the settle after it, so the journal of a guest, or
// of a host whose bot leaves and is seated again, open across its leave replays to
// the world it played on in.
func TestAJournalReplaysAcrossALeave(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	for _, side := range []string{"guest", "host with its bot"} {
		t.Run(side, func(t *testing.T) {
			host := mustHeadless(t, fixtureSeed, 120, 40)
			defer host.Close()
			tickUntilCursor(t, host)
			stop := tickInBackground(host)
			if err := host.BeginHosting("127.0.0.1:0"); err != nil {
				t.Fatalf("host: %v", err)
			}
			leaver, bots := host, 0
			if side != "guest" {
				var err error
				host.World().RunSafe(func() { err = (sessionControl{host}).AddBot("roam") })
				if err != nil {
					t.Fatalf("bot: %v", err)
				}
				bots = 1
			}
			waitForSeats(t, host, bots)
			guest, _ := mustSocketJoiner(t, host.HostAddr(), fixtureSeed, 120, 40)
			guest.cfg.JoinAddress = host.HostAddr()
			stop()
			waitForRosterPair(t, host, guest)
			if side == "guest" {
				leaver = guest
			}

			rng := vmath.NewFastRand(11)
			motions := []input.MotionOp{input.MotionLeft, input.MotionRight, input.MotionUp, input.MotionDown}
			want := map[event.Stamp][]string{}
			play := func(ticks int) {
				for range ticks {
					for _, a := range []*App{host, guest} {
						if rng.Intn(3) == 0 {
							inject(t, a, intentMotion(motions[rng.Intn(4)], 1+rng.Intn(3)))
						}
					}
					host.Tick(1)
					guest.Tick(1)
					guest.ApplyPendingCorrections()
					p := leaver.Position()
					want[event.Stamp{Run: p.Run, Tick: p.Tick}] = guestSurface(leaver)
				}
			}
			rec := journal.NewCapture()
			leaver.cfg.JournalSink = rec
			if _, err := leaver.openJournal(); err != nil {
				t.Fatalf("open: %v", err)
			}
			play(120)
			leaveAs(t, leaver)
			stop = tickInBackground(leaver)
			waitForSeats(t, leaver, bots)
			stop()
			play(120)
			leaver.World().RunSafe(func() { _ = leaver.stopJournalLocked(-1) })
			leaver.serveJournal()
			replayMatches(t, rec, want, guestSurface)
		})
	}
}

// TestAHeldSessionReplaysOnEitherSide: a session's hold and its end replay on host
// and guest alike, from a journal opened before the hold and one opened on it. Only
// the host's :pause asked for either; the guest's end came with the host's next tick,
// which its replay has no link to hear, so the guest ends its hold as its replay
// does: in the pass that dispatches the end, whatever input that pass carries.
func TestAHeldSessionReplaysOnEitherSide(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	const seed = 0x3022
	host := mustHeadless(t, seed, 120, 40)
	defer host.Close()
	tickUntilCursor(t, host)
	host.Tick(240)
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("host: %v", err)
	}
	stop := tickInBackground(host)
	guest, _ := mustSocketJoiner(t, host.HostAddr(), seed, 120, 40)
	stop()
	waitForRosterPair(t, host, guest)

	hostRec, guestRec := journal.NewCapture(), journal.NewCapture()
	host.cfg.JournalSink, guest.cfg.JournalSink = hostRec, guestRec
	open := func(a *App) {
		if _, err := a.openJournal(); err != nil {
			t.Fatalf("open: %v", err)
		}
	}
	rng := vmath.NewFastRand(11)
	motions := []input.MotionOp{input.MotionLeft, input.MotionRight, input.MotionUp, input.MotionDown}
	hostWant, guestWant := map[event.Stamp][]string{}, map[event.Stamp][]string{}
	play := func(ticks int) {
		for range ticks {
			for _, a := range []*App{host, guest} {
				if rng.Intn(3) == 0 {
					inject(t, a, intentMotion(motions[rng.Intn(4)], 1+rng.Intn(3)))
				}
			}
			host.Tick(1)
			guest.Tick(1)
			guest.ApplyPendingCorrections()
			for a, want := range map[*App]map[event.Stamp][]string{host: hostWant, guest: guestWant} {
				p := a.Position()
				want[event.Stamp{Run: p.Run, Tick: p.Tick}] = guestSurface(a)
			}
		}
	}
	held := func(want bool) {
		t.Helper()
		for deadline := time.Now().Add(socketWait); host.Context().TimeCtl.Held() != want ||
			guest.Context().TimeCtl.Held() != want; time.Sleep(joinTestTickInterval / 5) {
			if time.Now().After(deadline) {
				t.Fatalf("host held %v and guest held %v, want both %v", host.Context().TimeCtl.Held(),
					guest.Context().TimeCtl.Held(), want)
			}
			play(1)
		}
	}
	// Typed, as a player asks: the journal holds what the player's input pushed
	pause := func(body string) {
		inject(t, host, intentModeSwitch(input.ModeTargetCommand))
		inject(t, host, intentCommandBody(body)...)
	}
	open(host)
	play(100)
	pause("pause")
	held(true)
	open(guest)
	play(20)
	// Seen before any handler: the hold still stands when its end dispatches
	var ends []bool
	guest.SetDispatchTap(func(ev event.GameEvent) {
		if ev.Type == event.EventGameHoldRequest {
			ends = append(ends, guest.Context().TimeCtl.Held())
		}
	})
	pause("pause off")
	held(false)
	play(300)
	for _, a := range []*App{host, guest} {
		a.World().RunSafe(func() { _ = a.stopJournalLocked(-1) })
		a.serveJournal()
	}
	if !slices.Equal(ends, []bool{true}) {
		t.Fatalf("the guest's hold stood %v as each end dispatched, want [true]", ends)
	}
	replayMatches(t, hostRec, hostWant, guestSurface)
	replayMatches(t, guestRec, guestWant, guestSurface)
}

// TestAFrozenSurvivorReplaysAsItRan: a survivor journaled across its host's loss
// replays to the run it was. The replay stands still where the run froze, so the
// input the run took while its session stood counts alike on both.
func TestAFrozenSurvivorReplaysAsItRan(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x3024, 4, [][2]int{{1, 2}, {1, 3}, {1, 4}, {2, 3}, {2, 4}, {3, 4}})
	localCursors(t, apps)
	primeRetention(t, apps)
	quick, slow := apps[1:3], apps[3]
	recs := map[*App]*journal.Capture{}
	want := map[*App]map[event.Stamp][]string{}
	for i, a := range quick {
		recs[a], want[a] = journal.NewCapture(), map[event.Stamp][]string{}
		a.cfg.JournalSink = recs[a]
		if _, err := a.openJournal(); err != nil {
			t.Fatalf("participant %d: open: %v", i+2, err)
		}
	}
	rng := vmath.NewFastRand(13)
	motions := []input.MotionOp{input.MotionLeft, input.MotionRight, input.MotionUp, input.MotionDown}
	play := func(ticks int, in []*App) {
		for range ticks {
			for _, a := range quick {
				if rng.Intn(3) == 0 {
					inject(t, a, intentMotion(motions[rng.Intn(4)], 1+rng.Intn(3)))
				}
			}
			tickAll(in)
			for _, a := range quick {
				p := a.Position()
				want[a][event.Stamp{Run: p.Run, Tick: p.Tick}] = guestSurface(a)
			}
		}
	}
	play(100, apps)
	closeParticipant(apps[0])
	if !settleAuthority(t, quick, func() bool { return authorityOf(quick[1]).Authority == 2 }) {
		t.Fatalf("participant 3 never followed participant 2: %+v", authorityOf(quick[1]))
	}
	// Frozen until participant 4's notice arrives, and the command line still answers
	for _, a := range quick {
		inject(t, a, intentModeSwitch(input.ModeTargetCommand))
		inject(t, a, intentEscape())
	}
	play(4, quick)
	slow.ApplyPendingCorrections()
	play(300, apps[1:])
	for _, a := range quick {
		a.World().RunSafe(func() { _ = a.stopJournalLocked(-1) })
		a.serveJournal()
		replayMatches(t, recs[a], want[a], guestSurface)
	}
}

// TestARunTakesItsSessionWriteAsAnInstall: a run journaled across its :host takes
// the world it writes as the replay's install of it does, so what an install
// derives (the spatial gauges, what carriers publish as they load) stands alike on
// both, and the journal replays to the surface the run played on.
func TestARunTakesItsSessionWriteAsAnInstall(t *testing.T) {
	// Not parallel: hosting binds a real socket.
	a := mustHeadless(t, fixtureSeed, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	rec := journal.NewCapture()
	a.cfg.JournalSink = rec
	if _, err := a.openJournal(); err != nil {
		t.Fatalf("open: %v", err)
	}
	rng := vmath.NewFastRand(13)
	motions := []input.MotionOp{input.MotionLeft, input.MotionRight, input.MotionUp, input.MotionDown}
	want := map[event.Stamp][]string{}
	play := func(ticks int) {
		for range ticks {
			if rng.Intn(3) == 0 {
				inject(t, a, intentMotion(motions[rng.Intn(4)], 1+rng.Intn(3)))
			}
			a.Tick(1)
			p := a.Position()
			want[event.Stamp{Run: p.Run, Tick: p.Tick}] = guestSurface(a)
		}
	}
	play(60)
	if err := a.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("host: %v", err)
	}
	play(60)
	a.World().RunSafe(func() { _ = a.stopJournalLocked(-1) })
	a.serveJournal()
	replayMatches(t, rec, want, guestSurface)
}

// TestAReplayStandsWhereTheRunsWriteLeftIt: a write leaves its replay on the run's
// barrier and settle group. A host opens its session over a keystroke pushed and not
// yet settled, as a seat added from another goroutine does, and pushes another before
// the settle: both stay queued across the install. A journal it opens then does too.
func TestAReplayStandsWhereTheRunsWriteLeftIt(t *testing.T) {
	// Not parallel: hosting binds a real socket.
	rec := journal.NewCapture()
	a, err := NewHeadless(Config{Seed: fixtureSeed, Width: 120, Height: 40,
		Resources: resource.Options{Embedded: true}, Journal: true, JournalSink: rec})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	tickUntilCursor(t, a)
	// The glyph under the cursor: typing it moves the cursor, a crossing in a session
	typed := func() *input.Intent {
		var r rune
		a.World().RunSafe(func() {
			if pos, ok := a.World().LocalCursor(); ok {
				if e, ok := a.World().Positions.TopGlyphAt(pos.X, pos.Y); ok {
					g, _ := a.World().Components.Glyph.GetComponent(e)
					r = g.Rune
				}
			}
		})
		return intentTextChar(r)
	}
	for i := 0; typed().Char == 0; i++ {
		if i == 40 {
			t.Fatal("no glyph reached")
		}
		inject(t, a, intentMotion(input.MotionWordForward, 1))
		a.Tick(4)
	}
	inject(t, a, intentModeSwitch(input.ModeTargetInsert))
	unsettled := func() {
		t.Helper()
		if before := a.pushed(); !a.handleIntent(typed()) || a.pushed() == before {
			t.Fatal("the keystroke pushed nothing")
		}
	}
	type left struct {
		at      event.Stamp
		barrier bool
	}
	var hosted, opened left
	unsettled()
	a.World().RunSafe(func() {
		if err = a.beginHostingLocked("127.0.0.1:0", ""); err == nil {
			hosted = left{a.Position(), a.barrierLocked()}
		}
	})
	if err != nil || !hosted.barrier {
		t.Fatalf("host: %v; left %+v", err, hosted)
	}
	unsettled()
	a.Settle()
	typeOn := func(ticks int) {
		for range ticks {
			inject(t, a, typed())
			a.Tick(1)
		}
		a.World().RunSafe(func() { _ = a.stopJournalLocked(-1) })
		a.serveJournal()
	}
	typeOn(150)

	mid := journal.NewCapture()
	a.cfg.JournalSink = mid
	if _, err := a.openJournal(); err != nil {
		t.Fatalf("open: %v", err)
	}
	a.World().RunSafe(func() { opened = left{a.Position(), a.barrierLocked()} })
	typeOn(45)

	for _, c := range []struct {
		rec     *journal.Capture
		left    left
		digests int
	}{{rec, hosted, 7}, {mid, opened, 2}} {
		cfg, err := ConfigFromAnchor(c.rec.Anchors()[0])
		if err != nil {
			t.Fatal(err)
		}
		rep, err := NewHeadless(cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer rep.Close()
		end := c.rec.End()
		d, err := newReplayDriver(rep, journal.Set{Records: c.rec.Records(), Captures: c.rec.Captures(),
			Digests: c.rec.Digests(), End: &end}.Stream())
		if err != nil {
			t.Fatal(err)
		}
		for d.Stats().Installed == 0 {
			if more, err := d.Step(); err != nil || !more {
				t.Fatalf("replay before the write: %v", err)
			}
		}
		got := left{at: rep.Position()}
		rep.World().RunSafe(func() { got.barrier = rep.barrierLocked() })
		if got != c.left {
			t.Fatalf("the install left the replay at %+v; the run's write left it at %+v", got, c.left)
		}
		if err = d.RunAll(); err != nil {
			t.Fatal(err)
		}
		if st := d.Stats(); st.Diverged != nil || st.Installed != 1 || st.Digests < c.digests {
			t.Fatalf("replay diverged %v; %+v", st.Diverged, st)
		}
	}
}

// TestAReplayShowsNoInputTheRunPausedFor: a command typed or a menu read while the run
// paused took none of its time, so no presented step shows the run paused, on its
// command line or on a menu: not one a host's write landed among, nor the stop asked
// from the menu, which closes the journal once the menu has.
func TestAReplayShowsNoInputTheRunPausedFor(t *testing.T) {
	// Not parallel: hosting binds a real socket.
	rec := journal.NewCapture()
	a, err := NewHeadless(Config{Seed: fixtureSeed, Width: 120, Height: 40,
		Resources: resource.Options{Embedded: true}, Journal: true, JournalSink: rec})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	tickUntilCursor(t, a)
	a.Tick(30)
	injectExCommand(t, a, "bot add")
	a.Tick(60)
	inject(t, a, &input.Intent{Type: input.IntentConfigMenu, Count: 1})
	a.ctx.SetOverlaySelection("diagnostics")
	inject(t, a, &input.Intent{Type: input.IntentOverlayScroll, Motion: input.MotionRight, Count: 1})
	a.ctx.SetOverlaySelection("journal")
	inject(t, a, &input.Intent{Type: input.IntentOverlayScroll, Motion: input.MotionRight, Count: 1})
	a.serveJournal()
	if a.recorder.Load() == nil || a.ctx.SessionCtl.Journaling() {
		t.Fatalf("with the menu up after its stop, journal open %v, reported %v", a.recorder.Load() != nil, a.ctx.SessionCtl.Journaling())
	}
	// Back to the menu's pages, then out of it
	inject(t, a, &input.Intent{Type: input.IntentOverlayClose, Count: 1}, &input.Intent{Type: input.IntentOverlayClose, Count: 1})
	a.serveJournal()
	if a.recorder.Load() != nil || len(rec.Captures()) != 1 {
		t.Fatalf("journal open %v once the menu closed, %d worlds written", a.recorder.Load() != nil, len(rec.Captures()))
	}

	cfg, err := ConfigFromAnchor(rec.Anchors()[0])
	if err != nil {
		t.Fatal(err)
	}
	rep, err := NewHeadless(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rep.Close()
	end := rec.End()
	d, err := newReplayDriver(rep, journal.Set{Records: rec.Records(), Captures: rec.Captures(),
		Digests: rec.Digests(), End: &end}.Stream())
	if err != nil {
		t.Fatal(err)
	}
	for more, src := true, (journalSource{d: d, log: rep.log}); more; {
		if more, err = src.Step(); err != nil {
			t.Fatal(err)
		}
		if m, p := rep.ctx.GetMode(), rep.ctx.TimeCtl.IsPaused(); m == core.ModeCommand || m == core.ModeOverlay || p {
			t.Fatalf("the step presented at %+v shows the run in mode %d, paused %v", rep.Position(), m, p)
		}
	}
}

// TestAnOpeningKeepsTheRunsOwnCursor: a journal opened mid-session leaves each
// participant on its own slot, driving its own cursor with the energy and heat it
// held, and each journal replays on that slot.
func TestAnOpeningKeepsTheRunsOwnCursor(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x3017, 2, [][2]int{{1, 2}})
	own := localCursors(t, apps)
	rng := vmath.NewFastRand(9)
	motions := []input.MotionOp{input.MotionLeft, input.MotionRight, input.MotionUp, input.MotionDown}
	want := map[*App]map[event.Stamp][]string{apps[0]: {}, apps[1]: {}}
	play := func(ticks int, record bool) {
		for range ticks {
			for _, a := range apps {
				if rng.Intn(3) == 0 {
					inject(t, a, intentMotion(motions[rng.Intn(4)], 1+rng.Intn(3)))
				}
			}
			tickAll(apps)
			apps[1].ApplyPendingCorrections()
			if record {
				for _, a := range apps {
					p := a.Position()
					want[a][event.Stamp{Run: p.Run, Tick: p.Tick}] = guestSurface(a)
				}
			}
		}
	}
	type cursor struct {
		slot         uint8
		drives       core.Entity
		energy, heat int64
	}
	read := func(a *App, e core.Entity) (c cursor) {
		a.World().RunSafe(func() {
			w := a.World()
			en, _ := w.Components.Energy.GetComponent(e)
			h, _ := w.Components.Heat.GetComponent(e)
			c = cursor{w.Resources.Player.LocalSlot(), w.Resources.Player.Entity, en.Current, int64(h.Current)}
		})
		return c
	}
	play(200, false)
	for i, a := range apps {
		a.World().RunSafe(func() {
			w := a.World()
			w.Components.Energy.SetComponent(own[i], component.EnergyComponent{Current: 4321 + int64(i)})
			h, _ := w.Components.Heat.GetComponent(own[i])
			h.Current = 37 + i
			w.Components.Heat.SetComponent(own[i], h)
		})
	}
	recs := map[*App]*journal.Capture{}
	for i, a := range apps {
		recs[a] = journal.NewCapture()
		a.cfg.JournalSink = recs[a]
		before := read(a, own[i])
		if _, err := a.openJournal(); err != nil {
			t.Fatalf("participant %d: open: %v", i+1, err)
		}
		if after := read(a, own[i]); after != before || before.drives != own[i] {
			t.Fatalf("participant %d drove %+v before the opening and %+v after it", i+1, before, after)
		}
	}
	play(400, true)
	for _, a := range apps {
		a.World().RunSafe(func() { _ = a.stopJournalLocked(-1) })
		a.serveJournal()
		replayMatches(t, recs[a], want[a], guestSurface)
	}
}

// TestNoJournalOpensWhileANewGameStarts: between a reset's cleanup and the boot spawn
// that brings the roster back, an opening is refused, and once the game has started
// it opens on every cursor. A live run's scheduler starts the game once the lock the
// reset was dispatched under frees, so the reset is dispatched as its loop does.
func TestNoJournalOpensWhileANewGameStarts(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 0x7E5, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	spawnCursor(t, a)
	a.Tick(30)
	a.World().RunSafe(func() {
		a.ctx.PushEventOrigin(event.EventGameResetRequest, &event.GameResetPayload{}, event.OriginDebug)
		a.scheduler.SettleLocked()
	})
	a.cfg.JournalSink = journal.NewCapture()
	if _, err := a.openJournal(); err == nil {
		t.Fatal("a journal opened while the new game was starting")
	}
	a.Tick(1)
	if _, err := a.openJournal(); err != nil {
		t.Fatalf("open after the new game started: %v", err)
	}
	a.Tick(40)
	var n int
	a.World().RunSafe(func() { n = a.World().Resources.Player.Count() })
	if n != 2 {
		t.Fatalf("%d cursors after the new game, want 2", n)
	}
}

// TestAStoppedJournalEndsWhereTheRunStands: :journal stop ends the journal where the
// run stands as its loop serves the stop; the run then records nothing and reports no
// journal.
func TestAStoppedJournalEndsWhereTheRunStands(t *testing.T) {
	t.Parallel()
	rec := journal.NewCapture()
	a, err := NewHeadless(Config{Seed: 7, Width: 80, Height: 24,
		Resources: resource.Options{Embedded: true}, Journal: true, JournalSink: rec})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()
	a.Tick(5)
	inject(t, a, &input.Intent{Type: input.IntentFireMain, Count: 1})

	var stopErr error
	a.World().RunSafe(func() { stopErr = a.stopJournalLocked(-1) })
	a.serveJournal()
	if stopErr != nil {
		t.Fatalf("stop: %v", stopErr)
	}
	if end, at := rec.End(), a.Position(); end != at {
		t.Fatalf("end record at %+v, the run at %+v", end, at)
	}
	before := len(rec.Records())
	inject(t, a, &input.Intent{Type: input.IntentFireMain, Count: 1})
	a.Tick(5)
	if n := len(rec.Records()) - before; n != 0 {
		t.Fatalf("%d records after the stop", n)
	}
	if emitted, _ := a.JournalStats(); emitted != 0 || a.ctx.SessionCtl.Journaling() {
		t.Fatal("a stopped journal is still reported")
	}
	a.World().RunSafe(func() { stopErr = a.stopJournalLocked(-1) })
	if stopErr == nil {
		t.Fatal("a second stop found a journal")
	}
}

// TestJournalAsksApplyInTheOrderAsked: asks the run's loop has not served yet apply
// in order, so a stop cancels a start before it and a start after a stop opens
// another journal once the stop has closed; the status line tells what was served.
func TestJournalAsksApplyInTheOrderAsked(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 7, 80, 24)
	defer a.Close()
	a.cfg.JournalSink = journal.NewCapture()
	a.Tick(5)
	start, stop := a.startJournalLocked, a.stopJournalLocked
	ask := func(asks ...func(int) error) *journal.Recorder {
		t.Helper()
		var err error
		a.World().RunSafe(func() {
			for _, f := range asks {
				err = errors.Join(err, f(-1))
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		a.serveJournal()
		return a.recorder.Load()
	}
	first := ask(start)
	if r := ask(stop, start); r == nil || r == first {
		t.Fatal("a start after a stop opened no other journal")
	}
	if ask(stop, start, stop) != nil || !strings.HasPrefix(a.ctx.GetStatusMessage(), "Journal closed") {
		t.Fatalf("a stop after a start that followed a stop: journal open %v, status %q", a.recorder.Load() != nil, a.ctx.GetStatusMessage())
	}
	if ask(start, stop) != nil {
		t.Fatal("a start the stop overtook opened a journal")
	}
}

// TestAJournalMissingAWrittenWorldCloses: a journal a write could not reach is
// closed and the player told, while a journal opened since that write stays open.
func TestAJournalMissingAWrittenWorldCloses(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 7, 80, 24)
	defer a.Close()
	a.cfg.JournalSink = journal.NewCapture()
	a.Tick(5)
	var err error
	a.World().RunSafe(func() { err = a.startJournalLocked(-1) })
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	a.serveJournal()
	if a.recorder.Load() == nil {
		t.Fatal("no journal opened")
	}
	lost := errors.New("too large")
	a.World().RunSafe(func() { a.journalLostLocked(event.NewJournal(journal.NewCapture()), lost) })
	if a.recorder.Load() == nil {
		t.Fatal("a write missing another journal closed this one")
	}
	a.World().RunSafe(func() { a.journalLostLocked(a.world.Resources.Event.Queue.Journal(), lost) })
	if a.recorder.Load() != nil {
		t.Fatal("the journal stayed open past a world it could not record")
	}
	if msg := a.Context().GetStatusMessage(); !strings.Contains(msg, "could not be recorded (too large)") {
		t.Fatalf("status %q does not say why the journal closed", msg)
	}
}

// guestSurface is the simulation surface less what the recorded run's link did: a
// replay has none, so its traffic and connect events are not the simulation's. The
// session the simulation read, and the pause it decided, are compared in their place.
func guestSurface(a *App) []string {
	var out []string
	for _, l := range a.SnapshotSimulation() {
		if !strings.Contains(l, "|msg=network") && !strings.Contains(l, "|msg=event|") && !strings.Contains(l, "|msg=event.settle|") {
			out = append(out, l)
		}
	}
	s := a.World().Session()
	return append(out, fmt.Sprintf("session=%t/%d/%d/%t paused=%t", s.Live, s.Participant, s.Authority,
		s.SeatsOnly, a.Context().TimeCtl.IsPaused()))
}

// TestModeChangedAppliesWithoutRouter covers the applier directly, so a MetaSystem
// regression fails here rather than as an opaque snapshot diff. Leaving overlay mode
// closes the overlay, or a replayed :help would stay open with no router to close it.
func TestModeChangedAppliesWithoutRouter(t *testing.T) {
	t.Parallel()
	a, err := NewHeadless(scriptConfig(fixtureSeed))
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	a.Tick(1)
	a.Context().PushLocalOrigin(event.EventModeChanged,
		&event.ModeChangedPayload{Mode: core.ModeVisual}, event.OriginDebug)
	a.Settle()

	if got := a.Context().GetMode(); got != core.ModeVisual {
		t.Fatalf("mode %d, want %d", got, core.ModeVisual)
	}
	if s := a.World().Resources.Status.Strings.Get("context.mode").Load(); s != core.ModeNames[core.ModeVisual] {
		t.Fatalf("context.mode %q, want %q", s, core.ModeNames[core.ModeVisual])
	}

	a.Context().PushLocalOrigin(event.EventModeChanged,
		&event.ModeChangedPayload{Mode: core.ModeOverlay}, event.OriginDebug)
	a.Context().PushLocalOrigin(event.EventMetaHelpRequest, nil, event.OriginDebug)
	a.Settle()
	if !a.Context().IsOverlayActive() {
		t.Fatal("a replayed help request opened no overlay")
	}
	a.Context().PushLocalOrigin(event.EventModeChanged,
		&event.ModeChangedPayload{Mode: core.ModeNormal}, event.OriginDebug)
	a.Settle()
	if a.Context().IsOverlayActive() {
		t.Fatal("the overlay outlived overlay mode")
	}
}

// runSplitSettleScript settles several times between two ticks, and does it in the
// shape that actually bites: a producer whose handler queues an event of the same
// type a replayed record carries, so a merged settle applies them in the wrong order.
func runSplitSettleScript(t *testing.T, a *App) int {
	t.Helper()
	r := newScriptRunner(t, a)

	// SetupLevel clamps the cursor and queues CursorMoved; the motion below records
	// an absolute position that a later pass would otherwise overwrite
	a.SetupLevel(60, 30, true, true)
	if !a.Inject(intentMotion(input.MotionRight, 8)) {
		t.Fatal("quit")
	}
	r.step(1)

	// Two settles, one boundary
	if !a.Inject(intentMotion(input.MotionRight, 4)) {
		t.Fatal("quit")
	}
	if !a.Inject(intentSpecial(input.SpecialDeleteChar, 3)) {
		t.Fatal("quit")
	}
	r.step(1)

	// Inject then InputTick, still one boundary
	if !a.Inject(intentMotion(input.MotionDown, 2)) {
		t.Fatal("quit")
	}
	if !a.InputTick() {
		t.Fatal("quit")
	}
	r.step(1)

	r.step(1, intentModeSwitch(input.ModeTargetInsert))
	r.step(1, intentTextChar('x'))
	r.step(1, intentEscape())
	return r.done()
}

// TestDenySimKeysExist keeps the deny list honest: a renamed or dropped metric must
// fail here rather than silently widen the replay assertion surface
func TestDenySimKeysExist(t *testing.T) {
	t.Parallel()
	a, err := NewHeadless(scriptConfig(fixtureSeed))
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	reg := a.World().Resources.Status
	if !reg.Frozen() {
		t.Fatal("registry is not frozen: NewHeadless no longer calls Prepare")
	}
	for _, key := range snapshot.SimDeniedKeys() {
		if reg.Ints.Has(key) || reg.Bools.Has(key) || reg.Floats.Has(key) || reg.Strings.Has(key) {
			continue
		}
		t.Errorf("denied key %q is not registered: rename it in snapshot.deniedSimKey or drop it", key)
	}
}

// TestSnapshotSimulationExcludesSession asserts the split is real in both
// directions: operator state moves the full snapshot and not the simulation one
func TestSnapshotSimulationExcludesSession(t *testing.T) {
	t.Parallel()
	a, err := NewHeadless(scriptConfig(fixtureSeed))
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	a.Tick(2)
	wantSim, wantFull := a.SnapshotSimulation(), a.Snapshot()
	if len(wantSim) >= len(wantFull) {
		t.Fatalf("simulation view kept %d of %d lines: nothing was excluded", len(wantSim), len(wantFull))
	}

	ctx := a.Context()
	ctx.AutoFire.Store(engine.AutoFireOff)
	ctx.MouseDisabled.Store(!ctx.MouseDisabled.Load())
	ctx.TimeCtl.SetPaused(true)

	if i, x, y, ok := snapshot.FirstDiff(wantSim, a.SnapshotSimulation()); ok {
		t.Fatalf("session state reached the simulation view at line %d:\n  before %s\n  after  %s", i, x, y)
	}
	if _, _, _, ok := snapshot.FirstDiff(wantFull, a.Snapshot()); !ok {
		t.Fatal("full snapshot ignored a session change: :t save no longer reports operator state")
	}
}

// TestVerifyAnchorRejectsMismatch asserts a corpus or geometry mismatch is a
// startup error, not a snapshot diff a hundred ticks later
func TestVerifyAnchorRejectsMismatch(t *testing.T) {
	t.Parallel()
	a, err := NewHeadless(scriptConfig(fixtureSeed))
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	good := event.JournalAnchor{
		Schema:         event.JournalSchema,
		Seed:           fixtureSeed,
		Session:        a.World().Resources.Rand.Session(),
		ScenarioID:     a.scenario.Name,
		ScenarioDigest: a.scenario.Digest(),
		ContentID:      a.World().Resources.Status.Strings.Get("content.source").Load(),
		ContentFiles:   uint64(a.World().Resources.Status.Ints.Get("content.files").Load()),
		ContentBlocks:  uint64(a.World().Resources.Status.Ints.Get("content.blocks").Load()),
		ContentLines:   uint64(a.World().Resources.Status.Ints.Get("content.lines").Load()),
		TickInterval:   int64(parameter.GameUpdateInterval),
		Width:          a.Context().Width,
		Height:         a.Context().Height,
	}
	if err := a.VerifyAnchor(good); err != nil {
		t.Fatalf("verify own anchor: %v", err)
	}

	for name, mutate := range map[string]func(*event.JournalAnchor){
		"seed":           func(x *event.JournalAnchor) { x.Seed++ },
		"content_id":     func(x *event.JournalAnchor) { x.ContentID = "elsewhere" },
		"content_pin":    func(x *event.JournalAnchor) { x.ContentPin = "one.go" },
		"content_blocks": func(x *event.JournalAnchor) { x.ContentBlocks++ },
		"width":          func(x *event.JournalAnchor) { x.Width++ },
	} {
		bad := good
		mutate(&bad)
		if err := a.VerifyAnchor(bad); err == nil {
			t.Errorf("%s mismatch accepted", name)
		}
	}
}

// TestJournalStampRebasesOnReset pins the invariant the replay driver depends on:
// the run advances exactly when the tick counter is re-based, and its first tick
// is 1 whether :n or the scenario asked for it, so the driver counts ticks
func TestJournalStampRebasesOnReset(t *testing.T) {
	t.Parallel()
	a, err := NewHeadless(scriptConfig(fixtureSeed))
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	q := a.World().Resources.Event.Queue
	a.Tick(3)
	if s := q.Stamp(); s.Run != 0 || s.Tick != 3 {
		t.Fatalf("stamp %+v before reset, want run 0 tick 3", s)
	}

	a.Reset(false)
	if s := q.Stamp(); s.Run != 1 || s.Tick != 0 {
		t.Fatalf("stamp %+v after reset settle, want run 1 tick 0", s)
	}
	if n := a.World().Resources.Game.State.GetGameTicks(); n != 0 {
		t.Fatalf("game ticks %d after reset, want 0", n)
	}
	// Service the scheduler-side reset without advancing to tick one. Its FSM
	// entry actions run at tick zero and therefore need the rebased clock already.
	a.Tick(0)
	if got := a.World().Resources.Time.GameTime; !got.Equal(engine.SimEpoch) {
		t.Fatalf("game time after reset = %v, want simulation epoch %v", got, engine.SimEpoch)
	}

	a.Tick(2)
	if s := q.Stamp(); s.Run != 1 || s.Tick != 2 {
		t.Fatalf("stamp %+v after two ticks in run 1", s)
	}

	// A reset the scenario requests mid-tick ends that tick, so its run opens alike
	cursor := localCursorEntity(a)
	a.Context().PushEventOrigin(event.EventEnergySetRequest,
		&event.EnergySetPayload{Entity: cursor, Value: 0}, event.OriginDebug)
	a.Context().PushEventOrigin(event.EventHeatSetRequest,
		&event.HeatSetRequestPayload{Entity: cursor, Value: 0}, event.OriginDebug)
	for range 20 {
		if q.Stamp().Run == 2 {
			break
		}
		a.Tick(1)
	}
	if s, n := q.Stamp(), a.World().Resources.Game.State.GetGameTicks(); s.Run != 2 || s.Tick != 0 || n != 0 {
		t.Fatalf("stamp %+v with %d game ticks after a defeat, want run 2 at tick 0", s, n)
	}
	a.Tick(1)
	if s := q.Stamp(); s.Run != 2 || s.Tick != 1 {
		t.Fatalf("stamp %+v after one tick in run 2", s)
	}
}

// runLongScript crosses an APM fold, which intent-side accounting could not replay.
// One second of game time is 20 ticks; the fold lands inside the run.
func runLongScript(t *testing.T, a *App) int {
	t.Helper()
	r := newScriptRunner(t, a)

	for i := range 6 {
		r.step(2, intentMotion(input.MotionRight, 1+i%3))
		r.step(2, intentMotion(input.MotionDown, 1))
	}
	r.step(1, intentModeSwitch(input.ModeTargetInsert))
	r.step(1, intentTextChar('x'))
	r.step(2, intentEscape())
	return r.done()
}

// TestReplayAcrossAPMFold asserts APM is produced and reproduced
func TestReplayAcrossAPMFold(t *testing.T) {
	t.Parallel()
	cap, want, _, end := journalRun(t, runLongScript)
	if end.Tick <= uint64(time.Second/parameter.GameUpdateInterval) {
		t.Fatalf("script ran %d ticks, too short to cross an APM fold", end.Tick)
	}

	folded := false
	for _, line := range want {
		if strings.HasPrefix(line, "reg|stat|") &&
			strings.Contains(line, "|apm=") && !strings.Contains(line, "|apm=0|") {
			folded = true
		}
	}
	if !folded {
		t.Fatal("script folded no APM: admission is not seeing input-origin events")
	}
	if err := replayInto("", cap.Anchors(), cap.Records(), want, end); err != nil {
		t.Fatal(err)
	}
}

// TestAudioMuteStaysOutOfTheJournal is the rule that the speakers a run played on
// are no part of it: whoever replays the run holds their own mute, so a recorded
// toggle would only flip theirs from wherever it stands.
func TestAudioMuteStaysOutOfTheJournal(t *testing.T) {
	t.Parallel()
	cap, _, _, _ := journalRun(t, func(t *testing.T, a *App) int {
		r := newScriptRunner(t, a)
		r.step(1, &input.Intent{Type: input.IntentToggleAudioCycle, Count: 1})
		r.step(1, intentMotion(input.MotionRight, 1))
		return r.done()
	})
	for _, rec := range cap.Records() {
		if rec.Type == event.EventSoundMuteToggle {
			t.Fatalf("jseq %d journals the audio mute, origin %s", rec.JSeq, rec.Origin)
		}
	}
}

// TestResizeReflowsAndRejects covers the whole resize boundary: ScreenSize stays an
// exact inverse of the forward derivation, a degenerate report is dropped rather than
// clamped, and a replayed mid-run change reproduces in both crop modes — crop destroys
// out-of-bounds entities and resets the camera, no-crop preserves the map and clamps
// it, so the two exercise different halves of HandleResizeLocked.
func TestResizeReflowsAndRejects(t *testing.T) {
	t.Parallel()
	a, err := NewHeadless(Config{Resources: resource.Options{Embedded: true}, Seed: fixtureSeed, Width: 100, Height: 40})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	a.Tick(1)
	for _, d := range [][2]int{{100, 40}, {73, 39}, {50, 22}, {120, 48}} {
		a.Resize(d[0], d[1])
		a.Tick(1)
		if w, h := engine.ScreenSize(a.World().Resources.Config); w != d[0] || h != d[1] {
			t.Fatalf("ScreenSize = %dx%d after resize to %dx%d", w, h, d[0], d[1])
		}
		if a.Context().Width != d[0] || a.Context().Height != d[1] {
			t.Fatalf("context %dx%d after resize to %dx%d",
				a.Context().Width, a.Context().Height, d[0], d[1])
		}
	}

	w, h := a.Context().Width, a.Context().Height
	for _, d := range [][2]int{{0, 0}, {-4, 10},
		{parameter.LeftMargin, 40},
		{80, parameter.TopMargin + parameter.BottomMargin}} {
		a.Resize(d[0], d[1])
		if a.Context().Width != w || a.Context().Height != h {
			t.Fatalf("degenerate resize %dx%d applied: now %dx%d",
				d[0], d[1], a.Context().Width, a.Context().Height)
		}
	}

	for name, crop := range map[string]bool{"crop": true, "nocrop": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			replayAndCompare(t, func(t *testing.T, a *App) int {
				r := newScriptRunner(t, a)
				a.SetupLevel(60, 30, true, crop)
				r.step(2, intentMotion(input.MotionRight, 8))
				r.step(2, intentMotion(input.MotionDown, 6))

				a.Resize(50, 22) // shrink
				r.step(2)
				if a.Context().Width != 50 || a.Context().Height != 22 {
					t.Fatalf("resize not applied: %dx%d",
						a.Context().Width, a.Context().Height)
				}

				a.Resize(90, 40) // grow
				r.step(2, intentMotion(input.MotionRight, 4))
				return r.done()
			})
		})
	}
}

// runResetScript crosses a game reset, which restarts the tick counter: the run
// marker is the only thing that keeps the record stream ordered across it.
func runResetScript(t *testing.T, a *App) int {
	t.Helper()
	r := newScriptRunner(t, a)

	r.step(1, intentMotion(input.MotionRight, 6))
	r.step(1, intentModeSwitch(input.ModeTargetInsert))
	r.step(1, intentTextChar('a'))
	r.step(1, intentEscape())

	// ':new' is OriginCommand, so the reset request is itself journaled
	r.step(1, intentModeSwitch(input.ModeTargetCommand))
	r.step(2, intentCommandBody("new")...)
	if got := a.World().Resources.Event.Queue.Stamp().Run; got != 1 {
		t.Fatalf("run %d after :new, want 1", got)
	}

	r.step(2, intentMotion(input.MotionDown, 2))
	r.step(1, intentMotion(input.MotionRight, 3))

	// A second reset, from the debug path, so the script covers two generations
	a.Reset(false)
	r.step(2, intentMotion(input.MotionRight, 4))
	if got := a.World().Resources.Event.Queue.Stamp().Run; got != 2 {
		t.Fatalf("run %d after Reset, want 2", got)
	}
	return r.done()
}

// runDefeatScript ends a run the way the scenario does: the monitor answers a
// defeat with a new game, and no record carries that reset.
func runDefeatScript(t *testing.T, a *App) int {
	t.Helper()
	r := newScriptRunner(t, a)
	r.step(1, intentMotion(input.MotionRight, 6))
	cursor := localCursorEntity(a)
	a.Context().PushEventOrigin(event.EventEnergySetRequest,
		&event.EnergySetPayload{Entity: cursor, Value: 0}, event.OriginDebug)
	a.Context().PushEventOrigin(event.EventHeatSetRequest,
		&event.HeatSetRequestPayload{Entity: cursor, Value: 0}, event.OriginDebug)
	r.step(4)
	if got := a.World().Resources.Event.Queue.Stamp().Run; got != 1 {
		t.Fatalf("run %d after a defeat, want 1", got)
	}
	r.step(2, intentMotion(input.MotionDown, 2))
	return r.done()
}

// runHoldScript holds the run between two motions: the run takes no tick while it
// stands, and its replay, which a hold stops nowhere, has to reach the same world.
func runHoldScript(t *testing.T, a *App) int {
	t.Helper()
	r := newScriptRunner(t, a)
	r.step(1, intentMotion(input.MotionRight, 4))
	r.step(1, intentModeSwitch(input.ModeTargetCommand))
	r.step(3, intentCommandBody("pause")...)
	if !a.Context().TimeCtl.Held() {
		t.Fatal(":pause did not hold: the script no longer covers a held run's replay")
	}
	r.step(1, intentModeSwitch(input.ModeTargetCommand))
	r.step(1, intentCommandBody("pause")...)
	r.step(2, intentMotion(input.MotionDown, 2))
	return r.done()
}

// TestCursorMoveRequestAppliesWithoutRouter covers the cursor-owned placement path.
func TestCursorMoveRequestAppliesWithoutRouter(t *testing.T) {
	t.Parallel()
	cap := journal.NewCapture()
	cfg := scriptConfig(fixtureSeed)
	cfg.Journal = true
	cfg.JournalSink = cap
	a, err := NewHeadless(cfg)
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	a.Tick(1)
	player := a.World().Resources.Player.Entity
	if player != 1 || a.World().Resources.Player.Slot(0) != player {
		t.Fatalf("initial cursor = entity %d slot-zero %d, want entity 1 in slot zero",
			player, a.World().Resources.Player.Slot(0))
	}
	from, ok := a.World().Positions.GetPosition(player)
	if !ok {
		t.Fatal("no cursor position")
	}
	wantX, wantY := from.X+3, from.Y+2

	a.Context().PushEventOrigin(event.EventCursorMoveRequest,
		&event.CursorMoveRequestPayload{Entity: player, X: wantX, Y: wantY}, event.OriginDebug)
	a.Settle()

	got, _ := a.World().Positions.GetPosition(player)
	if got.X != wantX || got.Y != wantY {
		t.Fatalf("cursor at %d,%d after EventCursorMoveRequest(%d,%d)",
			got.X, got.Y, wantX, wantY)
	}

	records := cap.Records()
	if len(records) == 0 {
		t.Fatal("cursor move command was not journaled")
	}
	last := records[len(records)-1]
	if last.Type != event.EventCursorMoveRequest || !strings.Contains(last.Payload, "entity = 1") {
		t.Fatalf("last journal record = %s %q, want addressed cursor move",
			event.GetEventName(last.Type), last.Payload)
	}
}

// TestInputTickSerializesCursorLifecycle covers the live-loop race boundary:
// timer-driven auto-fire reads the local cursor while CursorSystem may unbind it
// during reset. The direct roster cycle keeps the test focused on that access;
// -race verifies the read and writes share World.updateMutex.
func TestInputTickSerializesCursorLifecycle(t *testing.T) {
	t.Parallel()
	a, err := NewHeadless(scriptConfig(fixtureSeed))
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	a.Tick(1)
	roster := a.World().Resources.Player
	player := roster.Slot(0)
	if player == 0 {
		t.Fatal("no local cursor")
	}

	const rounds = 256
	start := make(chan struct{})
	done := make(chan struct{})
	go func() {
		for range start {
			a.InputTick()
			done <- struct{}{}
		}
	}()

	for range rounds {
		start <- struct{}{}
		a.world.RunSafe(func() {
			roster.Unbind(0)
			roster.Bind(0, player)
		})
		<-done
	}
	close(start)

	if got := roster.Slot(0); got != player || roster.Entity != player {
		t.Fatalf("restored roster = slot %d local %d, want %d", got, roster.Entity, player)
	}
}

// TestReplayResetThenCursor pins the window the soak diverges in: a level whose map
// differs from the viewport, a reset, then cursor and mode intents applied before any
// tick of the new run, while the FSM reset is still pending
func TestReplayResetThenCursor(t *testing.T) {
	t.Parallel()
	replayAndCompare(t, func(t *testing.T, a *App) int {
		r := newScriptRunner(t, a)
		a.SetupLevel(40, 16, true, false) // map decoupled from the viewport
		r.step(2, intentMotion(input.MotionRight, 6))

		a.Reset(false)
		// No tick between the reset and these three, matching the soak's stamp
		if !a.Inject(intentMotion(input.MotionRight, 9)) {
			t.Fatal("quit")
		}
		if !a.Inject(intentModeSwitch(input.ModeTargetInsert)) {
			t.Fatal("quit")
		}
		if !a.Inject(intentModeSwitch(input.ModeTargetInsert)) {
			t.Fatal("quit")
		}
		r.step(2)
		return r.done()
	})
}

var bisectSeed = flag.Uint64("soak.seed", 0, "seed to bisect in TestReplayBisect")

// bisectOnce journals the first steps script actions and replays them
func bisectOnce(t *testing.T, seed uint64, steps int) error {
	t.Helper()
	cap := journal.NewCapture()
	cfg := scriptConfig(seed)
	cfg.Journal, cfg.JournalSink = true, cap

	a, err := NewHeadless(cfg)
	if err != nil {
		t.Fatalf("source run: %v", err)
	}
	if _, err := journal.RunFuzz(a, journal.DefaultFuzz(seed, steps)); err != nil {
		a.Close()
		t.Fatalf("script: %v", err)
	}
	want, end := a.SnapshotSimulation(), a.Position()
	a.Close()
	if len(cap.Records()) == 0 {
		return nil // nothing recorded yet, nothing to reproduce
	}
	return replayInto("", cap.Anchors(), cap.Records(), want, end)
}

// TestReplayBisect reports the shortest script prefix whose replay diverges, then
// dumps the records of the last two ticks of that prefix
func TestReplayBisect(t *testing.T) {
	t.Parallel()
	if *bisectSeed == 0 {
		t.Skip("set -soak.seed=0x... to bisect one seed")
	}
	seed := *bisectSeed
	if err := bisectOnce(t, seed, soakSteps); err == nil {
		t.Fatalf("seed %#x reproduces at %d steps", seed, soakSteps)
	}

	lo, hi := 0, soakSteps // lo reproduces, hi diverges
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if bisectOnce(t, seed, mid) == nil {
			lo = mid
		} else {
			hi = mid
		}
	}

	cap := journal.NewCapture()
	cfg := scriptConfig(seed)
	cfg.Journal, cfg.JournalSink = true, cap
	a, _ := NewHeadless(cfg)
	_, _ = journal.RunFuzz(a, journal.DefaultFuzz(seed, hi))
	end := a.Position()
	a.Close()

	t.Logf("seed %#x first diverges at step %d, ending at %+v", seed, hi, end)
	for _, r := range cap.Records() {
		if r.Run == end.Run && r.Tick+2 >= end.Tick {
			t.Logf("jseq %d run %d tick %d boundary %d seq %d %s %s payload=%q",
				r.JSeq, r.Run, r.Tick, r.Boundary, r.Seq,
				r.Origin, event.GetEventName(r.Type), r.Payload)
		}
	}
	t.Fatalf("bisected to step %d", hi)
}

// TestReplayLockstep replays against the source's own per-tick snapshots, so a
// divergence names the tick it started on rather than the run that ended wrong
func TestReplayLockstep(t *testing.T) {
	t.Parallel()
	if *bisectSeed == 0 {
		t.Skip("set -soak.seed=0x... to lockstep one seed")
	}
	seed := *bisectSeed

	cap := journal.NewCapture()
	cfg := scriptConfig(seed)
	cfg.Journal, cfg.JournalSink = true, cap

	src, err := NewHeadless(cfg)
	if err != nil {
		t.Fatalf("source run: %v", err)
	}
	// Perturb runs after every action and draws from no stream, so snapshotting
	// here records the source's state at the last action of each tick
	want := make(map[event.Stamp][]string, soakSteps)
	opt := journal.DefaultFuzz(seed, soakSteps)
	opt.Perturb = func() {
		p := src.Position()
		want[event.Stamp{Run: p.Run, Tick: p.Tick}] = src.SnapshotSimulation()
	}
	if _, err := journal.RunFuzz(src, opt); err != nil {
		src.Close()
		t.Fatalf("script: %v", err)
	}
	src.Close()

	anchors := cap.Anchors()
	rcfg, err := ConfigFromAnchor(anchors[0])
	if err != nil {
		t.Fatalf("config from anchor: %v", err)
	}
	rep, err := NewHeadless(rcfg)
	if err != nil {
		t.Fatalf("replay run: %v", err)
	}
	defer rep.Close()
	if err := rep.VerifyAnchor(anchors[0]); err != nil {
		t.Fatalf("verify anchor: %v", err)
	}
	d, err := newReplayDriver(rep, journal.Set{Records: cap.Records(), Captures: cap.Captures()}.Stream())
	if err != nil {
		t.Fatalf("driver: %v", err)
	}

	for {
		more, err := d.Step()
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if !more {
			return
		}
		p := rep.Position()
		w, ok := want[event.Stamp{Run: p.Run, Tick: p.Tick}]
		if !ok {
			continue // the source took no snapshot on this tick
		}
		got := rep.SnapshotSimulation()
		if i, _, _, ok := snapshot.FirstDiff(w, got); ok {
			t.Fatalf("diverged in run %d tick %d at line %d:\n%s\nrecords on this tick:\n%s",
				p.Run, p.Tick, i, strings.Join(snapshot.Diff(w, got, 12), "\n"),
				strings.Join(recordsAt(cap.Records(), p.Run, p.Tick), "\n"))
		}
	}
}

// recordsAt renders every record stamped on one tick
func recordsAt(recs []event.JournalRecord, run, tick uint64) []string {
	var out []string
	for _, r := range recs {
		if r.Run == run && r.Tick == tick {
			out = append(out, fmt.Sprintf("  jseq %d boundary %d seq %d %s %s payload=%q",
				r.JSeq, r.Boundary, r.Seq, r.Origin, event.GetEventName(r.Type), r.Payload))
		}
	}
	return out
}
