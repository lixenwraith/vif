package app

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// fixtureSeed pins the perturbation test so CI is reproducible
const fixtureSeed = 0x5eed1e55

// scriptConfig builds a headless config; zero dimensions take the defaults,
// which validateHeadless accepts by construction
func scriptConfig(seed uint64) Config {
	return Config{Headless: true, Seed: seed}
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

// runScript drives a fixed sequence and returns the ticks it executed.
// Two constraints, both replay properties rather than test style:
// injection happens at most once per tick boundary, because the driver settles
// once per boundary; and the run stays inside one APM window (1s of game time =
// 20 ticks), because the router admits APM from intents that a replay never sees.
func runScript(t *testing.T, a *App) int {
	t.Helper()

	total := 0
	step := func(ticks int, intents ...*input.Intent) {
		if len(intents) > 0 && !a.Inject(intents...) {
			t.Fatal("script intent quit the game")
		}
		a.Tick(ticks)
		total += ticks
	}

	step(2)
	if _, tick, _ := vlog.Stamp(); tick == 0 {
		t.Skip("build carries no vlog correlation stamp; records cannot be tick-aligned")
	}

	step(2, intentMotion(input.MotionRight, 5))
	step(2, intentModeSwitch(input.ModeTargetInsert)) // EventModeChanged
	step(2, intentTextChar('i'), intentTextChar('f')) // pooled CharacterTypedPayload
	step(2, intentEscape())                           // EventModeChanged, back to normal
	step(2, intentSpecial(input.SpecialDeleteChar, 2))

	// Auto-fire and macro playback, on their own boundary: InputTick settles what
	// it emits, so sharing a boundary with Inject would split the settle
	if !a.InputTick() {
		t.Fatal("script macro intent quit the game")
	}
	step(2)
	step(2, intentMotion(input.MotionDown, 3))
	return total
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
	recs := cap.Records()
	if len(recs) == 0 {
		t.Fatal("no records captured: the script produced no journaled events")
	}
	if uint64(len(recs)) != emitted {
		t.Fatalf("sink saw %d records, journal emitted %d", len(recs), emitted)
	}
}

// TestReplayReproducesRun journals a headless run, rebuilds its config from the
// anchor, and replays the records into a fresh App. The seed is drawn, never
// fixed: the anchor is the only channel it may travel through.
func TestReplayReproducesRun(t *testing.T) {
	cap := NewCapture()
	cfg := scriptConfig(0)
	cfg.Journal, cfg.JournalSink = true, cap

	src, err := NewHeadless(cfg)
	if err != nil {
		t.Fatalf("source run: %v", err)
	}
	ticks := runScript(t, src)
	want := src.Snapshot()
	seed := src.Seed()

	// APM is admitted from intents, not from the event stream: a fold inside the
	// script diverges on replay for a reason unrelated to the simulation
	if apm := src.World().Resources.Status.Ints.Get("engine.apm").Load(); apm != 0 {
		t.Fatalf("script outran the APM window (engine.apm=%d): shorten it or tag the intents MacroPlayback", apm)
	}
	src.Close()

	if seed == 0 {
		t.Fatal("run drew no seed")
	}
	anchors := cap.Anchors()
	if len(anchors) == 0 {
		t.Fatal("no anchor captured")
	}
	if anchors[0].Seed != seed {
		t.Fatalf("anchor seed %d, run seed %d", anchors[0].Seed, seed)
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

	st, err := rep.Replay(cap.Records())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	switch rest := ticks - int(st.Ticks); {
	case rest > 0:
		rep.Tick(rest) // the last record does not reach the end of the run
	case rest < 0:
		t.Fatalf("replay ran %d ticks, source ran %d", st.Ticks, ticks)
	}

	if i, x, y, ok := FirstDiff(want, rep.Snapshot()); ok {
		t.Fatalf("replay diverged at line %d (%d records, %d injected, %d filtered):\n  source %s\n  replay %s",
			i, st.Records, st.Injected, st.Filtered, x, y)
	}
}

// TestModeChangedAppliesWithoutRouter covers the phase-1 applier directly, so a
// ModeSystem regression fails here rather than as an opaque snapshot diff
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
