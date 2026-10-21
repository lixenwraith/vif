package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
)

// plantSentinel places a bare shared entity near the far map corner, where a
// cropping resize would mark it for death
func plantSentinel(t *testing.T, a *App) (e core.Entity, mapW, mapH int) {
	t.Helper()
	w := a.World()
	cfg := w.Resources.Config
	w.RunSafe(func() {
		mapW, mapH = cfg.MapWidth, cfg.MapHeight
		e = w.CreateEntity(core.DomainShared)
		w.Positions.SetPosition(e, component.PositionComponent{X: mapW - 2, Y: mapH - 2})
	})
	return e, mapW, mapH
}

// sentinelState reports whether the entity still holds a position and whether the
// crop path marked it for death
func sentinelState(a *App, e core.Entity) (alive, doomed bool) {
	a.World().RunSafe(func() {
		alive = a.World().Positions.HasPosition(e)
		doomed = a.World().Components.Death.HasEntity(e)
	})
	return alive, doomed
}

// TestMapSizeFollowsTheTerminalOnlyUntilASecondCursor asserts D-14 with its own
// negative control: a single participant's resize crops the map and destroys an
// out-of-bounds entity, and the identical resize under a second cursor rewrites
// nothing shared and crops nobody. Without the control the suppression could be
// asserting a crop that would not have fired.
func TestMapSizeFollowsTheTerminalOnlyUntilASecondCursor(t *testing.T) {
	t.Parallel()

	solo := mustHeadless(t, 0x14AC, 200, 60)
	defer solo.Close()
	tickUntilCursor(t, solo)
	sentinel, mapW, _ := plantSentinel(t, solo)
	solo.Resize(60, 24)
	solo.Tick(2)

	cfg := solo.World().Resources.Config
	var gotW, viewW int
	solo.World().RunSafe(func() { gotW, viewW = cfg.MapWidth, cfg.ViewportWidth })
	if gotW == mapW || gotW != viewW {
		t.Fatalf("map %d after crop, want the new viewport %d (was %d)", gotW, viewW, mapW)
	}
	if alive, _ := sentinelState(solo, sentinel); alive {
		t.Fatal("crop left an out-of-bounds entity alive; the locked case would prove nothing")
	}
	if solo.World().Resources.Status.Bools.Get("context.map_locked").Load() {
		t.Fatal("context.map_locked set while the map still follows the terminal")
	}

	shared := mustHeadless(t, 0x14AB, 200, 60)
	defer shared.Close()
	tickUntilCursor(t, shared)
	spawnCursor(t, shared)
	shared.Tick(1)
	sentinel, mapW, mapH := plantSentinel(t, shared)
	shared.Resize(60, 24)
	shared.Tick(2)

	cfg = shared.World().Resources.Config
	var gotH int
	shared.World().RunSafe(func() { gotW, gotH = cfg.MapWidth, cfg.MapHeight })
	if gotW != mapW || gotH != mapH {
		t.Fatalf("map resized under a second cursor: %dx%d, want %dx%d", gotW, gotH, mapW, mapH)
	}
	if alive, doomed := sentinelState(shared, sentinel); !alive || doomed {
		t.Fatalf("sentinel outside the new viewport was cropped: alive=%v doomed=%v", alive, doomed)
	}
	if !shared.World().Resources.Status.Bools.Get("context.map_locked").Load() {
		t.Fatal("context.map_locked not published")
	}
	if sw, sh := engine.ScreenSize(cfg); sw != 60 || sh != 24 {
		t.Fatalf("viewport did not follow the terminal: ScreenSize = %dx%d", sw, sh)
	}
}

// TestJoinerOnAnotherTerminalSharesTheMapFromTickZero is the regression for the
// divergence two windows of different size produced from the first tick: the FSM boot
// script spawns cursor slot zero at the centre of the map inside New, so a joiner
// adopting the bounds afterwards held that shared cursor on its own centre. It runs
// the production sequence — bounds before the FSM boots, then an install.
func TestJoinerOnAnotherTerminalSharesTheMapFromTickZero(t *testing.T) {
	t.Parallel()
	host := mustHeadless(t, 0x14AD, 160, 48)
	defer host.Close()
	an := host.JoinAnchor()
	guest := mustJoiner(t, 0x14AD, 84, 26, an)
	defer guest.Close()

	offer := sessionOfferFor(an, 2)
	if err := host.HostSession(offer); err != nil {
		t.Fatalf("host session: %v", err)
	}
	host.Tick(1)
	if err := guest.JoinSessionAt(offer, mustRoundTrip(t, host)); err != nil {
		t.Fatalf("join session: %v", err)
	}
	assertSharedParity(t, host, guest, 0)

	var hostView, guestView int
	host.World().RunSafe(func() { hostView = host.World().Resources.Config.ViewportWidth })
	guest.World().RunSafe(func() { guestView = guest.World().Resources.Config.ViewportWidth })
	if hostView == guestView {
		t.Fatalf("both participants ran a %d-column viewport; the criterion proves nothing", hostView)
	}
}

// TestSessionRunNeverCropsItsMap covers the window the peer count left open: a host
// waiting in its lobby has no peer and one cursor, so the old guard let a resize
// crop the very bounds the anchor it is handing out names.
func TestSessionRunNeverCropsItsMap(t *testing.T) {
	t.Parallel()
	a, err := NewHeadless(Config{Seed: 0x14AE, Width: 200, Height: 60, Resources: resource.Options{Embedded: true}, LockMap: true})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()
	tickUntilCursor(t, a)

	sentinel, mapW, mapH := plantSentinel(t, a)
	before := a.JoinAnchor()

	a.Resize(60, 24)
	a.Tick(2)

	cfg := a.World().Resources.Config
	var gotW, gotH int
	a.World().RunSafe(func() { gotW, gotH = cfg.MapWidth, cfg.MapHeight })
	if gotW != mapW || gotH != mapH {
		t.Fatalf("hosting run cropped to %dx%d, want the offered %dx%d", gotW, gotH, mapW, mapH)
	}
	if alive, doomed := sentinelState(a, sentinel); !alive || doomed {
		t.Fatalf("hosting run cropped away an entity: alive=%v doomed=%v", alive, doomed)
	}
	after := a.JoinAnchor()
	if after.Anchor.MapWidth != before.Anchor.MapWidth ||
		after.Anchor.MapHeight != before.Anchor.MapHeight {
		t.Fatalf("the anchor a joiner adopts moved under a resize: %dx%d then %dx%d",
			before.Anchor.MapWidth, before.Anchor.MapHeight,
			after.Anchor.MapWidth, after.Anchor.MapHeight)
	}
	if !after.Anchor.SessionShared {
		t.Fatal("a hosting run's anchor does not carry the D-14 latch a reproduction adopts")
	}
}

// TestLocalViewChangesLeaveTheFlowFieldPhaseAlone covers D-17 at its two producers.
// The flow-field cache is throttled, so its phase is shared state and every producer
// of a dirty mark has to be shared. Two purely local view changes were announcing
// EventCursorMoved — a resize reconciling cursors the locked map had not moved, and
// the rebind of this participant's own slot, which fires at every session start.
func TestLocalViewChangesLeaveTheFlowFieldPhaseAlone(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 0x14AF, 200, 60)
	defer a.Close()
	tickUntilCursor(t, a)
	spawnCursor(t, a) // a second cursor locks the map (D-14)
	a.Tick(2)

	moves := 0
	a.SetDispatchTap(func(ev event.GameEvent) {
		if ev.Type == event.EventCursorMoved {
			moves++
		}
	})
	a.Resize(90, 30)
	a.Tick(2)
	if moves != 0 {
		t.Fatalf("a resize under a locked map announced %d cursor moves", moves)
	}

	// A local rebind is the same rule in a second place, and the one that fires at
	// session start: every participant but slot zero binds its own cursor, so the
	// announcement offset the two instances' flow-field phases from tick one.
	spawnCursor(t, a)
	a.Tick(1)
	moves = 0
	a.Context().PushEventOrigin(event.EventCursorSetLocalRequest,
		&event.CursorSetLocalPayload{Slot: 1}, event.OriginDebug)
	a.Settle()
	a.Tick(1)
	if moves != 0 {
		t.Fatalf("a local rebind announced %d cursor moves", moves)
	}
	var bound uint8
	a.World().RunSafe(func() { bound = a.World().Resources.Player.LocalSlot() })
	if bound != 1 {
		t.Fatalf("rebind left the local slot at %d, want 1; the guard proves nothing", bound)
	}

	// The negative control: the same resize with one cursor crops the map, which
	// really does move cursors, and there the announcement is the mechanism.
	b := mustHeadless(t, 0x14AF, 200, 60)
	defer b.Close()
	tickUntilCursor(t, b)
	b.Tick(2)
	moves = 0
	b.SetDispatchTap(func(ev event.GameEvent) {
		if ev.Type == event.EventCursorMoved {
			moves++
		}
	})
	b.Resize(90, 30)
	b.Tick(2)
	if moves == 0 {
		t.Fatal("a cropping resize announced no cursor move; the guard above proves nothing")
	}
}

// TestAMapNeverCropsBelowItsScenarioFloor: a scenario's [map] floor holds at boot,
// under a smaller terminal, across a reset and through a level setup the cell cap
// trims, while a larger terminal still crops the map to itself.
func TestAMapNeverCropsBelowItsScenarioFloor(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	entry := filepath.Join(writeScenario(t, root, "floor", "A floored map."), "scenario.toml")
	body, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, append(body, "[map]\nmin_width = 50\nmin_height = 100\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := NewHeadless(Config{Seed: 0xF100, Width: 40, Height: 15, Resources: resource.Options{Dir: root, Scenario: "floor"}})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()
	cfg := a.World().Resources.Config
	expect := func(when string, w, h int) {
		t.Helper()
		var gotW, gotH int
		a.World().RunSafe(func() { gotW, gotH = cfg.MapWidth, cfg.MapHeight })
		if gotW != w || gotH != h {
			t.Fatalf("%s: map %dx%d, want %dx%d", when, gotW, gotH, w, h)
		}
	}
	expect("at boot", 50, 100)
	a.Resize(200, 110)
	expect("on a larger terminal", 200-parameter.LeftMargin, 110-parameter.TopMargin-parameter.BottomMargin)
	a.Resize(40, 15)
	expect("on a smaller terminal", 50, 100)
	a.Reset(false)
	a.Tick(1)
	expect("after a reset", 50, 100)
	a.SetupLevel(1500, 30, false, true)
	a.Tick(1)
	expect("past the cell cap", parameter.MaxMapCells/100, 100)
}

// writeOffMapScenario lays down a scenario whose map places no cursor and that takes
// keys 1 and 2 for its whole run, reconciled so a joiner takes them too.
func writeOffMapScenario(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "scenario", "keys")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "description = \"Keys without cursors.\"\n[map]\ncursors = false\n" +
		"[regions.player]\ninitial = \"PlayerBoot\"\nbackground = true\n" +
		"[regions.keys]\ninitial = \"Keys\"\n" +
		"[states.PlayerBoot]\nparent = \"Root\"\non_enter = [{ action = \"EmitEvent\", " +
		"event = \"EventCursorSpawnRequest\", payload = { center = true, heat = 10, energy = 100 } }]\n" +
		"[states.Keys]\nparent = \"Root\"\non_enter = [{ action = \"EmitEvent\", " +
		"event = \"EventKeyCaptureRequest\", payload = { keys = \"12\" }, reconcile = true }]\n"
	if err := os.WriteFile(filepath.Join(dir, "scenario.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// assertOffMap fails unless slot holds a cursor with nothing on the map: no cell and
// none of what a cell carries.
func assertOffMap(t *testing.T, a *App, slot uint8) {
	t.Helper()
	w := a.World()
	var e core.Entity
	var parts []string
	w.RunSafe(func() {
		e = w.Resources.Player.Slot(slot)
		if _, ok := w.Positions.GetPosition(e); ok {
			parts = append(parts, "position")
		}
		for name, held := range map[string]bool{"energy": w.Components.Energy.HasEntity(e),
			"heat": w.Components.Heat.HasEntity(e), "combat": w.Components.Combat.HasEntity(e),
			"view": w.Components.CursorView.HasEntity(e), "ping": w.Components.Ping.HasEntity(e)} {
			if held {
				parts = append(parts, name)
			}
		}
	})
	if e == 0 {
		t.Fatalf("slot %d holds no cursor", slot)
	}
	if len(parts) > 0 {
		t.Fatalf("slot %d's cursor holds %v on a map that places no cursor", slot, parts)
	}
}

// TestAMapWithoutCursorsRostersItsPlayerOffIt: [map] cursors = false rosters the boot
// spawn's slot with nothing on the map, through a reset and a level setup that
// clears the map; a motion moves nothing, and a captured key still presses from
// that slot.
func TestAMapWithoutCursorsRostersItsPlayerOffIt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeOffMapScenario(t, root)
	a, err := NewHeadless(Config{Seed: 0xF101, Width: 80, Height: 24, Resources: resource.Options{Dir: root, Scenario: "keys"}})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()
	tickUntilCursor(t, a)
	assertOffMap(t, a, 0)

	var moves int
	var presses []event.InputPayload
	a.SetDispatchTap(func(ev event.GameEvent) {
		switch p := ev.Payload.(type) {
		case *event.CursorMoveRequestPayload:
			moves++
		case *event.InputPayload:
			if ev.Type == event.EventInputPressed {
				presses = append(presses, *p)
			}
		}
	})
	key := func(k rune) *input.Intent {
		return a.inputMachine.Process(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: k})
	}
	a.Inject(key('l'), &input.Intent{Type: input.IntentMouseLeftDown, X: 3, Y: 3}, &input.Intent{Type: input.IntentMouseLeftUp})
	a.Tick(2)
	if moves != 0 {
		t.Fatalf("%d cursor moves on a map that places no cursor", moves)
	}
	a.Inject(key('1'))
	a.Tick(1)
	if len(presses) != 1 || presses[0].Key != "1" || presses[0].Slot != 0 {
		t.Fatalf("pressing 1 crossed %+v, want one press of 1 from slot 0", presses)
	}

	a.Reset(false)
	a.Tick(2)
	assertOffMap(t, a, 0)
	a.SetupLevel(60, 20, true, false)
	a.Tick(1)
	assertOffMap(t, a, 0)
	var count int
	a.World().RunSafe(func() { count = a.World().Resources.Player.Count() })
	if count != 1 {
		t.Fatalf("the roster holds %d cursors, want the one player", count)
	}
}

// TestAJoinerTakesASlotOnAMapWithoutCursors: on a map that places no cursor a joiner
// is rostered off it on both instances alike and follows its own slot, and its press
// reaches the host from that slot.
func TestAJoinerTakesASlotOnAMapWithoutCursors(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	const seed = 0xF102
	root := t.TempDir()
	writeOffMapScenario(t, root)
	host, err := NewHeadless(Config{Seed: seed, Width: 120, Height: 40, Resources: resource.Options{Dir: root, Scenario: "keys"}})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer host.Close()
	tickUntilCursor(t, host)
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("begin hosting: %v", err)
	}
	stop := tickInBackground(host)
	guest, _ := mustSocketJoiner(t, host.HostAddr(), seed, 120, 40, func(c *Config) { c.Resources.Dir = root })
	stop()

	settleCorrections(t, host, guest)
	alignTicks(t, host, guest)
	waitForRosterPair(t, host, guest)
	alignTicks(t, host, guest)
	assertControl(t, host, 1, component.ControlRemote)
	assertControl(t, guest, 1, component.ControlLocal)
	if got := guest.localSlot(); got != 1 {
		t.Fatalf("guest follows slot %d, want its own slot 1", got)
	}
	for _, a := range []*App{host, guest} {
		assertOffMap(t, a, 0)
		assertOffMap(t, a, 1)
	}
	assertSharedParity(t, host, guest, 0)

	var from []uint8
	host.SetDispatchTap(func(ev event.GameEvent) {
		if p, ok := ev.Payload.(*event.InputPayload); ok && ev.Type == event.EventInputPressed {
			from = append(from, p.Slot)
		}
	})
	guest.Inject(guest.inputMachine.Process(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: '2'}))
	for deadline := time.Now().Add(socketWait); len(from) == 0 && time.Now().Before(deadline); { // [wall] a link bound
		guest.Tick(1)
		host.Tick(1)
		time.Sleep(joinTestTickInterval / 5)
	}
	if !slices.Equal(from, []uint8{1}) {
		t.Fatalf("the guest's press reached the host from slots %v, want its slot 1", from)
	}
}
