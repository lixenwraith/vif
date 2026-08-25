package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// Soak profile. Each iteration is reproducible from its seed: rerun one with
// -run 'TestReplaySoak/<seed>'.
const (
	soakSeedBase   = 0x50a4_0000
	soakIterations = 120
	soakSteps      = 200
	soakShortIters = 8
)

// soakRun is one journalled source run and everything a replay needs to reproduce it
type soakRun struct {
	cap  *Capture
	want []string
	end  event.Stamp
	seed uint64
}

// soakConfigDir is the external map set the tower soak drives; the tower region is
// the only path that engages gateway, eye and route-graph navigation
const soakConfigDir = "../../config/main"
const soakContentDir = "../../data"

// towerRegions mirrors config/main's declared regions and their entry states
var towerRegions = []ScriptRegion{
	{"main", "MainSpawnGold"},
	{"quasar", "QuasarFuse"},
	{"storm", "StormSetup"},
	{"monitor", "MonitorWarmup"},
	{"tower", "TowerSetup"},
}

// towerConfig pins the external map set and a viewport the tower layout fits in
func towerConfig(t *testing.T, seed uint64) Config {
	t.Helper()
	if _, err := os.Stat(filepath.Join(soakConfigDir, parameter.GameConfigFile)); err != nil {
		t.Skipf("external map set %s not present", soakConfigDir)
	}
	cfg := Config{Mode: ModeHeadless, Seed: seed, GameScript: soakConfigDir, Width: 160, Height: 50}
	if _, err := os.Stat(soakContentDir); err == nil {
		cfg.ContentPath = soakContentDir
	}
	return cfg
}

// TestTowerDefenseConfigCursorOwnership covers the external producer side of
// cursor addressing. The TD machine must spawn and capture a cursor before its
// tower requests inject player_entity into their payloads.
func TestTowerDefenseConfigCursorOwnership(t *testing.T) {
	const tdConfigDir = "../../config/td"
	if _, err := os.Stat(filepath.Join(tdConfigDir, parameter.GameConfigFile)); err != nil {
		t.Skipf("external map set %s not present", tdConfigDir)
	}

	cfg := Config{Mode: ModeHeadless, Seed: fixtureSeed, GameScript: tdConfigDir, Width: 160, Height: 50}
	if _, err := os.Stat(soakContentDir); err == nil {
		cfg.ContentPath = soakContentDir
	}
	a, err := NewHeadless(cfg)
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	a.Tick(12)
	player := a.World().Resources.Player.Entity
	if player == 0 {
		t.Fatal("TD config did not spawn a cursor")
	}

	towers := a.World().Components.Tower.Entities()
	if len(towers) == 0 {
		t.Fatal("TD config did not spawn an explicitly owned tower")
	}
	for _, tower := range towers {
		combat, ok := a.World().Components.Combat.GetComponent(tower)
		if !ok || combat.OwnerEntity != player {
			t.Fatalf("tower %d owner = %d, want cursor %d", tower, combat.OwnerEntity, player)
		}
	}
}

func TestMainTowerConfigCursorOwnership(t *testing.T) {
	a, err := NewHeadless(towerConfig(t, fixtureSeed))
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	a.Tick(1)
	player := a.World().Resources.Player.Entity
	if player == 0 {
		t.Fatal("main config did not spawn a cursor")
	}
	a.Region(event.RegionSpawn, "tower", "TowerSetup")
	a.Tick(2)

	towers := a.World().Components.Tower.Entities()
	if len(towers) == 0 {
		t.Fatal("main config did not spawn an explicitly owned tower")
	}
	combat, ok := a.World().Components.Combat.GetComponent(towers[0])
	if !ok || combat.OwnerEntity != player {
		t.Fatalf("tower %d owner = %d, want cursor %d", towers[0], combat.OwnerEntity, player)
	}
}

// TestReplaySoakTower spawns the tower region outright rather than waiting for the
// escalation chain, which no 200-step run reaches, then soaks the same way
func TestReplaySoakTower(t *testing.T) {
	n := soakIterations / 4
	if testing.Short() {
		n = soakShortIters
	}
	for i := range n {
		seed := uint64(soakSeedBase) + 0x2000 + uint64(i)
		t.Run(strconv.FormatUint(seed, 16), func(t *testing.T) {
			opt := DefaultScript(seed, soakSteps)
			opt.RegionSet = towerRegions
			run := runSoakScriptCfg(t, towerConfig(t, seed), opt, func(a *App) {
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
// a prelude called before RunScript. The prelude's Region call is OriginDebug,
// so it journals and replays like any other record.
func runSoakScriptCfg(t *testing.T, cfg Config, opt ScriptOptions, prelude func(*App)) soakRun {
	t.Helper()

	cap := NewCapture()
	cfg.Journal, cfg.JournalSink = true, cap

	a, err := NewHeadless(cfg)
	if err != nil {
		t.Fatalf("source run: %v", err)
	}

	if prelude != nil {
		prelude(a)
	}

	if _, err := RunScript(a, opt); err != nil {
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
	return runSoakScriptCfg(t, scriptConfig(seed), DefaultScript(seed, steps), nil)
}

// replaySoak reproduces a source run from its capture
func replaySoak(run soakRun, recs []event.JournalRecord) error {
	return replayInto(run.cap.Anchors(), recs, run.want, run.end)
}

// TestReplaySoak drives many seeded scripts through journal → replay → FirstDiff
func TestReplaySoak(t *testing.T) {
	n := soakIterations
	if testing.Short() {
		n = soakShortIters
	}
	for i := range n {
		seed := uint64(soakSeedBase) + uint64(i)
		t.Run(strconv.FormatUint(seed, 16), func(t *testing.T) {
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
	const seed = soakSeedBase + 0x900

	plain, err := NewHeadless(scriptConfig(seed))
	if err != nil {
		t.Fatalf("plain run: %v", err)
	}
	if _, err := RunScript(plain, DefaultScript(seed, soakSteps)); err != nil {
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
	opt := DefaultScript(seed, soakSteps)
	opt.Perturb = func(a *App) {
		ctx := a.Context()
		ctx.MouseDisabled.Store(rng.Intn(2) == 0)
		ctx.MouseFreeMode.Store(rng.Intn(2) == 0)
		ctx.OverlayHUD.Store(rng.Intn(2) == 0)
		ctx.ToggleOverlayPin("engine")
		ctx.SetStatusMessage("perturb", 0, true)
		ctx.IncrementFrameNumber()
	}
	if _, err := RunScript(noisy, opt); err != nil {
		t.Fatalf("perturbed script: %v", err)
	}

	if i, x, y, ok := FirstDiff(want, noisy.SnapshotSimulation()); ok {
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
		if keyOf(recs[i]) != keyOf(recs[i+1]) || recs[i].Seq == recs[i+1].Seq {
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

// splitGroup moves a group's last record into the next settle, which the source applied together
func splitGroup(rng *vmath.FastRand, recs []event.JournalRecord) ([]event.JournalRecord, string, bool) {
	var sites []int
	for i := 1; i < len(recs); i++ {
		if keyOf(recs[i-1]) != keyOf(recs[i]) {
			continue // starts a group rather than ending one
		}
		if i+1 < len(recs) && keyOf(recs[i+1]) == keyOf(recs[i]) {
			continue
		}
		sites = append(sites, i)
	}
	i := pick(rng, sites)
	if i < 0 {
		return recs, "", false
	}
	recs[i].Boundary++
	return recs, fmt.Sprintf("moved jseq %d (%s) into the next settle",
		recs[i].JSeq, event.GetEventName(recs[i].Type)), true
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

// mutatePayload rewrites one recorded cursor move command.
func mutatePayload(rng *vmath.FastRand, recs []event.JournalRecord) ([]event.JournalRecord, string, bool) {
	const forced = "entity = 1\nx = 1\ny = 1\n"
	var sites []int
	for i := range recs {
		if recs[i].Type == event.EventCursorMoveRequest && recs[i].Payload != forced {
			sites = append(sites, i)
		}
	}
	i := pick(rng, sites)
	if i < 0 {
		return recs, "", false
	}
	recs[i].Payload = forced
	return recs, fmt.Sprintf("rewrote jseq %d cursor position", recs[i].JSeq), true
}

// TestReplaySoakNegative asserts a perturbed record stream is caught. Individual
// mutations can commute, but each control must bite on at least one seed.
func TestReplaySoakNegative(t *testing.T) {
	controls := []struct {
		name string
		fn   mutation
	}{
		{"reorder-in-group", swapInGroup},
		{"move-across-settle", splitGroup},
		{"drop-record", dropRecord},
		{"mutate-payload", mutatePayload},
	}

	seeds := 12
	if testing.Short() {
		seeds = 4
	}
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
			t.Skipf("%s: no stream offered a site; the generator no longer produces one", controls[c].name)
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
	if _, err := RunScript(a, DefaultScript(seed, soakSteps)); err != nil {
		t.Fatalf("script: %v", err)
	}
	return a.SnapshotSimulation()
}

// TestSoakAppsAreIndependent asserts two sequential runs of one seed agree, so a
// replay's baseline is the seed and not what the previous App left in package state
func TestSoakAppsAreIndependent(t *testing.T) {
	for _, seed := range []uint64{0x50a4002d, 0x50a40065, 0x50a41006} {
		t.Run(strconv.FormatUint(seed, 16), func(t *testing.T) {
			first := soakSnapshot(t, seed)
			if i, x, y, ok := FirstDiff(first, soakSnapshot(t, seed)); ok {
				t.Fatalf("two runs of one seed differ at line %d:\n  first  %s\n  second %s", i, x, y)
			}
		})
	}
}
