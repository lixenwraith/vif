package app

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// runningHost builds one participant, journals it into a retained log, and drives it
// for the given number of steps so a joiner has something to catch up to.
func runningHost(t *testing.T, seed uint64, steps int) (*App, *Capture) {
	t.Helper()

	a, err := NewHeadless(Config{
		Seed: seed, Width: 120, Height: 40, ForceDefault: true,
		RetainSessionLog: true,
	})
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	t.Cleanup(a.Close)
	log := a.sessionLog

	tickUntilCursor(t, a)
	a.SetupLevel(100, 30, true, false)
	a.Tick(1)

	opt := parityScript(seed, steps)
	opt.Regions, opt.MapSetups = false, false
	opt.DisableTicks, opt.DisableCommands, opt.DisableOverlays = true, true, true
	driver := NewScriptDriver(a, opt)
	for i := range steps {
		if !driver.Step() {
			t.Fatalf("step %d quit the host", i)
		}
		a.Tick(1)
	}
	return a, log
}

// TestLateJoinerReplaysTheSessionToTheHostPosition is the mid-run join criterion.
// Nothing transports world state, so the joiner reproduces the session rather than
// receiving it: same anchor, same record stream, replayed at full speed. Reaching
// byte-identical shared state is what says the reproduction was exact.
func TestLateJoinerReplaysTheSessionToTheHostPosition(t *testing.T) {
	const seed = 0x5EEDBEEF
	steps := soakScale(60, 150, 300)

	host, log := runningHost(t, seed, steps)
	records, at := host.SessionLog()
	if len(records) == 0 {
		t.Fatal("the host retained no records to catch up on")
	}
	if at.Tick == 0 {
		t.Fatal("the host is still at tick zero")
	}
	if got := len(log.Records()); got != len(records) {
		t.Fatalf("SessionLog returned %d records, the sink holds %d", len(records), got)
	}

	// A different terminal, as a joiner's will be: the map is latched shared state and
	// the viewport is not, so catching up must reproduce the one and not the other.
	anchor := host.JoinAnchor()
	joinCfg, err := ConfigForJoin(Config{Mode: ModeHeadless, Width: 90, Height: 30}, network.SessionOffer{Anchor: anchor})
	if err != nil {
		t.Fatalf("join config: %v", err)
	}
	guest, err := NewHeadless(joinCfg)
	if err != nil {
		t.Fatalf("guest: %v", err)
	}
	t.Cleanup(guest.Close)

	if err := guest.CatchUp(anchor, records); err != nil {
		t.Fatalf("catch up: %v", err)
	}
	if got := guest.Position(); got.Tick != at.Tick || got.Run != at.Run {
		t.Fatalf("guest position = run %d tick %d, want run %d tick %d",
			got.Run, got.Tick, at.Run, at.Tick)
	}
	assertSharedParity(t, host, guest, int(at.Tick))
}

// TestLateJoinerTakesTheRosterAndStaysInLockstep continues past the catch-up: the
// arrival crosses, so both instances create the new cursor at one agreed tick, and
// the joiner then produces crossings of its own like any other participant.
func TestLateJoinerTakesTheRosterAndStaysInLockstep(t *testing.T) {
	const seed = 0x5EEDBEEF
	steps := 200
	live := 120
	if testing.Short() {
		steps, live = 40, 30
	}

	host, _ := runningHost(t, seed, steps)
	records, at := host.SessionLog()
	anchor := host.JoinAnchor()

	joinCfg, err := ConfigForJoin(Config{Mode: ModeHeadless, Width: 120, Height: 40}, network.SessionOffer{Anchor: anchor})
	if err != nil {
		t.Fatalf("join config: %v", err)
	}
	guest, err := NewHeadless(joinCfg)
	if err != nil {
		t.Fatalf("guest: %v", err)
	}
	t.Cleanup(guest.Close)
	if err := guest.CatchUp(anchor, records); err != nil {
		t.Fatalf("catch up: %v", err)
	}

	mesh := network.NewMesh()
	mesh.Link(1, 2)
	host.AttachTransport(mesh.Node(1))
	guest.AttachTransport(mesh.Node(2))
	host.activateNetworkSession()
	guest.activateNetworkSession()

	apps := []*App{host, guest}
	joined := network.SessionParticipant{ID: 2, Slot: 1}
	if err := host.AdmitParticipant(joined); err != nil {
		t.Fatalf("admit: %v", err)
	}
	host.Settle()

	// The arrival is an artifact, so it lands on both instances at its apply tick and
	// on neither before it.
	for range parameter.NetworkBarrierDelayTicks + 2 {
		tickAll(apps)
	}

	var guestCursor core.Entity
	for i, a := range apps {
		var slot core.Entity
		var count int
		a.World().RunSafe(func() {
			slot = a.World().Resources.Player.Slot(1)
			count = a.World().Resources.Player.Count()
		})
		if slot == 0 || count != 2 {
			t.Fatalf("participant %d roster = slot1 %d count %d, want a second cursor", i+1, slot, count)
		}
		if guestCursor == 0 {
			guestCursor = slot
		} else if slot != guestCursor {
			t.Fatalf("participant %d holds %d in slot 1, want the shared entity %d", i+1, slot, guestCursor)
		}
	}
	if !ownsCursor(guest, guestCursor) {
		t.Fatal("the joiner does not simulate the cursor it was admitted as")
	}
	if ownsCursor(host, guestCursor) {
		t.Fatal("the host simulates the joiner's cursor")
	}
	assertSharedParity(t, host, guest, int(at.Tick))

	// Both now drive their own cursor; shared state must stay identical throughout.
	optHost := parityScript(seed, live)
	optHost.Regions, optHost.MapSetups = false, false
	optHost.DisableTicks, optHost.DisableCommands, optHost.DisableOverlays = true, true, true
	optGuest := optHost
	optGuest.Seed ^= 0x9E3779B97F4A7C15
	dh, dg := NewScriptDriver(host, optHost), NewScriptDriver(guest, optGuest)

	start := cursorPosition(guest, guestCursor)
	for i := range live {
		if !dh.Step() || !dg.Step() {
			t.Fatalf("step %d quit a participant", i)
		}
		tickAll(apps)
		assertSharedParity(t, host, guest, i)
	}
	for i := range parameter.NetworkBarrierDelayTicks + 1 {
		tickAll(apps)
		assertSharedParity(t, host, guest, live+i)
	}
	if cursorPosition(guest, guestCursor) == start {
		t.Fatal("the joiner never moved, so nothing of its own crossed")
	}
	if sent := statOf(guest, "network.crossings_sent"); sent == 0 {
		t.Fatal("the joiner sent no crossing")
	}
}

// TestCatchUpRefusesAnAdvancedInstance keeps the mechanism honest: replay reproduces
// a run from its start, so an instance that has already ticked cannot be caught up.
func TestCatchUpRefusesAnAdvancedInstance(t *testing.T) {
	host, _ := runningHost(t, 0x5EEDBEEF, 20)
	records, _ := host.SessionLog()
	anchor := host.JoinAnchor()

	guest := mustHeadless(t, 0x5EEDBEEF, 120, 40)
	t.Cleanup(guest.Close)
	guest.Tick(1)
	if err := guest.CatchUp(anchor, records); err == nil {
		t.Fatal("CatchUp accepted an instance that had already run")
	}
}
