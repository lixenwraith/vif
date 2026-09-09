package app

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vi-fighter/internal/asset"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/fsm"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/journal"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/resource"
)

// sharedGlyphs returns the shared-domain glyph entities and the ones that are not
// gold composite members
func sharedGlyphs(a *App) (count int, bad []string) {
	a.World().RunSafe(func() {
		w := a.World()
		w.Components.Glyph.Each(func(e core.Entity, g *component.GlyphComponent) bool {
			if e.Domain() != core.DomainShared {
				return true
			}
			count++
			if !w.Components.Member.HasEntity(e) || g.Type != component.GlyphGold {
				bad = append(bad, fmt.Sprintf("entity %d type %d", e.ID(), g.Type))
			}
			return true
		})
	})
	return count, bad
}

// TestSharedGlyphsAreGoldMembersOnly pins the one shared glyph population. Every
// other glyph is player-domain, which is what lets typing, cleaner and dust consume
// them without a crossing, and what keeps screen noise off the wire.
func TestSharedGlyphsAreGoldMembersOnly(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 0x901D, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)

	// Deterministic phase: force a gold sequence, so the invariant is not vacuous
	a.Context().PushEventOrigin(event.EventGoldSpawnRequest, nil, event.OriginDebug)
	a.Settle()
	a.Tick(2)

	count, bad := sharedGlyphs(a)
	if count != parameter.GoldSequenceLength {
		t.Fatalf("%d shared glyphs after a gold spawn, want %d", count, parameter.GoldSequenceLength)
	}
	if len(bad) > 0 {
		t.Fatalf("shared glyphs that are not gold members:\n  %s", strings.Join(bad, "\n  "))
	}

	// Soak phase: no other shared glyph population may appear
	if _, err := journal.RunFuzz(a, journal.DefaultFuzz(0x901D, 1200)); err != nil {
		t.Fatalf("soak: %v", err)
	}
	if _, bad = sharedGlyphs(a); len(bad) > 0 {
		t.Fatalf("shared glyphs that are not gold members:\n  %s", strings.Join(bad, "\n  "))
	}
}

// fsmConfigTrees names every configuration a build can boot: the two shipped
// scripts, the empty one, and the copy embedded in the binary. A rule that holds
// for one of them and not the others is not a rule.
func fsmConfigTrees(t *testing.T) map[string]func() (map[string]any, error) {
	t.Helper()
	root := repoRoot(t)
	trees := map[string]func() (map[string]any, error){
		"asset(embedded)": func() (map[string]any, error) {
			return fsm.ResolveConfig(asset.DefaultFSMConfig, asset.DefaultFSMEntry)
		},
	}
	for _, dir := range []string{"game", "games/td", "games/blank"} {
		d := filepath.Join(root, "wad", dir)
		if _, err := os.Stat(filepath.Join(d, "game.toml")); err != nil {
			continue
		}
		trees["wad/"+dir] = func() (map[string]any, error) {
			return fsm.ResolveConfig(os.DirFS(d), "game.toml")
		}
	}
	return trees
}

// repoRoot walks up from the test's working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for range 8 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("module root not found above the test's working directory")
	return ""
}

// TestFSMTriggersAreReplicated is D-20 made mechanical: every FSM region is shared
// state, so a region stays in agreement only if every event that moves it is an
// event every instance holds. A ClassLocal trigger never replicates, so the region
// advances on the producing instance and nowhere else, and nothing re-derives the
// missing event — which is how MonitorActive on EventHeatBurst diverged a session.
func TestFSMTriggersAreReplicated(t *testing.T) {
	t.Parallel()
	event.EnsureRegistry()

	totalChecked := 0
	trees := fsmConfigTrees(t)
	if len(trees) < 2 {
		t.Fatalf("only %d config tree(s) reachable; the check would barely cover anything", len(trees))
	}
	for name, load := range trees {
		t.Run(name, func(t *testing.T) {
			merged, err := load()
			if err != nil {
				t.Fatalf("resolve config: %v", err)
			}
			var root fsm.RootConfig
			if err := toml.Decode(merged, &root); err != nil {
				t.Fatalf("decode config: %v", err)
			}
			if len(root.States) == 0 {
				t.Fatal("no states decoded; the check would pass vacuously")
			}

			var offenders []string
			checked := 0
			for stateName, state := range root.States {
				if state == nil {
					continue
				}
				for _, tr := range state.Transitions {
					// "Tick" is the machine's own pulse, not a game event, and it
					// arrives on every instance by construction.
					if tr.Trigger == "" || tr.Trigger == "Tick" {
						continue
					}
					et, ok := event.GetEventType(tr.Trigger)
					if !ok {
						offenders = append(offenders, stateName+": unknown trigger "+tr.Trigger)
						continue
					}
					checked++
					// Stamped is resolved per event from the producer's domain, so
					// the type alone cannot condemn it; a Stamped trigger is the one
					// case this check hands to the producer's own domain rules.
					if c := event.ClassOf(et); c == event.ClassLocal || c == event.ClassUnset {
						offenders = append(offenders,
							stateName+" transitions on "+tr.Trigger+" ("+c.String()+")")
					}
				}
			}
			// wad/games/blank declares no transitions at all, so a per-tree floor
			// would fail it; the suite-wide floor below is what keeps the check
			// from passing vacuously.
			totalChecked += checked
			if len(offenders) > 0 {
				sort.Strings(offenders)
				t.Fatalf("a shared FSM region is steered by an event that does not "+
					"replicate, so only the producing instance advances it:\n  %v",
					offenders)
			}
		})
	}
	if totalChecked == 0 {
		t.Fatal("no event triggers found in any config tree; the check passed vacuously")
	}
}

// targetFields name the receiving side of a payload. The emitter side is asserted
// unconditionally: D-4 reduces a player emitter to HasOrigin/OriginX/Y on every
// instance, crossing or not.
var targetFields = map[string]bool{
	"TargetEntity": true, "HitEntity": true, "HitEntities": true,
}

var entityType = reflect.TypeOf(core.Entity(0))

// TestAnInstalledPositionReconcilesItsRegionsSystems is D-20's per-instance half.
// A region's declared system toggles are an effect of the Shared position that
// owns them, so they are re-derived from it — and a participant that reaches that
// position by installing a world runs no region's entry actions.
func TestAnInstalledPositionReconcilesItsRegionsSystems(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 0x61A7, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)

	glyph := func() bool {
		var on bool
		a.World().RunSafe(func() { on = a.World().Resources.Status.Bools.Get("glyph.enabled").Load() })
		return on
	}
	if !glyph() {
		t.Fatal("the main region declares glyph enabled and it is not")
	}
	a.Context().PushEventOrigin(event.EventMetaSystemCommandRequest,
		&event.MetaSystemCommandPayload{SystemName: "glyph", Enabled: false}, event.OriginDebug)
	a.Settle()
	if glyph() {
		t.Fatal("glyph kept running through its own disable")
	}

	a.World().RunSafe(func() {
		if err := a.scheduler.ImportFSM(a.scheduler.ExportFSM(), true); err != nil {
			t.Errorf("import fsm: %v", err)
		}
	})
	a.Settle()
	if !glyph() {
		t.Fatal("the installed position left glyph as this instance had it, not as its region declares")
	}
}

// TestBusPayloadsNameOnlySharedEntities asserts D-4 over a soak: a record that
// replicates names only shared entities. The transported set comes from the class
// table rather than a hand-list, and a record that does not replicate constrains
// nothing and is skipped whole. The tap runs on the caller's goroutine — a driven
// App has no scheduler — so it needs no synchronization.
func TestBusPayloadsNameOnlySharedEntities(t *testing.T) {
	t.Parallel()
	const seed, steps = 0x4B15, 1500 // This seed produces no crossing inside the old 300-step short horizon.

	a := mustHeadless(t, seed, 120, 40)
	defer a.Close()

	named, crossings := 0, 0
	seen := make(map[string]bool)
	var bad []string
	a.SetDispatchTap(func(ev event.GameEvent) {
		if ev.Payload == nil || !event.Replicated(ev.Type, ev.Domain) {
			return
		}
		crossings++
		entityScan(reflect.ValueOf(ev.Payload), event.GetEventName(ev.Type), "",
			true, &named, func(msg string) {
				if !seen[msg] {
					seen[msg] = true
					bad = append(bad, msg)
				}
			})
	})

	if _, err := journal.RunFuzz(a, journal.DefaultFuzz(seed, steps)); err != nil {
		t.Fatalf("soak: %v", err)
	}
	if named == 0 {
		t.Fatal("no replicated payload named an entity; the soak asserts nothing")
	}
	t.Logf("inspected %d entity references across %d replicated records", named, crossings)
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatalf("D-4 violations:\n  %s", strings.Join(bad, "\n  "))
	}
}

// entityScan walks a payload, counting the entities it names and reporting each one
// that is not shared. Target fields are skipped when the instance is not a crossing.
func entityScan(v reflect.Value, path, field string, crossing bool, named *int, report func(string)) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			entityScan(v.Elem(), path, field, crossing, named, report)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			entityScan(v.Index(i), fmt.Sprintf("%s[%d]", path, i), field, crossing, named, report)
		}
	case reflect.Struct:
		t := v.Type()
		for i := range v.NumField() {
			name := t.Field(i).Name
			if !crossing && targetFields[name] {
				continue
			}
			entityScan(v.Field(i), path+"."+name, name, crossing, named, report)
		}
	default:
		if v.Type() != entityType {
			return
		}
		e := core.Entity(v.Uint())
		if e == 0 {
			return
		}
		*named++
		if e.Domain() != core.DomainShared {
			report(path + " names a " + e.Domain().String() + " entity")
		}
	}
}

// unstampedLocal pins the Local-class types some producer still pushes in the
// ambient domain. The set must only shrink: an entry that stops appearing fails, a
// type not listed here fails on first sight. A per-instance effect journaled as
// shared is a record two instances legitimately differ on.
// TODO: empty this, then delete it and the exemption with it.
var unstampedLocal = map[string]bool{
	"EventCombatAttackAreaRequest":  true,
	"EventDecaySpawnOne":            true,
	"EventDustAllRequest":           true,
	"EventGamePauseChanged":         true,
	"EventGamePauseRequest":         true,
	"EventGameSpeedChanged":         true,
	"EventLightningSpawnRequest":    true,
	"EventMetaStatusMessageRequest": true,
	"EventMissileSpawnRequest":      true,
	"EventModeChanged":              true,
	"EventScreenResize":             true,
}

// TestLocalEventsCarryThePlayerDomain asserts that a Local-class record is tagged
// player. The class already keeps it off the wire, so this is about the record being
// honest. core.DomainShared is the zero value and the ambient domain defaults to it,
// so every type reported here is a push site that never stamped.
func TestLocalEventsCarryThePlayerDomain(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("soak")
	}

	const seed, steps = 0x10CA1, 1500

	a := mustHeadless(t, seed, 120, 40)
	defer a.Close()

	unstamped := make(map[string]int)
	a.SetDispatchTap(func(ev event.GameEvent) {
		if event.ClassOf(ev.Type) == event.ClassLocal && ev.Domain == core.DomainShared {
			unstamped[event.GetEventName(ev.Type)]++
		}
	})

	if _, err := journal.RunFuzz(a, journal.DefaultFuzz(seed, steps)); err != nil {
		t.Fatalf("soak: %v", err)
	}

	var bad []string
	for name, n := range unstamped {
		if !unstampedLocal[name] {
			bad = append(bad, fmt.Sprintf(
				"%s: %d records tagged shared; stamp the push site or add it to unstampedLocal", name, n))
		}
	}
	for name := range unstampedLocal {
		if unstamped[name] == 0 {
			bad = append(bad, name+": listed in unstampedLocal but every push now stamps; drop the entry")
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatalf("local-class stamping drifted:\n  %s", strings.Join(bad, "\n  "))
	}
	t.Logf("%d local-class types still push unstamped", len(unstamped))
}

// TestAQuasarsEffectsReachOnlyTheCursorItWasFusedFrom is the reported defect. A
// quasar's grayout and drain pause belong to the cursor it was fused from, but the
// region that raises them is shared, so every instance runs the same on_enter
// actions: before the scope payload, one participant's quasar darkened everyone's
// screen. This drives the whole path across two linked instances.
func TestAQuasarsEffectsReachOnlyTheCursorItWasFusedFrom(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA6A6, 2, [][2]int{{1, 2}})
	local := localCursors(t, apps)

	spawns := make([]int, len(apps))
	for i, a := range apps {
		if grayedOut(a) || drainsPaused(a) {
			t.Fatalf("participant %d starts greyed out or paused", i+1)
		}
		a.SetDispatchTap(func(ev event.GameEvent) {
			if ev.Type == event.EventQuasarSpawnRequest {
				spawns[i]++
			}
		})
		a.World().Resources.Status.Ints.Get("kills.drain").Store(9)
	}

	// Participant 2's cursor takes the tenth shared drain. The crossing reaches
	// both FSMs and both take MainEscalate, which captures the causal cursor as
	// fuse_owner and spawns the quasar region from it.
	apps[1].Context().PushCrossing(event.EventDrainDefeated,
		&event.DrainDefeatedPayload{Entity: local[1]})
	apps[1].Settle()

	// The strobe is a 200 ms flash rather than a standing state, so it is observed
	// while it is running rather than asserted at the end. It follows the same
	// cursor as the other two: the region emits it with `cursor` bound to
	// fuse_owner, and an instance that does not simulate that cursor ignores it.
	strobed := make([]bool, len(apps))
	for range 12 {
		tickAll(apps)
		for i, a := range apps {
			strobed[i] = strobed[i] || strobing(a)
		}
	}
	for i, got := range strobed {
		if want := i == 1; got != want {
			t.Fatalf("participant %d strobe = %v, want %v", i+1, got, want)
		}
	}
	for i, a := range apps {
		if quasarState(a) == "-" {
			t.Fatalf("the quasar region is not running on participant %d", i+1)
		}
		// Exactly the participant the region names, on both halves of the effect.
		want := i == 1
		if got := grayedOut(a); got != want {
			t.Fatalf("participant %d grayout = %v, want %v", i+1, got, want)
		}
		if got := drainsPaused(a); got != want {
			t.Fatalf("participant %d drain pause = %v, want %v", i+1, got, want)
		}
	}

	// The region ends, and the owner's effects end with it: a scoped hold that
	// nothing released would stop that participant's drains for the rest of the run.
	for range 24 {
		tickAll(apps)
	}
	for i, a := range apps {
		if s := quasarState(a); s != "-" {
			t.Fatalf("participant %d is still in the quasar region (%s)", i+1, s)
		}
		if grayedOut(a) || drainsPaused(a) {
			t.Fatalf("participant %d kept the quasar's effects after it ended: grayout=%v paused=%v",
				i+1, grayedOut(a), drainsPaused(a))
		}
	}

	// The shared half is unchanged: one logical fusion producing one spawn request on
	// each instance, not one per participant. Full snapshot parity is not asserted —
	// both machines enter the region a barrier apart, so their elapsed times differ by
	// the delivery lead rather than by anything this test is about.
	for i, got := range spawns {
		if got != 1 {
			t.Fatalf("participant %d observed %d quasar spawn requests, want 1", i+1, got)
		}
	}
}

// quasarState is the region's current state name, "-" while it is not running.
func quasarState(a *App) (state string) {
	a.World().RunSafe(func() {
		state = a.World().Resources.Status.Strings.Get("fsm.quasar.state").Load()
	})
	return state
}

// grayedOut reads the overlay resource the transient system owns, rather than the
// telemetry key beside it, so the assertion is on the effect and not its report.
func grayedOut(a *App) (active bool) {
	a.World().RunSafe(func() { active = a.World().Resources.View.Grayout.Active })
	return active
}

// strobing reads the flash the transient system owns, for the same reason
// grayedOut reads the overlay rather than the key beside it.
func strobing(a *App) (active bool) {
	a.World().RunSafe(func() { active = a.World().Resources.View.Strobe.Active })
	return active
}

// drainsPaused reads the drain system's published hold. The system itself is not
// reachable from here, and the key is what an operator watching a stalled session
// reads too.
func drainsPaused(a *App) (paused bool) {
	a.World().RunSafe(func() {
		paused = a.World().Resources.Status.Bools.Get("drain.paused").Load()
	})
	return paused
}

// TestAppsScopeOperatorState keeps view, help and log state on the App that owns it.
func TestAppsScopeOperatorState(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 0xA11CE, 120, 40)
	b := mustHeadless(t, 0xA11CE, 120, 40)
	defer a.Close()
	defer b.Close()

	for _, x := range []*App{a, b} {
		tickUntilCursor(t, x)
		x.SetupLevel(100, 30, true, false)
	}

	a.Resize(140, 44)
	b.Resize(90, 28)
	a.Context().PushLocal(event.EventDebugFlowToggle, nil)
	a.Settle()
	if !a.Context().NavigationDebug.ShowFlow {
		t.Fatal("instance a did not enable its flow overlay")
	}
	if b.Context().NavigationDebug.ShowFlow {
		t.Fatal("instance b inherited instance a's flow overlay")
	}
	if a.Context().NavigationDebug.CompositePassability == b.Context().NavigationDebug.CompositePassability {
		t.Fatal("navigation debug state is shared between Apps")
	}

	a.Context().KeyTable = &input.KeyTable{}
	a.Context().PushLocal(event.EventMetaHelpRequest, nil)
	a.Settle()
	b.Context().PushLocal(event.EventMetaHelpRequest, nil)
	b.Settle()
	gotA := a.Context().GetOverlayContent()
	gotB := b.Context().GetOverlayContent()
	if gotA == nil {
		t.Fatal("instance a help produced no content")
	}
	if gotB == nil || len(gotB.Items) == 0 {
		t.Fatal("instance b help inherited instance a's empty key table")
	}
	if len(gotA.Items) >= len(gotB.Items) {
		t.Fatalf("help item counts = (%d, %d), want instance a's empty bindings scoped", len(gotA.Items), len(gotB.Items))
	}

	a.Tick(2)
	b.Tick(1)
	_, tickA, _ := a.Context().Correlation.Stamp()
	_, tickB, _ := b.Context().Correlation.Stamp()
	if tickA == tickB {
		t.Fatalf("correlation ticks = (%d, %d), want independent values", tickA, tickB)
	}
}

// corpusDir is the multi-file corpus the parity criterion needs. The embedded one
// is a single file, so its cursor never rolls over and the divergence below cannot
// occur — which is exactly why every criterion built on it missed this.
const corpusDir = "../../wad/content"

// TestParticipantsShareTheCorpusFingerprintNotItsCursor: content glyphs are
// player-domain, so two participants who type differently consume blocks at
// different rates. The fingerprint — files, blocks, lines and source — is shared and
// stays compared; the file the cursor has reached is not, and comparing it
// desynchronised a live session with a world that agreed completely.
func TestParticipantsShareTheCorpusFingerprintNotItsCursor(t *testing.T) {
	t.Parallel()
	const seed = 0xC0FFEE
	if _, err := os.Stat(corpusDir); err != nil {
		t.Skipf("multi-file corpus %s not present", corpusDir)
	}

	base := Config{Mode: ModeHeadless, Seed: seed, Resources: resource.Options{Embedded: true}}
	base.Resources.Content = corpusDir
	base.Resources.Embedded = false

	host := base
	host.Width, host.Height = 120, 40
	a, err := NewHeadless(host)
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	defer a.Close()
	an := a.JoinAnchor()

	guest := base
	guest.Width, guest.Height = 84, 26
	guest.MapWidth, guest.MapHeight = an.Anchor.MapWidth, an.Anchor.MapHeight
	guest.CropOnResize, guest.LockMap = an.Anchor.CropOnResize, an.Anchor.SessionShared
	b, err := NewHeadless(guest)
	if err != nil {
		t.Fatalf("guest: %v", err)
	}
	defer b.Close()
	if err := b.Join(an); err != nil {
		t.Fatalf("join: %v", err)
	}
	a.adoptMapLatch(an.Anchor)

	pa, pb := network.NewLoopbackPair(1, 2)
	a.AttachTransport(pa)
	b.AttachTransport(pb)
	for _, x := range []*App{a, b} {
		tickUntilCursor(t, x)
		x.Tick(1)
	}
	mirrorCursors(t, a, b)
	assertSharedParity(t, a, b, -1)

	// One participant consumes more of the corpus than the other, which is what
	// typing does. Drawing directly makes the asymmetry immediate and exact instead
	// of waiting several hundred steps for two scripts to drift apart.
	corpusFile := func(x *App) string {
		var s string
		x.World().RunSafe(func() { s = x.World().Resources.Status.Strings.Get("content.file").Load() })
		return s
	}
	start := corpusFile(a)
	if start != corpusFile(b) {
		t.Fatalf("participants began on different corpus files: %q and %q", start, corpusFile(b))
	}
	var drawn int
	for range 400 {
		if corpusFile(a) != start {
			break
		}
		a.World().RunSafe(func() {
			if res := a.World().Resources.Content; res != nil && res.Provider != nil {
				res.Provider.NextBlock()
			}
		})
		drawn++
	}
	if corpusFile(a) == start {
		t.Fatalf("the corpus cursor never left %q after %d blocks; it may hold one file", start, drawn)
	}

	for i := range 8 {
		a.Tick(1)
		b.Tick(1)
		assertSharedParity(t, a, b, i)
	}
	if corpusFile(a) == corpusFile(b) {
		t.Fatalf("both participants report corpus file %q; the criterion proves nothing", corpusFile(a))
	}
}

// TestEmbedderSharedMutationIsRefusedInALiveSession. SetupLevel and Region carry
// ClassShared payloads: applied locally they change one instance's map bounds or FSM
// regions and no correction repairs the result. event.OnWire admits only Bus and
// Stamped, so a ClassShared event reaches no peer whoever pushes it — the authority
// is refused too, and Reset keeps its crossing because its request is ClassBus.
func TestEmbedderSharedMutationIsRefusedInALiveSession(t *testing.T) {
	t.Parallel()
	host, guest := pair(t, 0x5EEDBEEF, 0)

	mapOf := func(x *App) (w, h int) {
		x.World().RunSafe(func() {
			cfg := x.World().Resources.Config
			w, h = cfg.MapWidth, cfg.MapHeight
		})
		return w, h
	}
	pausedOf := func(x *App) (v bool) {
		x.World().RunSafe(func() {
			v = x.World().Resources.Status.Bools.Get("fsm.main.paused").Load()
		})
		return v
	}

	wantW, wantH := mapOf(host)
	if w, h := mapOf(guest); w != wantW || h != wantH {
		t.Fatalf("the pair did not start on one map: host %dx%d, guest %dx%d", wantW, wantH, w, h)
	}
	for _, x := range []struct {
		name string
		app  *App
	}{{"host", host}, {"guest", guest}} {
		if x.app.SetupLevel(60, 20, true, false) {
			t.Fatalf("%s published a level setup into a live session", x.name)
		}
		if x.app.Region(event.RegionPause, "main", "") {
			t.Fatalf("%s published an FSM region change into a live session", x.name)
		}
	}
	for range parameter.NetworkBarrierDelayTicks + 2 {
		tickAll([]*App{host, guest})
	}
	for _, x := range []struct {
		name string
		app  *App
	}{{"host", host}, {"guest", guest}} {
		if w, h := mapOf(x.app); w != wantW || h != wantH {
			t.Fatalf("%s map = %dx%d after a refused setup, want %dx%d", x.name, w, h, wantW, wantH)
		}
		if pausedOf(x.app) {
			t.Fatalf("%s paused its main region on a refused request", x.name)
		}
	}

	// The Bus request the plan compared them against does still travel, from the
	// authority and from nobody else.
	if guest.Reset(false) {
		t.Fatal("a guest reset a live session")
	}
	if !host.Reset(false) {
		t.Fatal("the authority was refused its own reset")
	}
}
