package app

import (
	"strconv"
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/manifest"
)

// testConfig is the fixture every determinism test builds from: embedded FSM
// and corpus, fixed seed, fixed dimensions, no recorder, no snapshot period.
// Every input is declared here, so a divergence cannot come from the config.
func testConfig(seed uint64) Config {
	return Config{
		ForceDefault: true, // embedded FSM and corpus; no filesystem discovery
		Seed:         seed,
		Width:        HeadlessDefaultWidth,
		Height:       HeadlessDefaultHeight,
		StatTicks:    -1, // disabled: the harness snapshots explicitly
		RecTicks:     -1, // disabled: no flight recorder, no process-global sink
	}
}

// newTestApp builds a headless runtime and registers its teardown
func newTestApp(t *testing.T, cfg Config) *App {
	t.Helper()
	a, err := NewHeadless(cfg)
	if err != nil {
		t.Fatalf("NewHeadless: %v", err)
	}
	t.Cleanup(a.Close)
	return a
}

// TestHeadlessConstruction asserts the headless branch omits presentation and
// leaves the runtime usable
func TestHeadlessConstruction(t *testing.T) {
	a := newTestApp(t, testConfig(1))

	if a.term != nil || a.termSvc != nil {
		t.Error("headless app holds a terminal")
	}
	if a.orchestrator != nil {
		t.Error("headless app holds a render orchestrator")
	}
	if a.router == nil || a.inputMachine == nil {
		t.Fatal("headless app lacks the intent pipeline")
	}

	// MetaSystem joins outside the manifest, so the set is one larger
	if got, want := len(a.world.Systems()), len(manifest.ActiveSystems())+1; got != want {
		t.Errorf("systems = %d, want %d", got, want)
	}
	if !a.world.Resources.Status.Frozen() {
		t.Error("metric set not frozen by NewHeadless")
	}

	lines := a.Snapshot()
	if len(lines) == 0 {
		t.Fatal("empty snapshot")
	}
	for _, l := range lines {
		if !strings.HasPrefix(l, "ctx|") && !strings.HasPrefix(l, "reg|") {
			t.Fatalf("malformed snapshot line: %q", l)
		}
	}
}

// TestHeadlessRejectsInvalidConfig covers the settings a headless run cannot
// honour and the dimension floor
func TestHeadlessRejectsInvalidConfig(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"color mode", func(c *Config) { c.ColorModeSet = true }},
		{"audio backend", func(c *Config) { c.AudioBackend = "pulse" }},
		{"time scale", func(c *Config) { c.TimeScaleSpec = "2" }},
		{"narrow", func(c *Config) { c.Width = 4 }},
		{"short", func(c *Config) { c.Height = 2 }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig(1)
			tc.mutate(&cfg)
			a, err := NewHeadless(cfg)
			if err == nil {
				a.Close()
				t.Fatal("expected rejection")
			}
		})
	}
}

// TestHeadlessDefaultsDimensions asserts Normalize fills unset dimensions
func TestHeadlessDefaultsDimensions(t *testing.T) {
	cfg := testConfig(1)
	cfg.Width, cfg.Height = 0, 0
	a := newTestApp(t, cfg)

	if a.ctx.Width != HeadlessDefaultWidth || a.ctx.Height != HeadlessDefaultHeight {
		t.Errorf("dimensions = %dx%d, want %dx%d",
			a.ctx.Width, a.ctx.Height, HeadlessDefaultWidth, HeadlessDefaultHeight)
	}
}

// TestHeadlessTicks asserts a tick budget is consumed exactly
func TestHeadlessTicks(t *testing.T) {
	const ticks = 50 // arbitrary: this asserts the counter, not gameplay

	a := newTestApp(t, testConfig(1))
	a.Tick(ticks)

	if got := a.ctx.State.GetGameTicks(); got != ticks {
		t.Errorf("game ticks = %d, want %d", got, ticks)
	}
}

// TestHeadlessTickComposition asserts Tick(a) then Tick(b) equals Tick(a+b).
// RunTicks drains the reset channel at both ends, so the split must not
// introduce or lose a reset.
func TestHeadlessTickComposition(t *testing.T) {
	const total = 120 // spans several MainSpawnGold retries at a 50ms tick

	whole := newTestApp(t, testConfig(7))
	whole.Tick(total)

	split := newTestApp(t, testConfig(7))
	split.Tick(total / 3)
	split.Tick(total - total/3)

	assertSnapshotsEqual(t, whole.Snapshot(), split.Snapshot())
}

// TestHeadlessResetServiced asserts a reset requested headless is executed
// rather than dropped: only schedulerLoop receives resetChan interactively.
func TestHeadlessResetServiced(t *testing.T) {
	const preTicks = 40

	a := newTestApp(t, testConfig(3))
	a.Tick(preTicks)

	before := a.world.Resources.Rand.Session()
	a.Reset(false)
	a.Tick(1) // drainReset executes the FSM reset

	if after := a.world.Resources.Rand.Session(); after <= before {
		t.Errorf("RNG session = %d, want > %d: reset was not serviced", after, before)
	}

	// A second reset proves the one-slot channel did not latch
	mid := a.world.Resources.Rand.Session()
	a.Reset(false)
	a.Tick(1)
	if after := a.world.Resources.Rand.Session(); after <= mid {
		t.Errorf("second reset dropped: session %d, want > %d", after, mid)
	}
}

// TestHeadlessMapViewportDecoupled asserts SetupLevel emulates both resize
// modes, so a headless run is not pinned to the viewport
func TestHeadlessMapViewportDecoupled(t *testing.T) {
	a := newTestApp(t, testConfig(1))
	a.Tick(1)

	cfg := a.world.Resources.Config
	vw, vh := cfg.ViewportWidth, cfg.ViewportHeight

	a.SetupLevel(vw*2, vh*2, true, false)
	a.Tick(1)

	if cfg.MapWidth != vw*2 || cfg.MapHeight != vh*2 {
		t.Fatalf("map = %dx%d, want %dx%d", cfg.MapWidth, cfg.MapHeight, vw*2, vh*2)
	}
	if cfg.ViewportWidth != vw || cfg.ViewportHeight != vh {
		t.Errorf("viewport moved with the map: %dx%d", cfg.ViewportWidth, cfg.ViewportHeight)
	}

	// Zero dimensions restore map = viewport with crop enabled
	a.SetupLevel(0, 0, true, true)
	a.Tick(1)
	if cfg.MapWidth != vw || cfg.MapHeight != vh {
		t.Errorf("map = %dx%d after restore, want %dx%d", cfg.MapWidth, cfg.MapHeight, vw, vh)
	}
}

// === Determinism ===

// TestDeterminismSequential is the payoff test: two runs of one seed, no input,
// diffed on the status snapshot.
func TestDeterminismSequential(t *testing.T) {
	const ticks = 600 // 30s of game time: past MainDecayWait/MainDecayWave

	first := newTestApp(t, testConfig(0x5EED))
	first.Tick(ticks)

	second := newTestApp(t, testConfig(0x5EED))
	second.Tick(ticks)

	assertSnapshotsEqual(t, first.Snapshot(), second.Snapshot())
}

// TestDeterminismSequentialScripted repeats the payoff test with the god-mode
// script, so combat, death, genetics and adaptation are on the tick path.
func TestDeterminismSequentialScripted(t *testing.T) {
	const ticks = 600

	first := newTestApp(t, testConfig(0x5EED))
	runScript(t, first, ticks)

	second := newTestApp(t, testConfig(0x5EED))
	runScript(t, second, ticks)

	assertSnapshotsEqual(t, first.Snapshot(), second.Snapshot())
}

// TestDeterminismInterleaved runs two instances in lockstep and reports the
// first divergent tick directly, rather than bisecting after the fact.
// Also the standing check that two live worlds do not share mutable state.
func TestDeterminismInterleaved(t *testing.T) {
	const ticks = 400

	a := newTestApp(t, testConfig(0xA11CE))
	b := newTestApp(t, testConfig(0xA11CE))

	for n := 1; n <= ticks; n++ {
		a.Tick(1)
		b.Tick(1)

		sa, sb := a.Snapshot(), b.Snapshot()
		if idx, la, lb, ok := FirstDiff(sa, sb); ok {
			_, _, _ = idx, la, lb
			t.Fatalf("divergence at tick %d:\n%s", n,
				strings.Join(diffSnapshots(a.Snapshot(), b.Snapshot()), "\n"))
		}
	}
}

// TestDeterminismInterleavedScripted drives both instances with identical
// scripted input, which is where map-iteration order in the fitness paths
// is expected to surface.
func TestDeterminismInterleavedScripted(t *testing.T) {
	const ticks = 400

	a := newTestApp(t, testConfig(0xA11CE))
	b := newTestApp(t, testConfig(0xA11CE))

	sa, sb := newScript(a), newScript(b)
	for n := 1; n <= ticks; n++ {
		sa.step(t, n)
		sb.step(t, n)

		if idx, la, lb, ok := FirstDiff(a.Snapshot(), b.Snapshot()); ok {
			t.Fatalf("divergence at tick %d, snapshot line %d:\n  A: %s\n  B: %s",
				n, idx, la, lb)
		}
	}
}

// TestDeterminismDistinctSeeds guards the negative case: identical snapshots
// across seeds would mean the comparison is blind to simulation state.
func TestDeterminismDistinctSeeds(t *testing.T) {
	const ticks = 300

	a := newTestApp(t, testConfig(1))
	runScript(t, a, ticks)

	b := newTestApp(t, testConfig(2))
	runScript(t, b, ticks)

	if _, _, _, ok := FirstDiff(a.Snapshot(), b.Snapshot()); !ok {
		t.Fatal("distinct seeds produced identical snapshots: the snapshot is not observing simulation state")
	}
}

// TestSnapshotNoLateMetrics asserts nothing registers after Freeze; a late
// metric is a detached cell, absent from every snapshot, and invalidates
// every comparison above.
func TestSnapshotNoLateMetrics(t *testing.T) {
	const ticks = 300

	a := newTestApp(t, testConfig(1))
	runScript(t, a, ticks)

	if late := a.world.Resources.Status.Ints.Get("stat.late").Load(); late != 0 {
		t.Errorf("stat.late = %d, want 0", late)
	}
}

// assertSnapshotsEqual reports the first difference with surrounding context
func assertSnapshotsEqual(t *testing.T, want, got []string) {
	t.Helper()

	idx, lw, lg, ok := FirstDiff(want, got)
	if !ok {
		return
	}

	var b strings.Builder
	b.WriteString("snapshots differ at line ")
	b.WriteString(itoa(idx))
	b.WriteString("\n  A: ")
	b.WriteString(lw)
	b.WriteString("\n  B: ")
	b.WriteString(lg)
	b.WriteString("\ncontext:\n")
	for i := max(0, idx-3); i < min(len(want), idx+4); i++ {
		b.WriteString("  ")
		b.WriteString(want[i])
		b.WriteByte('\n')
	}
	t.Fatal(b.String())
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// statInt reads a registered int metric. MetricMap.Get on a frozen registry
// returns a detached cell and counts a late registration, so an unknown key
// must fail the test rather than silently read zero.
func statInt(t *testing.T, a *App, key string) int64 {
	t.Helper()
	reg := a.world.Resources.Status
	if !reg.Ints.Has(key) {
		t.Fatalf("status int %q is not registered", key)
	}
	return reg.Ints.Get(key).Load()
}

// diffSnapshots returns every differing line pair, capped for readability.
// The full set names which subsystems diverged; FirstDiff alone is dominated
// by whichever group sorts first.
func diffSnapshots(x, y []string) []string {
	const cap = 12

	var out []string
	n := min(len(x), len(y))
	for i := range n {
		if x[i] != y[i] {
			out = append(out, "  A: "+x[i], "  B: "+y[i])
			if len(out) >= cap*2 {
				return append(out, "  ... truncated")
			}
		}
	}
	if len(x) != len(y) {
		out = append(out, "  line count differs: "+itoa(len(x))+" vs "+itoa(len(y)))
	}
	return out
}

// setSystemEnabled toggles a system by name and settles the request
func setSystemEnabled(a *App, name string, enabled bool) {
	a.ctx.PushEvent(event.EventMetaSystemCommandRequest, &event.MetaSystemCommandPayload{
		SystemName: name,
		Enabled:    enabled,
	})
	a.Settle()
}

// entityKind names the stores an entity belongs to, so a diverging entity
// identifies its owning system
func entityKind(a *App, e core.Entity) string {
	c := &a.world.Components
	var kinds []string
	for _, probe := range []struct {
		name string
		has  func(core.Entity) bool
	}{
		{"glyph", c.Glyph.HasEntity}, {"drain", c.Drain.HasEntity},
		{"missile", c.Missile.HasEntity}, {"cleaner", c.Cleaner.HasEntity},
		{"dust", c.Dust.HasEntity}, {"loot", c.Loot.HasEntity},
		{"orb", c.Orb.HasEntity}, {"bullet", c.Bullet.HasEntity},
		{"blossom", c.Blossom.HasEntity}, {"decay", c.Decay.HasEntity},
		{"flash", c.Flash.HasEntity}, {"fadeout", c.Fadeout.HasEntity},
		{"materialize", c.Materialize.HasEntity}, {"nugget", c.Nugget.HasEntity},
		{"swarm", c.Swarm.HasEntity}, {"quasar", c.Quasar.HasEntity},
		{"eye", c.Eye.HasEntity}, {"wall", c.Wall.HasEntity},
		{"kinetic", c.Kinetic.HasEntity}, {"combat", c.Combat.HasEntity},
	} {
		if probe.has(e) {
			kinds = append(kinds, probe.name)
		}
	}
	if len(kinds) == 0 {
		return "unknown"
	}
	return strings.Join(kinds, "+")
}

// dumpDivergence reports the first differing entities between two runs
func dumpDivergence(t *testing.T, a, b *App, limit int) {
	t.Helper()

	var out []string
	a.world.RunSafe(func() {
		b.world.RunSafe(func() {
			ea, eb := a.world.Positions.Entities(), b.world.Positions.Entities()
			if len(ea) != len(eb) {
				out = append(out, "position store size "+itoa(len(ea))+" vs "+itoa(len(eb)))
			}
			n := min(len(ea), len(eb))
			for i := range n {
				if len(out) >= limit {
					break
				}
				if ea[i] != eb[i] {
					out = append(out, "slot "+itoa(i)+": entity "+itoa(int(ea[i]))+
						" ("+entityKind(a, ea[i])+") vs "+itoa(int(eb[i]))+
						" ("+entityKind(b, eb[i])+")")
					continue
				}
				e := ea[i]
				pa, _ := a.world.Positions.GetPosition(e)
				pb, _ := b.world.Positions.GetPosition(e)
				if pa != pb {
					out = append(out, "entity "+itoa(int(e))+" ("+entityKind(a, e)+") pos "+
						itoa(pa.X)+","+itoa(pa.Y)+" vs "+itoa(pb.X)+","+itoa(pb.Y))
					continue
				}
				ka, oka := a.world.Components.Kinetic.GetComponent(e)
				kb, okb := b.world.Components.Kinetic.GetComponent(e)
				if oka && okb && (ka.PreciseX != kb.PreciseX || ka.PreciseY != kb.PreciseY ||
					ka.VelX != kb.VelX || ka.VelY != kb.VelY) {
					out = append(out, "entity "+itoa(int(e))+" ("+entityKind(a, e)+") kinetic "+
						fmtKin(ka.PreciseX, ka.PreciseY, ka.VelX, ka.VelY)+" vs "+
						fmtKin(kb.PreciseX, kb.PreciseY, kb.VelX, kb.VelY))
				}
			}
		})
	})

	if len(out) == 0 {
		t.Log("no per-entity difference: divergence is in counters or non-hashed state")
		return
	}
	t.Log("first entity differences:\n  " + strings.Join(out, "\n  "))
}

func fmtKin(px, py, vx, vy float64) string {
	f := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
	return "p(" + f(px) + "," + f(py) + ") v(" + f(vx) + "," + f(vy) + ")"
}
