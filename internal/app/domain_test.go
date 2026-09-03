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
	for _, dir := range []string{"main", "td", "blank"} {
		d := filepath.Join(root, "config", dir)
		if _, err := os.Stat(filepath.Join(d, "game.toml")); err != nil {
			continue
		}
		trees["config/"+dir] = func() (map[string]any, error) {
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

// TestFSMTriggersAreReplicated is D-20 made mechanical.
//
// Every FSM region is shared state (§4): each instance re-derives the same region
// in the same state at the same tick, and fsm.<region> is compared across the
// session. A region can only stay in agreement if every event that moves it is an
// event every instance holds. A ClassLocal trigger is not: by definition it never
// replicates, so the region advances on the one instance whose participant
// produced it and nowhere else, and the two never converge again — nothing
// re-derives a missing local event.
//
// This is not hypothetical. MonitorActive transitioned on EventHeatBurst, which
// HeatSystem pushes with PushLocal for the cursor that overheated. In the
// 2026-08-31 session that fired at tick 1903; the shared surface reported
// reg|stat|fsm.monitor divergent from tick 1914 and the session was marked
// DIVERGED at 1934. The sweep it wanted is a per-instance effect (D-6) and
// HeatSystem emits it directly now.
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
			// config/blank declares no transitions at all, so a per-tree floor
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

// TestBusPayloadsNameOnlySharedEntities asserts D-4 over a soak: a record that
// replicates names only shared entities. The transported set comes from the class
// table, so this runs against the declared set rather than a hand-list — a Stamped
// type resolves through the domain its producer stamped, which for a combat hit is
// the target's own domain. A record that does not replicate constrains nothing and
// is skipped whole; its player entities are this instance's business.
//
// The tap runs on the caller's goroutine — a driven App has no scheduler — so no
// synchronization is needed.
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
// ambient domain. The owner-authored grants, the D-6 effects, internal/mode and
// every artifact an FSM region emits now stamp; app, engine and the shared species
// systems still push these unstamped.
// The set must only shrink: an entry that stops appearing fails, and a type not
// listed here fails on first sight.
// Not a transport gate — the class keeps a Local type off the wire whatever its
// tag — but a per-instance effect journaled as shared is a record two instances
// legitimately differ on while claiming they should not.
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
// player. The class already keeps it out of the transported set, so this is about
// the record being honest: a per-instance effect journaled as shared is a record
// two instances will legitimately differ on while claiming they should not.
//
// core.DomainShared is the zero value and the ambient domain defaults to it, so
// every type reported here is a push site that never stamped.
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

// The quasar is fused from one cursor's drains, and its two standing effects — the
// grayout and the drain pause — belong to that cursor.
//
// The region that raises them does not: it is shared, so every instance runs the
// same machine, enters QuasarFuse and executes the same on_enter actions. Before
// the scope payload that made one participant's quasar darken every participant's
// screen and stop every participant's drains. The unit tests in internal/system pin
// the two handlers; this one drives the whole path — a shared drain defeat, the
// MainEscalate capture, the spawned region, the emitted effects, the region's end —
// across two linked instances, which is the only place the fan-out was visible.

// TestAQuasarsEffectsReachOnlyTheCursorItWasFusedFrom is the reported defect.
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

	// The shared half is unchanged: one logical fusion producing one spawn request
	// on each instance, not one per participant.
	//
	// Full snapshot parity is not the assertion here. Both machines run the region
	// and both leave it, but they enter it a barrier apart, so the states they hold
	// afterwards differ by that lead in elapsed time — a property of the delivery
	// lead rather than of the scope this test is about.
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
const corpusDir = "../../data"

// TestParticipantsShareTheCorpusFingerprintNotItsCursor is the criterion for a
// leak the harness could not see: content glyphs are player-domain, so two
// participants who type differently consume blocks at different rates, and the
// corpus cursor is a position in a shared file list rather than shared state.
//
// The fingerprint — how many files, blocks and lines the corpus holds, and where it
// came from — is shared and stays compared. The file the cursor has reached is not,
// and comparing it desynchronised a live session the moment the two participants
// rolled onto different files: a permanent DESYNC with a world that agreed
// completely.
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
