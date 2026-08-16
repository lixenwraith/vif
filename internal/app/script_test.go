package app

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/input"
)

// Script cadence, in game ticks
const (
	// scriptInputEvery drives ProcessInputTick; auto-fire gates on
	// AutoFireInterval in game time, so a coarser cadence only fires less
	scriptInputEvery = 1

	// scriptHeatEvery restores heat drained by ember decay. Drain spawn count
	// correlates with heat, and heat gates region progression.
	scriptHeatEvery = 200
)

// script drives one App through a fixed gameplay sequence: god mode with
// auto-fire, cursor left at its spawn centre. Deterministic by construction —
// every action is keyed to a tick index, never to elapsed wall time.
type script struct {
	app     *App
	started bool
}

func newScript(a *App) *script { return &script{app: a} }

// runScript drives n ticks of the script and fails on an early quit
func runScript(t *testing.T, a *App, n int) {
	t.Helper()
	s := newScript(a)
	for i := 1; i <= n; i++ {
		s.step(t, i)
	}
}

// step advances one tick, applying whatever the script owes at this index
func (s *script) step(t *testing.T, tick int) {
	t.Helper()

	if !s.started {
		s.begin(t)
		s.started = true
	} else if tick%scriptHeatEvery == 0 {
		s.command(t, "heat 100")
	}

	if tick%scriptInputEvery == 0 {
		if !s.app.InputTick() {
			t.Fatalf("script quit at tick %d", tick)
		}
	}
	s.app.Tick(1)
}

// begin enables auto-fire and grants god mode; the cursor spawns centred
func (s *script) begin(t *testing.T) {
	t.Helper()

	s.app.ctx.AutoFire.Store(true)
	s.app.ctx.MouseDisabled.Store(true) // no pointer source headless
	s.command(t, "god")
}

// command types a command-mode line and confirms it
func (s *script) command(t *testing.T, cmd string) {
	t.Helper()

	intents := make([]*input.Intent, 0, len(cmd)+2)
	intents = append(intents, &input.Intent{
		Type:       input.IntentModeSwitch,
		ModeTarget: input.ModeTargetCommand,
	})
	for _, r := range cmd {
		intents = append(intents, &input.Intent{Type: input.IntentTextChar, Char: r})
	}
	intents = append(intents, &input.Intent{Type: input.IntentTextConfirm})

	if !s.app.Inject(intents...) {
		t.Fatalf("command %q quit the game", cmd)
	}
	if mode := s.app.ctx.GetMode(); mode != core.ModeNormal {
		t.Fatalf("command %q left mode %d, want normal", cmd, mode)
	}
}

// TestScriptReachesCombat asserts the script exercises the systems the
// determinism arms are meant to cover. A green determinism test over an inert
// world proves nothing.
func TestScriptReachesCombat(t *testing.T) {
	const ticks = 600

	a := newTestApp(t, testConfig(0xC0FFEE))
	runScript(t, a, ticks)

	if got := statInt(t, a, "engine.ticks"); got != ticks {
		t.Errorf("engine.ticks = %d, want %d", got, ticks)
	}
	if got := statInt(t, a, "weapon.main_fired"); got == 0 {
		t.Error("weapon.main_fired = 0: auto-fire is not reaching the tick path")
	}
	if got := statInt(t, a, "weapon.rod_fired"); got == 0 {
		t.Error("weapon.rod_fired = 0: rod acquired no targets")
	}
	if got := statInt(t, a, "missile.spawned"); got == 0 {
		t.Error("missile.spawned = 0: launcher acquired no targets")
	}
	if got := statInt(t, a, "event.dropped"); got != 0 {
		t.Errorf("event.dropped = %d: queue overflow invalidates the run", got)
	}

	// Reported, not asserted: tuning signal for the heat/drain cadence
	t.Logf("kills drain=%d swarm=%d quasar=%d storm=%d",
		statInt(t, a, "kills.drain"), statInt(t, a, "kills.swarm"),
		statInt(t, a, "kills.quasar"), statInt(t, a, "kills.storm"))
	t.Logf("entities created=%d destroyed=%d | heat=%d | fsm=%s",
		statInt(t, a, "entity.created_total"), statInt(t, a, "entity.destroyed_total"),
		statInt(t, a, "heat.current"), a.world.Resources.Status.Strings.Get("fsm.state").Load())
}
