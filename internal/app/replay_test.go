package app

import (
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// fixtureSeed pins the perturbation test so CI is reproducible
const fixtureSeed = 0x5eed1e55

// scriptConfig builds a hermetic headless config: ForceDefault pins the embedded
// FSM and corpus, so a run does not depend on cwd or $XDG_CONFIG_HOME
func scriptConfig(seed uint64) Config {
	return Config{Headless: true, ForceDefault: true, Seed: seed}
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
	if _, tick, _ := vlog.Stamp(); tick == 0 {
		t.Skip("build carries no vlog correlation stamp; records cannot be tick-aligned")
	}
	return r
}

func (r *scriptRunner) step(ticks int, intents ...*input.Intent) {
	r.t.Helper()
	if len(intents) > 0 && !r.a.Inject(intents...) {
		r.t.Fatal("script intent quit the game")
	}
	r.a.Tick(ticks)
	r.ticks += ticks
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

	r.step(1, intentMotion(input.MotionRight, 5))       // EventCursorMoved
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
	if b := a.World().Resources.Player.GetBounds(); !b.Active {
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
// simulation snapshot, the drawn seed and the tick count
func journalRun(t *testing.T, script func(*testing.T, *App) int) (*Capture, []string, uint64, int) {
	t.Helper()

	cap := NewCapture()
	cfg := scriptConfig(0) // drawn seed: the anchor is its only channel to the replay
	cfg.Journal, cfg.JournalSink = true, cap

	a, err := NewHeadless(cfg)
	if err != nil {
		t.Fatalf("source run: %v", err)
	}
	ticks := script(t, a)
	snap, seed := a.SnapshotSimulation(), a.Seed()
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
	return cap, snap, seed, ticks
}

// replayCapture rebuilds from the anchor, verifies it, replays and compares
func replayCapture(t *testing.T, cap *Capture, want []string, ticks int) {
	t.Helper()

	anchors := cap.Anchors()
	if len(anchors) == 0 {
		t.Fatal("no anchor captured")
	}
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

	st, err := rep.Replay(cap.Records())
	if err != nil {
		t.Fatalf("replay: %v (%+v)", err, st)
	}
	switch rest := ticks - int(st.Ticks); {
	case rest > 0:
		rep.Tick(rest)
	case rest < 0:
		t.Fatalf("replay ran %d ticks, source ran %d", st.Ticks, ticks)
	}

	if i, x, y, ok := FirstDiff(want, rep.SnapshotSimulation()); ok {
		t.Fatalf("replay diverged at line %d (%+v):\n  source %s\n  replay %s", i, st, x, y)
	}
}

func replayAndCompare(t *testing.T, script func(*testing.T, *App) int) {
	t.Helper()
	cap, want, seed, ticks := journalRun(t, script)
	if a := cap.Anchors(); len(a) > 0 && a[0].Seed != seed {
		t.Fatalf("anchor seed %d, run seed %d", a[0].Seed, seed)
	}
	replayCapture(t, cap, want, ticks)
}

// TestJournalDoesNotPerturb drives one fixed-seed sequence twice, journaled and
// not, and asserts the snapshots match and the capture is dense
func TestJournalDoesNotPerturb(t *testing.T) {
	plain, err := NewHeadless(scriptConfig(fixtureSeed))
	if err != nil {
		t.Fatalf("plain run: %v", err)
	}
	runScript(t, plain)
	want := plain.Snapshot()
	plain.Close()

	cap := NewCapture()
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

	if i, x, y, ok := FirstDiff(want, got); ok {
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

// TestReplayReproducesRun replays the gameplay script, covering mode, pause,
// command origin and ping bounds
func TestReplayReproducesRun(t *testing.T) { replayAndCompare(t, runScript) }

// TestReplayOverlayRoundTrip replays a debug-overlay open and close. It exists to
// settle whether overlay mode belongs in the record stream: everything the snapshot
// observes on this path is event-driven, so it reproduces.
func TestReplayOverlayRoundTrip(t *testing.T) { replayAndCompare(t, runOverlayScript) }

// TestModeChangedAppliesWithoutRouter covers the applier directly, so a MetaSystem
// regression fails here rather than as an opaque snapshot diff
func TestModeChangedAppliesWithoutRouter(t *testing.T) {
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

// TestReplaySplitSettle asserts the recorded settle boundary reproduces multi-settle
// tick boundaries, which App.Loop produces for every live input event
func TestReplaySplitSettle(t *testing.T) { replayAndCompare(t, runSplitSettleScript) }

// TestDenySimKeysExist keeps the deny list honest: a renamed or dropped metric must
// fail here rather than silently widen the replay assertion surface
func TestDenySimKeysExist(t *testing.T) {
	a, err := NewHeadless(scriptConfig(fixtureSeed))
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	reg := a.World().Resources.Status
	if !reg.Frozen() {
		t.Fatal("registry is not frozen: NewHeadless no longer calls Prepare")
	}
	for key := range denySim {
		if reg.Ints.Has(key) || reg.Bools.Has(key) || reg.Floats.Has(key) || reg.Strings.Has(key) {
			continue
		}
		t.Errorf("denied key %q is not registered: rename it in denySim or drop it", key)
	}
}

// TestSnapshotSimulationExcludesSession asserts the split is real in both
// directions: operator state moves the full snapshot and not the simulation one
func TestSnapshotSimulationExcludesSession(t *testing.T) {
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

	if i, x, y, ok := FirstDiff(wantSim, a.SnapshotSimulation()); ok {
		t.Fatalf("session state reached the simulation view at line %d:\n  before %s\n  after  %s", i, x, y)
	}
	if _, _, _, ok := FirstDiff(wantFull, a.Snapshot()); !ok {
		t.Fatal("full snapshot ignored a session change: :d save no longer reports operator state")
	}
}

// TestVerifyAnchorRejectsMismatch asserts a corpus or geometry mismatch is a
// startup error, not a snapshot diff a hundred ticks later
func TestVerifyAnchorRejectsMismatch(t *testing.T) {
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
	cap, want, _, ticks := journalRun(t, runLongScript)
	if ticks <= int(time.Second/parameter.GameUpdateInterval) {
		t.Fatalf("script ran %d ticks, too short to cross an APM fold", ticks)
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
	replayCapture(t, cap, want, ticks)
}

// TestScreenSizeInvertsViewport pins the inverse against the forward derivation, so
// a margin change cannot desync the anchor from the geometry it describes
func TestScreenSizeInvertsViewport(t *testing.T) {
	a, err := NewHeadless(Config{Headless: true, ForceDefault: true,
		Seed: fixtureSeed, Width: 100, Height: 40})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

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
}

// TestReplayResize replays a mid-run screen change in both crop modes. Crop
// destroys out-of-bounds entities and resets the camera; no-crop preserves the map
// and clamps the camera, so the two exercise different halves of HandleResizeLocked.
func TestReplayResize(t *testing.T) {
	for name, crop := range map[string]bool{"crop": true, "nocrop": false} {
		t.Run(name, func(t *testing.T) {
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

// TestResizeRejectsDegenerate asserts a collapsed report is dropped rather than
// clamped, which is what keeps ScreenSize an exact inverse
func TestResizeRejectsDegenerate(t *testing.T) {
	a, err := NewHeadless(scriptConfig(fixtureSeed))
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	a.Tick(1)
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
}
