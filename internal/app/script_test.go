package app

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
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

	// scriptTowerTick is when the tower arm enters the region: late enough that
	// main has spawned gold and drains, early enough for a full pylon chain.
	scriptTowerTick = 100
)

// script drives one App through a fixed gameplay sequence: god mode with
// auto-fire, cursor left at its spawn centre. Deterministic by construction —
// every action is keyed to a tick index, never to elapsed wall time.
type script struct {
	app     *App
	tower   bool
	started bool
}

func newScript(a *App) *script      { return &script{app: a} }
func newTowerScript(a *App) *script { return &script{app: a, tower: true} }

// runScript drives n ticks of the combat arm and fails on an early quit
func runScript(t *testing.T, a *App, n int) { runArm(t, newScript(a), n) }

// runTowerScript drives n ticks of the tower arm and fails on an early quit
func runTowerScript(t *testing.T, a *App, n int) { runArm(t, newTowerScript(a), n) }

// runArm advances a script to completion
func runArm(t *testing.T, s *script, n int) {
	t.Helper()
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

	if s.tower {
		switch tick {
		case scriptTowerTick:
			s.app.Region(event.RegionPause, "main", "")
			s.app.Region(event.RegionSpawn, "tower", "TowerSetup")
		case scriptTowerTick + 1:
			// publishRegionStats ran during the previous tick
			if got := regionState(t, s.app, "tower"); got == "-" {
				t.Fatalf("tower region did not spawn")
			}
		}
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

// TestScriptReachesTower asserts the tower arm puts eyes, gateways, route
// graphs and the GA on the tick path
func TestScriptReachesTower(t *testing.T) {
	const ticks = 600

	a := newTestApp(t, testConfig(0xC0FFEE))
	s := newTowerScript(a)

	var maxEyes, maxGraphs, maxRecomputes int64
	for i := 1; i <= ticks; i++ {
		s.step(t, i)
		maxEyes = max(maxEyes, statInt(t, a, "eye.count"))
		maxGraphs = max(maxGraphs, statInt(t, a, "adapt.graphs"))
		maxRecomputes = max(maxRecomputes, statInt(t, a, "nav.recomputes"))
	}

	if maxEyes == 0 {
		t.Error("eye.count never rose: gateways spawned nothing")
	}
	if maxGraphs == 0 {
		t.Error("adapt.graphs never rose: no route graph reached the bandit")
	}
	if maxRecomputes == 0 {
		t.Error("nav.recomputes never rose: composite flow fields are idle")
	}
	if got := statInt(t, a, "eye.ga.outcomes"); got == 0 {
		t.Error("eye.ga.outcomes = 0: no fitness was reported")
	}
	if got := statInt(t, a, "event.dropped"); got != 0 {
		t.Errorf("event.dropped = %d: queue overflow invalidates the run", got)
	}
}
