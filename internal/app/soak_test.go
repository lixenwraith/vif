package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// Soak profile. Each iteration is reproducible from its seed: rerun one with
// -run 'TestReplaySoak/<seed>'.
const (
	soakSeedBase = 0x50a4_0000
	soakSteps    = 200
)

// soakScale picks one of three effort profiles for a repetition or step count. The
// default is what a change is validated against and what CI runs; full is the wide
// seed sweep, run by hand with VIF_SOAK=full rather than paid for on every edit.
//
//	go test ./...  |  go test -short ./...  |  VIF_SOAK=full go test ./...
func soakScale(short, normal, full int) int {
	if testing.Short() {
		return short
	}
	if os.Getenv("VIF_SOAK") == "full" {
		return full
	}
	return normal
}

// soakRun is one journalled source run and everything a replay needs to reproduce it
type soakRun struct {
	cap  *journal.Capture
	want []string
	end  event.Stamp
	seed uint64
	root string // the config root the replay resolves the anchor's scenario under
}

// The tower soak drives the canonical campaign and its gateway routes.
const soakRoot = "../../wad"
const soakScenarioDir = "../asset/scenario"
const soakContentDir = soakRoot + "/content"

// The fuzz driver can enter these encounters without waiting for campaign escalation.
var towerRegions = []journal.FuzzRegion{
	{Name: "main", State: "MainSpawnGold"},
	{Name: "quasar", State: "QuasarFuse"},
	{Name: "storm", State: "StormSetup"},
	{Name: "kraken", State: "KrakenSetup"},
	{Name: "monitor", State: "MonitorActive"},
	{Name: "tower", State: "TowerSetup"},
}

// towerScenario pins the external scenario and a viewport the tower layout fits in
func towerScenario(t *testing.T, seed uint64) Config {
	t.Helper()
	if _, err := os.Stat(filepath.Join(soakScenarioDir, paths.ScenarioFile)); err != nil {
		t.Skipf("external scenario %s not present", soakScenarioDir)
	}
	cfg := Config{Mode: ModeHeadless, Seed: seed, Width: 160, Height: 50,
		Resources: resource.Options{Dir: soakRoot, Scenario: paths.MainScenarioName}}
	if _, err := os.Stat(soakContentDir); err == nil {
		cfg.Resources.Content = soakContentDir
	}
	return cfg
}

func TestMainProgressesFromThreeStormKillsThroughTwoKrakensToTower(t *testing.T) {
	a, err := NewHeadless(towerScenario(t, fixtureSeed))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Tick(1)
	w := a.World()
	cursor := w.Resources.Player.Slot(0)
	a.Context().PushEventOrigin(event.EventCursorMoveRequest, &event.CursorMoveRequestPayload{Entity: cursor, X: 2, Y: 2}, event.OriginDebug)
	a.Settle()
	mazeCount, mazeWalls, krakenSpawns := 0, 0, 0
	nuggetEnabled := false
	a.SetDispatchTap(func(ev event.GameEvent) {
		switch p := ev.Payload.(type) {
		case *event.MazeSpawnRequestPayload:
			mazeCount++
			if mazeCount == 1 && (p.RoomCount != 0 || len(p.Rooms) != 0) {
				t.Error("Kraken maze contains authored rooms")
			}
		case *event.WallSpawnedPayload:
			if mazeCount == 1 && krakenSpawns == 0 {
				mazeWalls = w.Components.Wall.CountEntities()
			}
		case *event.MetaSystemCommandPayload:
			if p.SystemName == "nugget" {
				nuggetEnabled = p.Enabled
			}
		case *event.SpeciesCreatedPayload:
			if p.Species == component.SpeciesKraken {
				krakenSpawns++
				if mazeWalls == 0 || p.X != w.Resources.Config.MapWidth/2 || p.Y != w.Resources.Config.MapHeight/2 {
					t.Error("Kraken did not spawn at the center after maze construction")
				}
			}
		}
	})
	for round := 1; round <= 2; round++ {
		for killed := 1; killed <= 3; killed++ {
			a.Region(event.RegionPause, "main", "")
			a.Region(event.RegionSpawn, "storm", "StormSetup")
			if w.Components.Storm.CountEntities() != 1 {
				t.Fatal("Storm did not spawn for progression check")
			}
			w.Components.Combat.Each(func(_ core.Entity, c *component.CombatComponent) bool {
				if c.CombatEntityType == component.CombatEntityStorm {
					c.HitPoints, c.LastDamagedBy = 0, cursor
				}
				return true
			})
			a.Tick(3)
			if killed < 3 && (w.Components.Kraken.CountEntities() != 0 || w.Components.Tower.CountEntities() != 0) {
				t.Fatal("Storm escalated before the third kill")
			}
		}
		if krakenSpawns != round || mazeCount != round || (round == 1 && w.Components.Wall.CountEntities() >= mazeWalls) {
			t.Fatal("third Storm kill did not spawn Kraken into a destructible maze")
		}
		cfg := w.Resources.Config
		if cfg.MapWidth != cfg.ViewportWidth || cfg.MapHeight != cfg.ViewportHeight || cfg.CropOnResize {
			t.Fatal("Kraken maze is not a fixed viewport-sized map")
		}
		if !statBoolOf(a, "glyph.enabled") || !nuggetEnabled {
			t.Fatal("Kraken region did not enable glyphs and nuggets")
		}
		if w.Components.Kraken.CountEntities() != 1 {
			t.Fatal("Kraken was not available for this cycle")
		}
		e := w.Components.Kraken.Entities()[0]
		hp, _ := w.Components.Combat.GetPtr(e)
		hp.HitPoints, hp.LastDamagedBy = 0, cursor
		a.Tick(2)
		if round == 1 {
			a.Tick(60)
			if w.Components.Tower.CountEntities() != 0 || w.Components.Kraken.CountEntities() != 0 ||
				w.Resources.Status.Bools.Get("fsm.main.paused").Load() {
				t.Fatal("first Kraken kill did not return to main between storm cycles")
			}
		}
	}
	if krakenSpawns != 2 || w.Components.Tower.CountEntities() != 1 || w.Components.Kraken.CountEntities() != 0 {
		t.Fatal("second Kraken kill did not hand off to Tower")
	}
	a.Region(event.RegionTerminate, "tower", "")
	a.Region(event.RegionSpawn, "kraken", "KrakenSetup")
	a.Tick(1)
	a.Context().PushEventOrigin(event.EventCursorDefeatState, &event.CursorDefeatStatePayload{Entity: cursor, Defeated: true, ProducedTick: a.World().Resources.Game.State.GetGameTicks()}, event.OriginDebug)
	a.Tick(3)
	if w.Components.Kraken.CountEntities() != 0 || w.Resources.Status.Strings.Get("fsm.kraken.state").Load() != "-" {
		t.Fatal("global defeat left the Kraken region or its entity alive")
	}
}

// A gold sequence running when main hands over to quasar, or quasar back to main, is
// adopted by the region taking over rather than cancelled, and still pays once.
func TestGoldOutlivesTheHandoverBetweenMainAndQuasar(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, fixtureSeed, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	w := a.World()
	cursor := w.Resources.Player.Slot(0)
	state := func(region string) string { return w.Resources.Status.Strings.Get("fsm." + region + ".state").Load() }
	tickUntil := func(region, want string) {
		t.Helper()
		for range 100 {
			if state(region) == want {
				return
			}
			a.Tick(1)
		}
		t.Fatalf("%s did not reach %s, at %s", region, want, state(region))
	}
	goldHeader := func() (header core.Entity) {
		w.Components.Header.Each(func(e core.Entity, h *component.HeaderComponent) bool {
			if h.Behavior == component.BehaviorGold {
				header = e
			}
			return header == 0
		})
		return header
	}

	tickUntil("main", "MainGoldActive")
	gold := goldHeader()
	w.Resources.Status.Ints.Get("kills.drain").Store(9)
	a.Context().PushCrossing(event.EventDrainDefeated, &event.DrainDefeatedPayload{Entity: cursor})
	a.Settle()
	tickUntil("quasar", "QuasarGoldActive")
	if goldHeader() != gold {
		t.Fatal("the quasar encounter replaced main's running gold")
	}
	zeroHitPoints(a, w.Components.Quasar.Entities()[0], cursor)
	tickUntil("main", "MainGoldActive")
	if goldHeader() != gold {
		t.Fatal("main replaced the quasar encounter's running gold")
	}

	energy := func() int64 { e, _ := w.Components.Energy.GetComponent(cursor); return e.Current }
	before := energy()
	header, _ := w.Components.Header.GetComponent(gold)
	for i, m := range header.MemberEntries {
		a.Context().PushCrossing(event.EventCompositeMemberDestroyed, &event.CompositeMemberDestroyedPayload{
			HeaderEntity: gold, MemberEntity: m.Entity, Entity: cursor,
			RemainingCount: len(header.MemberEntries) - 1 - i,
		})
	}
	a.Settle()
	if got := energy() - before; got != 1000 {
		t.Fatalf("completing the carried gold paid %d energy, want 1000 once", got)
	}
}

// Main's gold broken by anything but typing dusts every glyph, as quasar's does, so
// it is worth something to a player who never types it.
func TestMainGoldDestroyedDustsEveryGlyph(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, fixtureSeed, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	w := a.World()
	for range 100 {
		if w.Resources.Status.Strings.Get("fsm.main.state").Load() == "MainGoldActive" {
			break
		}
		a.Tick(1)
	}
	var member core.Entity
	w.Components.Header.Each(func(_ core.Entity, h *component.HeaderComponent) bool {
		if h.Behavior == component.BehaviorGold {
			member = h.MemberEntries[0].Entity
		}
		return member == 0
	})
	if member == 0 {
		t.Fatal("main spawned no gold")
	}
	dusted := false
	a.SetDispatchTap(func(ev event.GameEvent) { dusted = dusted || ev.Type == event.EventDustAllRequest })
	w.RunSafe(func() { w.DestroyEntity(member) })
	a.Tick(2)
	if !dusted || w.Resources.Status.Strings.Get("fsm.main.state").Load() != "MainDecayWait" {
		t.Fatal("destroyed main gold did not dust every glyph before the decay wait")
	}
}

// Clearing the tower is rewarded by an event drop, not a species roll: one gun for
// the cursor, which collecting it turns into a second lane.
func TestTowerVictoryDropsAGunThatAddsALane(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, fixtureSeed, 160, 50)
	defer a.Close()
	tickUntilCursor(t, a)
	w := a.World()
	cursor := w.Resources.Player.Slot(0)
	a.Region(event.RegionPause, "main", "")
	a.Region(event.RegionSpawn, "tower", "TowerSetup")
	a.Tick(12)
	if w.Components.Pylon.CountEntities() == 0 {
		t.Fatal("the tower encounter spawned no pylons")
	}
	for _, e := range w.Components.Pylon.Entities() {
		zeroHitPoints(a, e, cursor)
		header, _ := w.Components.Header.GetComponent(e)
		for _, m := range header.MemberEntries {
			zeroHitPoints(a, m.Entity, cursor)
		}
	}
	a.Tick(3)
	if w.Resources.Status.Strings.Get("fsm.tower.state").Load() != "-" {
		t.Fatal("pylon defeats did not clear the tower")
	}
	guns := 0
	w.Components.Loot.Each(func(_ core.Entity, l *component.LootComponent) bool {
		if l.Type == component.LootGun && l.Owner == cursor {
			guns++
		}
		return true
	})
	if guns != 1 {
		t.Fatalf("tower victory dropped %d guns, want 1", guns)
	}
	lanes := func() int { wc, _ := w.Components.Weapon.GetComponent(cursor); return wc.Charges[component.WeaponGun] }
	for range 400 {
		if lanes() == 2 {
			return
		}
		a.Tick(1)
	}
	t.Fatalf("the gun drop was not collected into a lane: %d", lanes())
}

// Each shipped scenario must pass -check, boot a player and end the run on one
// participant defeat.
func TestEveryShippedScenarioResetsOnAnyCursorDefeat(t *testing.T) {
	t.Parallel()
	event.EnsureRegistry()
	for _, name := range []string{"main", "blank", "td", "simon", "warden", "vanguard", ""} {
		label := name
		if label == "" {
			label = resource.EmbeddedLabel
		}
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			opts := resource.Options{Embedded: name == ""}
			if name != "" {
				opts = resource.Options{Dir: soakRoot, Scenario: name}
				if _, err := resource.ScenarioPath(opts); err != nil {
					t.Skipf("external scenario %s not present", name)
				}
			}
			if _, err := resource.ValidateScenario(opts); err != nil {
				t.Fatalf("check: %v", err)
			}
			a, err := NewHeadless(Config{Mode: ModeHeadless, Seed: fixtureSeed,
				Width: 160, Height: 50, Resources: opts})
			if err != nil {
				t.Fatalf("headless: %v", err)
			}
			defer a.Close()
			a.Tick(60)
			var placed bool
			a.World().RunSafe(func() { _, placed = a.World().LocalCursor() })
			if !placed {
				t.Fatal("no cursor on the map after 60 ticks")
			}
			a.Context().PushEventOrigin(event.EventCursorSpawnRequest, &event.CursorSpawnRequestPayload{
				Slot: 1, X: 2, Y: 2, Heat: 10, Energy: 100, Control: uint8(component.ControlRemote), PeerID: 2,
			}, event.OriginDebug)
			a.Settle()
			remote := a.World().Resources.Player.Slot(1)
			resets := 0
			a.SetDispatchTap(func(ev event.GameEvent) {
				if ev.Type == event.EventGameResetRequest {
					resets++
				}
				if p, ok := ev.Payload.(*event.LevelSetupPayload); ok && p.ClearEntities && !p.PreserveLoot {
					resets++
				}
			})
			a.Context().PushCrossing(event.EventCursorDefeatState, &event.CursorDefeatStatePayload{Entity: remote, Defeated: true, ProducedTick: a.World().Resources.Game.State.GetGameTicks()})
			a.Tick(110)
			if resets == 0 {
				t.Fatal("a remote participant's defeat did not reset the run while the local cursor survived")
			}

		})
	}
}

// TestSimonTakesBackWhatItShowed plays the shipped simon scenario through each way a
// press arrives: a typed key, a click on the button, and a bot's press. Each level
// shows one more note, a press sounds once on the pressing instance, a key the game
// is not taking crosses nothing, and a wrong note starts a new game.
func TestSimonTakesBackWhatItShowed(t *testing.T) {
	t.Parallel()
	opts := resource.Options{Dir: soakRoot, Scenario: "simon"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("simon scenario not present")
	}
	a, err := NewHeadless(Config{Mode: ModeHeadless, Seed: fixtureSeed, Width: 80, Height: 24, Resources: opts})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	w := a.World()
	state := func() string { return w.Resources.Status.Strings.Get("fsm.simon.state").Load() }
	var shown []int
	var tones, presses int
	a.SetDispatchTap(func(ev event.GameEvent) {
		switch p := ev.Payload.(type) {
		case *event.ButtonLightRequestPayload:
			if state() == "Listen" {
				shown = append(shown, p.Button)
			}
		case *event.SoundRequestPayload:
			if strings.HasPrefix(p.Name, "tone_") {
				tones++
			}
		case *event.InputPayload:
			if ev.Type == event.EventInputPressed {
				presses++
			}
		}
	})
	await := func(want string) {
		t.Helper()
		for range 400 {
			if state() == want {
				return
			}
			a.Tick(1)
		}
		t.Fatalf("simon stayed in %s, never reached %s", state(), want)
	}
	press := func(intent *input.Intent, want int) {
		t.Helper()
		tones, presses = 0, 0
		if intent != nil { // a key the parser keeps as a count
			a.Inject(intent)
		}
		if presses != want || tones != want {
			t.Fatalf("press %v crossed %d and sounded %d times, want %d", intent, presses, tones, want)
		}
	}
	key := func(k rune) *input.Intent {
		return a.inputMachine.Process(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: k})
	}
	// Energy reads the level, and each arm refills the heat the turn's ember burns
	score := func(level int64) {
		t.Helper()
		cursor := w.Resources.Player.Entity
		energy, _ := w.Components.Energy.GetComponent(cursor)
		heat, _ := w.Components.Heat.GetComponent(cursor)
		if energy.Current != level || heat.Current != parameter.HeatMax {
			t.Fatalf("energy %d heat %d at level %d", energy.Current, heat.Current, level)
		}
	}

	await("Ready") // the legend runs out unless a press cuts it short
	press(key('1'), 1)
	await("Listen")
	shown = nil
	press(key('1'), 0) // between turns 1 is a count again
	await("Repeat")
	if len(shown) != 1 {
		t.Fatalf("level 1 showed %v", shown)
	}

	// A click presses its button, held until the release
	config := w.Resources.Config
	x, y, bw, bh := component.ButtonRect(shown[0], w.Components.Button.CountEntities(), config.MapWidth, config.MapHeight)
	press(&input.Intent{Type: input.IntentMouseLeftDown, X: x + bw/2, Y: y + bh/2, MapCell: true}, 1)
	held := func() (n int) {
		w.Components.Button.Each(func(_ core.Entity, b *component.ButtonComponent) bool {
			if b.Held {
				n++
			}
			return true
		})
		return n
	}
	if held() != 1 {
		t.Fatal("a pointer press did not hold its button")
	}
	a.Inject(&input.Intent{Type: input.IntentMouseLeftUp})
	if held() != 0 {
		t.Fatal("the release did not let the button go")
	}

	await("Cleared")
	shown = nil
	await("Repeat")
	if len(shown) != 2 {
		t.Fatalf("level 2 showed %v", shown)
	}
	score(2)
	for _, b := range shown {
		bot, err := input.IntentFor(input.PressAction, 1, strconv.Itoa(b))
		if err != nil {
			t.Fatal(err)
		}
		press(&bot, 1)
	}

	await("Cleared")
	shown = nil
	await("Repeat")
	press(key(rune('1'+shown[0]%4)), 1) // the next button over is the wrong note
	await("Wrong")
	await("NewGame")
	score(1)
}

// TestShippedConfigsSpawnTowersThatOwnThemselves covers both shipped scenarios'
// tower requests: a tower is the session's, spawned without a cursor named and
// owned by its own header, so no player's weapon targets it.
func TestShippedConfigsSpawnTowersThatOwnThemselves(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		dir  string
		// spawn enters the tower region where the escalation chain would not reach
		// it inside a test-length run.
		spawn bool
	}{
		{"td", "../../wad/scenario/td", false},
		{"main", soakScenarioDir, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := os.Stat(filepath.Join(tc.dir, paths.ScenarioFile)); err != nil {
				t.Skipf("external scenario %s not present", tc.dir)
			}
			cfg := Config{
				Mode: ModeHeadless, Seed: fixtureSeed, Width: 160, Height: 50,
				Resources: resource.Options{Scenario: tc.dir},
			}
			if _, err := os.Stat(soakContentDir); err == nil {
				cfg.Resources.Content = soakContentDir
			}
			a, err := NewHeadless(cfg)
			if err != nil {
				t.Fatalf("headless: %v", err)
			}
			defer a.Close()

			a.Tick(1)
			if tc.spawn {
				a.Region(event.RegionSpawn, "tower", "TowerSetup")
				a.Tick(2)
			} else {
				a.Tick(11)
			}

			towers := a.World().Components.Tower.Entities()
			if len(towers) == 0 {
				t.Fatal("the config did not spawn a tower")
			}
			for _, tower := range towers {
				combat, ok := a.World().Components.Combat.GetComponent(tower)
				if !ok || combat.OwnerEntity != tower {
					t.Fatalf("tower %d owner = %d, want itself", tower, combat.OwnerEntity)
				}
			}
		})
	}
}

// TestReplaySoakTower spawns the tower region outright rather than waiting for the
// escalation chain, which no 200-step run reaches, then soaks the same way.
func TestReplaySoakTower(t *testing.T) {
	t.Parallel()
	n := soakScale(2, 3, 30)
	for i := range n {
		seed := uint64(soakSeedBase) + 0x2000 + uint64(i)
		t.Run(strconv.FormatUint(seed, 16), func(t *testing.T) {
			t.Parallel()
			opt := journal.DefaultFuzz(seed, soakSteps)
			opt.RegionSet = towerRegions
			run := runSoakScriptCfg(t, towerScenario(t, seed), opt, func(a *App) {
				a.Tick(1) // boot and capture player_entity before TowerSetup reads it
				a.Region(event.RegionSpawn, "tower", "TowerSetup")
				a.Tick(3)
			})
			if err := replaySoak(run, run.cap.Records()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// runSoakScriptCfg is runSoakScript with the config and options injected and
// a prelude called before journal.RunFuzz. The prelude's Region call is OriginDebug,
// so it journals and replays like any other record.
func runSoakScriptCfg(t *testing.T, cfg Config, opt journal.FuzzOptions, prelude func(*App)) soakRun {
	t.Helper()

	cap := journal.NewCapture()
	cfg.Journal, cfg.JournalSink = true, cap

	a, err := NewHeadless(cfg)
	if err != nil {
		t.Fatalf("source run: %v", err)
	}

	if prelude != nil {
		prelude(a)
	}

	if _, err := journal.RunFuzz(a, opt); err != nil {
		a.Close()
		t.Fatalf("script: %v", err)
	}

	reg := a.World().Resources.Status
	if n := reg.Ints.Get("engine.tick_slips").Load(); n != 0 {
		t.Errorf("engine.tick_slips = %d; a manual clock cannot slip", n)
	}
	if n := reg.Ints.Get("event.dropped").Load(); n != 0 {
		t.Errorf("event.dropped = %d; the queue overran and lost state", n)
	}
	if _, encFail := a.JournalStats(); encFail != 0 {
		t.Errorf("%d payload encode failures", encFail)
	}

	run := soakRun{
		cap:  cap,
		want: a.SnapshotSimulation(),
		end:  a.Position(),
		seed: opt.Seed,
		root: cfg.Resources.Dir,
	}
	a.Close()

	if err := cap.CheckDense(); err != nil {
		t.Error(err)
	}
	if len(cap.Records()) == 0 {
		t.Fatal("no records captured")
	}
	return run
}

// runSoakScript drives one seeded script under a capture sink, asserting the run
// itself was clean before any replay is attempted
func runSoakScript(t *testing.T, seed uint64, steps int) soakRun {
	t.Helper()
	return runSoakScriptCfg(t, scriptConfig(seed), journal.DefaultFuzz(seed, steps), nil)
}

// replaySoak reproduces a source run from its capture
func replaySoak(run soakRun, recs []event.JournalRecord) error {
	return replayInto(run.root, run.cap.Anchors(), recs, run.want, run.end)
}

// TestReplaySoak drives seeded scripts through journal, replay and comparison.
func TestReplaySoak(t *testing.T) {
	t.Parallel()
	n := soakScale(2, 4, 120)
	for i := range n {
		seed := uint64(soakSeedBase) + uint64(i)
		t.Run(strconv.FormatUint(seed, 16), func(t *testing.T) {
			t.Parallel()
			run := runSoakScript(t, seed, soakSteps)
			if err := replaySoak(run, run.cap.Records()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestSoakSessionStateStaysOperator flips operator-owned state between actions and
// asserts the simulation view is byte-identical to an unperturbed run of the same
// seed. AutoFire is excluded: it produces journaled events, so it is simulation input.
func TestSoakSessionStateStaysOperator(t *testing.T) {
	t.Parallel()
	const seed = soakSeedBase + 0x900

	plain, err := NewHeadless(scriptConfig(seed))
	if err != nil {
		t.Fatalf("plain run: %v", err)
	}
	if _, err := journal.RunFuzz(plain, journal.DefaultFuzz(seed, soakSteps)); err != nil {
		t.Fatalf("plain script: %v", err)
	}
	want := plain.SnapshotSimulation()
	plain.Close()

	noisy, err := NewHeadless(scriptConfig(seed))
	if err != nil {
		t.Fatalf("perturbed run: %v", err)
	}
	defer noisy.Close()

	rng := vmath.NewSeededRand(seed, "perturb")
	opt := journal.DefaultFuzz(seed, soakSteps)
	opt.Perturb = func() {
		ctx := noisy.Context()
		ctx.MouseDisabled.Store(rng.Intn(2) == 0)
		ctx.MouseFreeMode.Store(rng.Intn(2) == 0)
		ctx.OverlayHUD.Store(rng.Intn(2) == 0)
		ctx.ToggleOverlayPin("engine")
		ctx.SetStatusMessage("perturb", parameter.StatusAlert)
		ctx.IncrementFrameNumber()
	}
	if _, err := journal.RunFuzz(noisy, opt); err != nil {
		t.Fatalf("perturbed script: %v", err)
	}

	if i, x, y, ok := snapshot.FirstDiff(want, noisy.SnapshotSimulation()); ok {
		t.Fatalf("operator state reached the simulation view at line %d:\n  plain %s\n  noisy %s", i, x, y)
	}
}

// --- Negative controls ---

// mutation perturbs a record stream; ok is false when the stream offers no site.
// The site is drawn from the seed's own stream: taking the first eligible one tested
// the same early, commuting pair on every seed.
type mutation func(rng *vmath.FastRand, recs []event.JournalRecord) ([]event.JournalRecord, string, bool)

// pick draws one of the listed sites, -1 when there are none
func pick(rng *vmath.FastRand, sites []int) int {
	if len(sites) == 0 {
		return -1
	}
	return sites[rng.Intn(len(sites))]
}

// groupPairs lists indices i where i and i+1 share a settle group. distinct keeps
// only pairs of differing event types, where dispatch order can actually matter.
func groupPairs(recs []event.JournalRecord, distinct bool) []int {
	var out []int
	for i := 0; i+1 < len(recs); i++ {
		if !journal.SameReplayGroup(recs[i], recs[i+1]) || recs[i].Seq == recs[i+1].Seq {
			continue
		}
		if distinct && recs[i].Type == recs[i+1].Type {
			continue
		}
		out = append(out, i)
	}
	return out
}

// orderSensitive lists event types whose dispatch order against another such type
// changes shared world state. Control, view and operator events are excluded: they
// either commute or land in denySim, so swapping them proves nothing.
var orderSensitive = map[event.EventType]bool{
	event.EventCursorMoveRequest: true,
	event.EventCharacterTyped:    true,
	event.EventDeleteRequest:     true,
	event.EventWeaponFireRequest: true,
	event.EventNuggetJumpRequest: true,
	event.EventGoldJumpRequest:   true,
}

// swapInGroup swaps the queue slots of two order-sensitive records sharing a settle
// group, which is what the driver sorts by; reordering the slice alone would be undone
func swapInGroup(rng *vmath.FastRand, recs []event.JournalRecord) ([]event.JournalRecord, string, bool) {
	var sites []int
	for _, i := range groupPairs(recs, true) {
		if orderSensitive[recs[i].Type] && orderSensitive[recs[i+1].Type] {
			sites = append(sites, i)
		}
	}
	i := pick(rng, sites)
	if i < 0 {
		return recs, "", false
	}
	what := fmt.Sprintf("swapped the slots of jseq %d (%s) and %d (%s)",
		recs[i].JSeq, event.GetEventName(recs[i].Type),
		recs[i+1].JSeq, event.GetEventName(recs[i+1].Type))
	recs[i].Seq, recs[i+1].Seq = recs[i+1].Seq, recs[i].Seq
	return recs, what, true
}

// dropRecord removes one record from the interior of the stream
func dropRecord(rng *vmath.FastRand, recs []event.JournalRecord) ([]event.JournalRecord, string, bool) {
	if len(recs) < 3 {
		return recs, "", false
	}
	i := 1 + rng.Intn(len(recs)-2)
	what := fmt.Sprintf("dropped jseq %d (%s)", recs[i].JSeq, event.GetEventName(recs[i].Type))
	return append(recs[:i:i], recs[i+1:]...), what, true
}

// Mutate the final move so a later absolute placement cannot erase the change.
func mutatePayload(_ *vmath.FastRand, recs []event.JournalRecord) ([]event.JournalRecord, string, bool) {
	for i := len(recs) - 1; i >= 0; i-- {
		if recs[i].Type != event.EventCursorMoveRequest {
			continue
		}
		decoded, err := event.DecodePayload(recs[i].Type, recs[i].Payload)
		p, ok := decoded.(*event.CursorMoveRequestPayload)
		if err != nil || !ok {
			continue
		}
		x := 1
		if p.X == x {
			x = 2
		}
		f, encErr := event.NewWireFrame(event.GameEvent{Type: recs[i].Type,
			Payload: &event.CursorMoveRequestPayload{Entity: p.Entity, X: x, Y: 1}})
		if encErr != "" {
			continue
		}
		recs[i].Payload = f.Payload
		return recs, fmt.Sprintf("rewrote final cursor move jseq %d", recs[i].JSeq), true
	}
	return recs, "", false
}

// TestReplaySoakNegative asserts a perturbed record stream is caught. Individual
// mutations can commute, but each control must bite on at least one seed.
func TestReplaySoakNegative(t *testing.T) {
	t.Parallel()
	controls := []struct {
		name string
		fn   mutation
	}{
		{"reorder-in-group", swapInGroup},
		{"drop-record", dropRecord},
		{"mutate-payload", mutatePayload},
	}

	seeds := soakScale(2, 2, 12)
	caught := make([]int, len(controls))
	applied := make([]int, len(controls))

	for i := range seeds {
		seed := uint64(soakSeedBase) + 0x1000 + uint64(i)
		run := runSoakScript(t, seed, soakSteps)

		// The unperturbed stream must reproduce, or the controls prove nothing
		if err := replaySoak(run, run.cap.Records()); err != nil {
			t.Errorf("seed %#x baseline: %v", seed, err)
			continue // controls prove nothing against a stream that already diverges
		}

		for c := range controls {
			rng := vmath.NewSeededRand(seed, "mutate:"+controls[c].name)
			recs, what, ok := controls[c].fn(rng, run.cap.Records())
			if !ok {
				continue
			}
			applied[c]++
			if err := replaySoak(run, recs); err != nil {
				caught[c]++
				continue
			}
			t.Logf("%s: seed %#x %s did not diverge", controls[c].name, seed, what)
		}
	}

	for c := range controls {
		switch {
		case applied[c] == 0:
			// Not a failure and not a reason to abandon the other controls: the
			// generator simply produced no eligible site on these seeds.
			t.Logf("%s: no stream offered a site", controls[c].name)
		case caught[c] == 0:
			t.Errorf("%s: %d perturbed streams all reproduced; the replay is not sensitive to it",
				controls[c].name, applied[c])
		default:
			t.Logf("%s: caught %d of %d", controls[c].name, caught[c], applied[c])
		}
	}
}

// soakSnapshot drives one seeded script and returns its simulation view
func soakSnapshot(t *testing.T, seed uint64) []string {
	t.Helper()
	a, err := NewHeadless(scriptConfig(seed))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	defer a.Close()
	if _, err := journal.RunFuzz(a, journal.DefaultFuzz(seed, soakSteps)); err != nil {
		t.Fatalf("script: %v", err)
	}
	return a.SnapshotSimulation()
}

// TestSoakAppsAreIndependent asserts two sequential runs of one seed agree, so a
// replay's baseline is the seed and not what the previous App left in package state
func TestSoakAppsAreIndependent(t *testing.T) {
	t.Parallel()
	for _, seed := range []uint64{0x50a4002d, 0x50a40065, 0x50a41006} {
		t.Run(strconv.FormatUint(seed, 16), func(t *testing.T) {
			t.Parallel()
			first := soakSnapshot(t, seed)
			if i, x, y, ok := snapshot.FirstDiff(first, soakSnapshot(t, seed)); ok {
				t.Fatalf("two runs of one seed differ at line %d:\n  first  %s\n  second %s", i, x, y)
			}
		})
	}
}

// TestDomainAuditSoakClean asserts the audit counts zero over a full soak, which is
// 4.16(3) without a log grep. The pin is process-wide: never t.Parallel here.
// The pin also survives between ticks, so component attaches made by event handlers
// outside processTick are audited too.
func TestDomainAuditSoakClean(t *testing.T) {
	engine.PinDomainAudit(true)
	defer engine.PinDomainAudit(false)

	a := mustHeadless(t, 0xD0A17, 120, 40)
	defer a.Close()

	if _, err := journal.RunFuzz(a, journal.DefaultFuzz(0xD0A17, soakScale(600, 1500, 4000))); err != nil {
		t.Fatalf("soak: %v", err)
	}
	if n := engine.DomainMismatches(); n != 0 {
		t.Fatalf("domain audit counted %d violations:\n  %s",
			n, strings.Join(engine.DomainViolations(), "\n  "))
	}
}
