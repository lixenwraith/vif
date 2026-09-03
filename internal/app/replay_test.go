package app

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/journal"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/resource"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
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
	// Two ticks: EnergySystem.Update pushes EventShieldActivate on the first,
	// ShieldSystem applies it on the second
	r.step(2, intentCommandBody("energy 500")...)

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

// runOverlayScript drives the debug overlay through command mode and closes it.
// Every mode change and pause change on this path is an event; the overlay's own
// state (content, scroll, selection, pins) is view state and appears in no snapshot.
func runOverlayScript(t *testing.T, a *App) int {
	t.Helper()
	r := newScriptRunner(t, a)

	r.step(1, intentModeSwitch(input.ModeTargetCommand))
	r.step(1, intentCommandBody("d")...) // EventMetaDebugRequest, RequestMode(Overlay), pause
	if a.Context().GetMode() != core.ModeOverlay {
		t.Fatalf("mode %d after :d, want overlay", a.Context().GetMode())
	}
	if !a.Context().IsOverlayActive() {
		t.Fatal(":d built no overlay content")
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
func replayInto(anchors []event.JournalAnchor, recs []event.JournalRecord,
	want []string, end event.Stamp) error {

	if len(anchors) == 0 {
		return errors.New("no anchor captured")
	}
	rcfg, err := ConfigFromAnchor(anchors[0])
	if err != nil {
		return fmt.Errorf("config from anchor: %w", err)
	}
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
	if err := replayInto(cap.Anchors(), cap.Records(), want, end); err != nil {
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

// TestReplayReproducesRecordedRuns drives one authored script per record-stream
// shape and requires each to reproduce bit-identically:
//
//   - gameplay covers mode, pause, command origin and ping bounds;
//   - overlay settles whether debug-overlay mode belongs in the stream — everything
//     the snapshot observes on that path is event-driven, so it reproduces;
//   - split-settle reproduces the multi-settle tick boundaries App.Loop produces
//     for every live input event;
//   - reset spans game resets, where the tick counter restarts and a run-blind
//     driver would reject the stream outright.
func TestReplayReproducesRecordedRuns(t *testing.T) {
	t.Parallel()
	for name, script := range map[string]func(*testing.T, *App) int{
		"gameplay":     runScript,
		"overlay":      runOverlayScript,
		"split-settle": runSplitSettleScript,
		"reset":        runResetScript,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			replayAndCompare(t, script)
		})
	}
}

// TestModeChangedAppliesWithoutRouter covers the applier directly, so a MetaSystem
// regression fails here rather than as an opaque snapshot diff
func TestModeChangedAppliesWithoutRouter(t *testing.T) {
	t.Parallel()
	a, err := NewHeadless(scriptConfig(fixtureSeed))
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	a.Tick(1)
	a.Context().PushEventOrigin(event.EventModeChanged,
		&event.ModeChangedPayload{Mode: core.ModeVisual}, event.OriginDebug)
	a.Settle()

	if got := a.Context().GetMode(); got != core.ModeVisual {
		t.Fatalf("mode %d, want %d", got, core.ModeVisual)
	}
	if s := a.World().Resources.Status.Strings.Get("context.mode").Load(); s != core.ModeNames[core.ModeVisual] {
		t.Fatalf("context.mode %q, want %q", s, core.ModeNames[core.ModeVisual])
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
	ctx.AutoFire.Store(!ctx.AutoFire.Load())
	ctx.MouseDisabled.Store(!ctx.MouseDisabled.Load())
	ctx.TimeCtl.SetPaused(true)

	if i, x, y, ok := snapshot.FirstDiff(wantSim, a.SnapshotSimulation()); ok {
		t.Fatalf("session state reached the simulation view at line %d:\n  before %s\n  after  %s", i, x, y)
	}
	if _, _, _, ok := snapshot.FirstDiff(wantFull, a.Snapshot()); !ok {
		t.Fatal("full snapshot ignored a session change: :d save no longer reports operator state")
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
		Schema:        event.JournalSchema,
		Seed:          fixtureSeed,
		Session:       a.World().Resources.Rand.Session(),
		ConfigID:      resolveConfigID(a.cfg),
		ContentID:     a.World().Resources.Status.Strings.Get("content.source").Load(),
		ContentFiles:  uint64(a.World().Resources.Status.Ints.Get("content.files").Load()),
		ContentBlocks: uint64(a.World().Resources.Status.Ints.Get("content.blocks").Load()),
		ContentLines:  uint64(a.World().Resources.Status.Ints.Get("content.lines").Load()),
		TickInterval:  int64(parameter.GameUpdateInterval),
		Width:         a.Context().Width,
		Height:        a.Context().Height,
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
// the run advances exactly when the tick counter is re-based
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

	a.Tick(2)
	if s := q.Stamp(); s.Run != 1 || s.Tick != 2 {
		t.Fatalf("stamp %+v after two ticks in run 1", s)
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
	if err := replayInto(cap.Anchors(), cap.Records(), want, end); err != nil {
		t.Fatal(err)
	}
}

// TestResizeReflowsAndRejects covers the whole resize boundary: ScreenSize stays
// an exact inverse of the forward derivation so a margin change cannot desync the
// anchor from the geometry it describes, a degenerate report is dropped rather
// than clamped, and a replayed mid-run change reproduces in both crop modes. Crop
// destroys out-of-bounds entities and resets the camera; no-crop preserves the map
// and clamps the camera, so the two exercise different halves of
// HandleResizeLocked.
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
	return replayInto(cap.Anchors(), cap.Records(), want, end)
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
	d, err := newReplayDriver(rep, cap.Records())
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
