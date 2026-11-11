package system

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/network"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// syncFor builds one owner-authored state message for a cursor (D-13).
func syncFor(e core.Entity, slot uint8, seq uint64, energy int64) *event.CursorStatePayload {
	return &event.CursorStatePayload{Entity: e, Slot: slot, Seq: seq, Energy: energy, Heat: 11}
}

// TestCursorStateSyncWritesOnlyACoherentRemoteCursor pins the admission rules on
// the receiving half of D-13. The payload names an entity and a slot: the entity
// selects the cells written, the slot keys the sequence that decides whether to
// write them. A sync is applied only when both agree, addresses a cursor this
// instance does not simulate, and carries a sequence the slot has not seen.
func TestCursorStateSyncWritesOnlyACoherentRemoteCursor(t *testing.T) {
	w, local, _ := testCursorWorld(t)
	remote := spawnRemoteCursor(t, w, 2, 25, 5, 7)
	net := ready(NewNetworkSystem(w)).(*NetworkSystem)

	energyOf := func(e core.Entity) int64 {
		c, _ := w.Components.Energy.GetComponent(e)
		return c.Current
	}

	// A coherent sync is the non-vacuous case: without it every rejection below
	// would pass on a path that never writes anything.
	net.writeCursorState(syncFor(remote, 2, 1, 40))
	if got := energyOf(remote); got != 40 {
		t.Fatalf("remote energy after sync = %d, want 40", got)
	}

	// Slot disagreeing with the entity: applying it would age slot 2's state under
	// slot 1's sequence, so the write is refused rather than reconciled.
	net.writeCursorState(syncFor(remote, 1, 9, 55))
	if got := energyOf(remote); got != 40 {
		t.Fatalf("remote energy after mismatched slot = %d, want 40", got)
	}

	// Replayed or reordered: the newer value already landed.
	net.writeCursorState(syncFor(remote, 2, 1, 60))
	if got := energyOf(remote); got != 40 {
		t.Fatalf("remote energy after stale sequence = %d, want 40", got)
	}

	// A cursor this instance simulates has exactly one authority, and it is local.
	before := energyOf(local)
	net.writeCursorState(syncFor(local, 0, 1, 77))
	if got := energyOf(local); got != before {
		t.Fatalf("local energy after peer sync = %d, want %d", got, before)
	}
}

// TestASyncHoldingAValueItsOwnerCannotIsDroppedWhole: a peer's sync is written only
// while each value lies where its owner's systems keep it. One just past a bound is
// dropped whole and counted, its sequence left to the next; one at every bound lands.
func TestASyncHoldingAValueItsOwnerCannotIsDroppedWhole(t *testing.T) {
	edge := func(w *engine.World, remote core.Entity) *event.CursorStatePayload {
		cfg := w.Resources.Config
		span := math.Hypot(float64(cfg.MapWidth), cfg.CellAspect.Circular(float64(cfg.MapHeight)))
		p := &event.CursorStatePayload{Entity: remote, Slot: 2, Seq: 1,
			Energy: -parameter.GodEnergyAmount, Heat: parameter.HeatMax, Overheat: parameter.HeatMaxOverheat,
			ShieldActive: true, ShieldRadius: span, ShieldColor: color.RGB{R: 10, G: 20, B: 30},
			BoostActive: true, BoostRemaining: int64(time.Hour), BoostTotal: int64(time.Hour),
			MainFireCooldown: int64(parameter.WeaponCooldownMain), HitPoints: parameter.CombatInitialHPCursor,
			DamageImmunity: int64(parameter.CombatDamageImmunityDuration),
			WeaponCharges:  make([]int, component.WeaponCount), WeaponCooldown: make([]int64, component.WeaponCount)}
		for wt, spec := range component.WeaponSpecs {
			p.WeaponCharges[wt], p.WeaponCooldown[wt] = spec.MaxCharges, int64(max(spec.Cooldown, spec.MaxedCooldown))
		}
		p.WeaponCharges[component.WeaponGun] = parameter.GunPathMax
		for atk := range p.DamageImmunitySpent {
			p.DamageImmunitySpent[atk] = unownedAttacker<<1 - 1
		}
		return p
	}
	rows := []struct {
		name string
		past func(p *event.CursorStatePayload) // nil leaves every value at its bound
	}{
		{"every value at its bound", nil},
		{"heat past its meter", func(p *event.CursorStatePayload) { p.Heat++ }},
		{"overheat past its meter", func(p *event.CursorStatePayload) { p.Overheat++ }},
		{"shield radius past the map's diagonal", func(p *event.CursorStatePayload) {
			p.ShieldRadius = math.Nextafter(p.ShieldRadius, math.Inf(1))
		}},
		{"shield radius NaN", func(p *event.CursorStatePayload) { p.ShieldRadius = math.NaN() }},
		{"shield radius below zero", func(p *event.CursorStatePayload) { p.ShieldRadius = math.Nextafter(0, -1) }},
		{"boost remaining below zero", func(p *event.CursorStatePayload) { p.BoostRemaining = -1 }},
		{"boost total below zero", func(p *event.CursorStatePayload) { p.BoostTotal = -1 }},
		{"charges past the kind's cap", func(p *event.CursorStatePayload) { p.WeaponCharges[component.WeaponRod]++ }},
		{"gun charges past the longest path", func(p *event.CursorStatePayload) { p.WeaponCharges[component.WeaponGun]++ }},
		{"cooldown past the kind's longest", func(p *event.CursorStatePayload) { p.WeaponCooldown[component.WeaponLauncher]++ }},
		{"main fire cooldown past its interval", func(p *event.CursorStatePayload) { p.MainFireCooldown++ }},
		{"hit points past the cursor's", func(p *event.CursorStatePayload) { p.HitPoints++ }},
		{"damage immunity past its window", func(p *event.CursorStatePayload) { p.DamageImmunity++ }},
		{"damage immunity below zero", func(p *event.CursorStatePayload) { p.DamageImmunity = -1 }},
		{"immunity spent by no attacker", func(p *event.CursorStatePayload) {
			p.DamageImmunitySpent[component.CombatAttackSelfDestruct]++
		}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			w, _, _ := testCursorWorld(t)
			remote := spawnRemoteCursor(t, w, 2, 25, 5, 7)
			net := ready(NewNetworkSystem(w)).(*NetworkSystem)
			p := edge(w, remote)
			if row.past == nil {
				if !net.writeCursorState(p) {
					t.Fatal("a sync at every bound was dropped")
				}
				if c, _ := w.Components.Weapon.GetComponent(remote); c.Charges[component.WeaponGun] != parameter.GunPathMax {
					t.Fatalf("gun charges after a sync at every bound = %d, want %d", c.Charges[component.WeaponGun], parameter.GunPathMax)
				}
				if c, _ := w.Components.Shield.GetComponent(remote); c.Radius != p.ShieldRadius || c.Color != p.ShieldColor {
					t.Fatalf("shield after a sync at every bound = %g %+v, want %g %+v", c.Radius, c.Color, p.ShieldRadius, p.ShieldColor)
				}
				return
			}
			row.past(p)
			dropped := net.statDrop.Load()
			if net.writeCursorState(p) || net.statDrop.Load() != dropped+1 {
				t.Fatal("the sync was applied, or dropped without being counted once")
			}
			if c, _ := w.Components.Energy.GetComponent(remote); c.Current == p.Energy {
				t.Fatal("the dropped sync wrote its energy")
			}
			if !net.writeCursorState(edge(w, remote)) {
				t.Fatal("the dropped sync spent its sequence: the next one at it was refused")
			}
		})
	}
}

// TestTheTimerSplashFollowsTheInstalledGoldSequence: the countdown is a
// player-domain effect spawnGold raises, so a receiver corrected past the spawn
// shows none, and one whose sequence an install replaced keeps the old one on an
// id the install may hand to another composite. The carrier owns both (D-6, D-19).
func TestTheTimerSplashFollowsTheInstalledGoldSequence(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	gold := ready(NewGoldSystem(w)).(*GoldSystem)

	// The store pass installs each header with its deadline before LoadShared
	header := w.CreateEntity(core.DomainShared)
	armTimer(w, header, component.TimerDeadline, parameter.GoldDuration)
	data, err := json.Marshal(goldSnapshot{
		Active: true, SpawnEnabled: true, HeaderEntity: header,
	})
	if err != nil {
		t.Fatalf("marshal a captured sequence: %v", err)
	}
	if err := gold.LoadShared(data); err != nil {
		t.Fatalf("LoadShared: %v", err)
	}
	w.Resources.Event.Queue.Consume()

	anchors := func() []core.Entity {
		var out []core.Entity
		for _, ev := range w.Resources.Event.Queue.Consume() {
			if ev.Type != event.EventSplashTimerRequest {
				continue
			}
			if p, ok := ev.Payload.(*event.SplashTimerRequestPayload); ok {
				out = append(out, p.AnchorEntity)
			}
		}
		return out
	}

	gold.Update()
	if got := anchors(); len(got) != 1 || got[0] != header {
		t.Fatalf("timer requests after an install = %v, want one anchored to %d",
			got, uint64(header))
	}

	// The splash outlives the tick that raised it; a second one would replace it
	// every cadence and reset the placement search.
	gold.Update()
	if got := anchors(); len(got) != 0 {
		t.Fatalf("timer requests on the next tick = %v, want none", got)
	}

	next := w.CreateEntity(core.DomainShared)
	armTimer(w, next, component.TimerDeadline, parameter.GoldDuration)
	data, err = json.Marshal(goldSnapshot{
		Active: true, SpawnEnabled: true, HeaderEntity: next,
	})
	if err != nil {
		t.Fatalf("marshal a second sequence: %v", err)
	}
	if err := gold.LoadShared(data); err != nil {
		t.Fatalf("LoadShared: %v", err)
	}
	var cancelled []core.Entity
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if p, ok := ev.Payload.(*event.SplashTimerCancelPayload); ok {
			cancelled = append(cancelled, p.AnchorEntity)
		}
	}
	if len(cancelled) != 1 || cancelled[0] != header {
		t.Fatalf("timer cancels after a replacing install = %v, want the replaced %d",
			cancelled, uint64(header))
	}
}

// TestDepartureReleasesTheSlotSyncSequence covers slot reuse. Sequences are
// per-sender and restart at one, so a slot still holding its predecessor's high
// water mark would silently discard every sync from the participant that replaces it.
func TestDepartureReleasesTheSlotSyncSequence(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	remote := spawnRemoteCursor(t, w, 2, 25, 5, 7)
	net := ready(NewNetworkSystem(w)).(*NetworkSystem)

	net.writeCursorState(syncFor(remote, 2, 5, 40))
	if c, _ := w.Components.Energy.GetComponent(remote); c.Current != 40 {
		t.Fatalf("remote energy after sync = %d, want 40", c.Current)
	}

	net.removeParticipant(&event.ParticipantDepartedPayload{Participant: 7, Slot: 2})
	if net.lastSync[2] != 0 {
		t.Fatalf("slot 2 sync sequence = %d after departure, want 0", net.lastSync[2])
	}

	// The successor's first message starts over at one and must still be admitted.
	net.writeCursorState(syncFor(remote, 2, 1, 63))
	if c, _ := w.Components.Energy.GetComponent(remote); c.Current != 63 {
		t.Fatalf("remote energy after slot reuse = %d, want 63", c.Current)
	}
}

// TestLinkLossDoesNotDespawnWhereItIsObserved pins the reason a departure crosses at
// all: only a direct neighbour sees a disconnect, and it sees it at a tick of its own
// transport's choosing, so removing the cursor there would remove it at a different
// tick on every instance — and never on one that shared no link with the departing
// participant. The observation produces an artifact; the artifact does the removal.
func TestLinkLossDoesNotDespawnWhereItIsObserved(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	remote := spawnRemoteCursor(t, w, 2, 25, 5, 7)
	net := ready(NewNetworkSystem(w)).(*NetworkSystem)

	net.noticeDeparture(7, false)
	w.Resources.Event.Queue.Consume()
	if got := w.Resources.Player.Slot(2); got != remote {
		t.Fatalf("slot 2 = %d after a link loss, want the cursor to survive until the crossing", got)
	}

	// Observing it twice announces once: a second notice for a participant already
	// accounted for is a duplicate whatever path it arrived by.
	before := net.statDuplicates.Load()
	net.receiveDeparture(3, mustJSON(t, event.ParticipantDepartedPayload{Participant: 7, Slot: 2}))
	if got := net.statDuplicates.Load(); got != before+1 {
		t.Fatalf("duplicate notices counted = %d, want %d", got, before+1)
	}
}

// holderWorld is a world holding participant 7's cursor in slot 2 and its bot,
// participant 8, in slot 3, on an authority whose session layer records each
// departure it is told of and whether it is final.
func holderWorld(t *testing.T) (*engine.World, *NetworkSystem, map[uint32]bool) {
	t.Helper()
	w, _, _ := testCursorWorld(t)
	spawnRemoteCursor(t, w, 2, 25, 5, 7)
	bot := spawnRemoteCursor(t, w, 3, 30, 5, 8)
	if c, ok := w.Components.Cursor.GetPtr(bot); ok {
		c.Holder = 7
	}
	host, guest := network.NewLoopbackPair(1, 2)
	t.Cleanup(func() { _ = host.Close(); _ = guest.Close() })
	w.Resources.Network = engine.NewNetworkResource(host)
	w.RunSafe(w.LatchSession)
	finals := map[uint32]bool{}
	w.Resources.Network.OnDeparture = func(id uint32, final bool) { finals[id] = final }
	return w, ready(NewNetworkSystem(w)).(*NetworkSystem), finals
}

// departures is every participant departure the queue holds.
func departures(w *engine.World) (out []event.ParticipantDepartedPayload) {
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if p, ok := ev.Payload.(*event.ParticipantDepartedPayload); ok && ev.Type == event.EventParticipantDeparted {
			out = append(out, *p)
		}
	}
	return out
}

// TestALinkLossTurnsAHoldersGroupWraithAndADismissalDespawnsIt: one crossing asks
// the same of a holder's cursor and its bot's, to wait as Wraiths or to go, and
// tells the session layer which, so a Wraith's identity and slot stay taken.
func TestALinkLossTurnsAHoldersGroupWraithAndADismissalDespawnsIt(t *testing.T) {
	for _, dismissed := range []bool{false, true} {
		w, net, finals := holderWorld(t)
		net.removeParticipant(&event.ParticipantDepartedPayload{Participant: 7, Slot: 2, Dismissed: dismissed})
		var asked []string
		for _, ev := range w.Resources.Event.Queue.Consume() {
			switch p := ev.Payload.(type) {
			case *event.CursorWraithRequestPayload:
				asked = append(asked, fmt.Sprintf("wraith %d", p.Slot))
			case *event.CursorDespawnRequestPayload:
				asked = append(asked, fmt.Sprintf("despawn %d", p.Slot))
			}
		}
		kind := "wraith"
		if dismissed {
			kind = "despawn"
		}
		if want := []string{kind + " 3", kind + " 2"}; !slices.Equal(asked, want) {
			t.Fatalf("a departure dismissed %t asked %v, want %v", dismissed, asked, want)
		}
		if final, ok := finals[7]; !ok || final != dismissed || finals[8] != dismissed || len(finals) != 2 {
			t.Fatalf("a departure dismissed %t told the session layer %v", dismissed, finals)
		}
	}
}

// wraithWorld is holderWorld with both cursors turned Wraith, and wait, which ages
// every Wraith through one cursor tick of the given length.
func wraithWorld(t *testing.T) (w *engine.World, net *NetworkSystem, wait func(time.Duration)) {
	t.Helper()
	w, net, _ = holderWorld(t)
	cursors := ready(NewCursorSystem(w)).(*CursorSystem)
	for _, slot := range []uint8{2, 3} {
		cursors.HandleEvent(event.GameEvent{Type: event.EventCursorWraithRequest,
			Payload: &event.CursorWraithRequestPayload{Slot: slot}})
	}
	w.Resources.Event.Queue.Consume()
	return w, net, func(d time.Duration) {
		w.Resources.Time.DeltaTime = d
		cursors.Update()
	}
}

// TestAWraithIsDroppedOnceItHasWaitedItsTime: the authority dismisses a Wraith
// WraithTimeout of game time after it turned, once, and a bot whose holder waits
// too goes in its holder's dismissal rather than one of its own.
func TestAWraithIsDroppedOnceItHasWaitedItsTime(t *testing.T) {
	w, net, wait := wraithWorld(t)
	expire := func(more time.Duration) []event.ParticipantDepartedPayload {
		wait(more)
		net.expireWraiths()
		return departures(w)
	}
	if got := expire(parameter.WraithTimeout - time.Millisecond); len(got) != 0 {
		t.Fatalf("Wraiths short of their time were dismissed: %+v", got)
	}
	want := event.ParticipantDepartedPayload{Participant: 7, Slot: 2, Dismissed: true}
	if got := expire(time.Millisecond); len(got) != 1 || got[0] != want {
		t.Fatalf("at their time the authority dismissed %+v, want %+v alone", got, want)
	}
	if got := expire(parameter.WraithTimeout); len(got) != 0 {
		t.Fatalf("a dismissal already crossed was crossed again: %+v", got)
	}
}

// TestAWraithOnItsWayBackIsNotDropped: a Wraith the session layer is giving back to
// its participant outlives its time until the arrival takes it back.
func TestAWraithOnItsWayBackIsNotDropped(t *testing.T) {
	w, net, wait := wraithWorld(t)
	back := true
	w.Resources.Network.WraithKept = func(participant uint32) bool { return back && participant == 7 }
	wait(parameter.WraithTimeout)
	if net.expireWraiths(); len(departures(w)) != 0 {
		t.Fatal("a Wraith on its way back was dismissed")
	}
	back = false
	if net.expireWraiths(); len(departures(w)) != 1 {
		t.Fatal("a Wraith whose return fell through did not go at its time")
	}
}

// TestAYieldedWraithIsDroppedAtOnce: the Wraith whose place the session layer gives
// a newcomer is dismissed without waiting out its time, and only it.
func TestAYieldedWraithIsDroppedAtOnce(t *testing.T) {
	w, net, _ := wraithWorld(t)
	spawnRemoteCursor(t, w, 4, 35, 5, 9)
	ready(NewCursorSystem(w)).(*CursorSystem).HandleEvent(event.GameEvent{Type: event.EventCursorWraithRequest,
		Payload: &event.CursorWraithRequestPayload{Slot: 4}})
	w.Resources.Event.Queue.Consume()
	yield := uint32(9)
	w.Resources.Network.DismissWraith = func() uint32 { id := yield; yield = 0; return id }
	net.expireWraiths()
	want := event.ParticipantDepartedPayload{Participant: 9, Slot: 4, Dismissed: true}
	if got := departures(w); len(got) != 1 || got[0] != want {
		t.Fatalf("a yield dismissed %+v, want %+v alone", got, want)
	}
	if net.expireWraiths(); len(departures(w)) != 0 {
		t.Fatal("a yield was crossed twice")
	}
}

// TestANoticeDismissesOnlyASeat: a seat its holder dropped is gone for good on
// whichever neighbour's word reaches the authority; a player whose link closed is
// only ever away.
func TestANoticeDismissesOnlyASeat(t *testing.T) {
	for _, tc := range []struct {
		participant uint32
		slot        uint8
	}{{8, 3}, {7, 2}} {
		w, net, _ := holderWorld(t)
		net.receiveDeparture(3, mustJSON(t, event.ParticipantDepartedPayload{Participant: tc.participant,
			Slot: tc.slot, Dismissed: true}))
		want := event.ParticipantDepartedPayload{Participant: tc.participant, Slot: tc.slot, Dismissed: tc.participant == 8}
		if got := departures(w); len(got) != 1 || got[0] != want {
			t.Fatalf("a dismissing notice for participant %d crossed %+v, want %+v", tc.participant, got, want)
		}
	}
}

// TestADepartureAResetInterruptedIsAnnouncedAgain: a departure a reset finds still
// scheduled, applied to cursors it rebuilds live, or noticed while it had cleared
// them is crossed again as it was, once, by the authority and no reproduction; one
// forged, or whose participant already waits, is back or is gone, is not.
func TestADepartureAResetInterruptedIsAnnouncedAgain(t *testing.T) {
	type departure = event.ParticipantDepartedPayload
	type world = func(*engine.World, *NetworkSystem)
	plain, gone := departure{Participant: 7, Slot: 2}, departure{Participant: 7, Slot: 2, Dismissed: true}
	dropped := departure{Participant: 7, Slot: 2, Dismissed: true, Dropped: true}
	cross := func(net *NetworkSystem, p departure) {
		net.schedule(event.GameEvent{Type: event.EventParticipantDeparted, Payload: &p,
			Origin: event.OriginSession, Domain: core.DomainPlayer})
	}
	settle := func(w *engine.World) *CursorSystem { // the cursor system takes what a roster change asked of it
		cursors := ready(NewCursorSystem(w)).(*CursorSystem)
		for _, ev := range w.Resources.Event.Queue.Consume() {
			cursors.HandleEvent(ev)
		}
		return cursors
	}
	apply := func(w *engine.World, net *NetworkSystem, p departure, settled bool) {
		cross(net, p)
		net.applyDue(net.lastApplyTick)
		if net.removeParticipant(&p); settled {
			settle(w)
		}
	}
	scheduled := func(p departure) world { return func(_ *engine.World, net *NetworkSystem) { cross(net, p) } }
	noticed := func(_ *engine.World, net *NetworkSystem) { net.noticeDeparture(7, false) }
	for name, tc := range map[string]struct {
		interrupted world // what the reset finds
		after       world // what the new game sees before its first flush
		want        []departure
	}{
		"scheduled":             {interrupted: scheduled(plain), want: []departure{plain}},
		"a dismissal scheduled": {interrupted: scheduled(gone), want: []departure{gone}},
		"a Wraith's drop scheduled at its time": {interrupted: func(w *engine.World, net *NetworkSystem) {
			apply(w, net, plain, false)
			w.Resources.Time.DeltaTime = parameter.WraithTimeout
			settle(w).Update()
			cross(net, dropped)
		}, want: []departure{dropped}},
		"applied to a holder and its bot": {
			interrupted: func(w *engine.World, net *NetworkSystem) { apply(w, net, plain, false) }, want: []departure{plain}},
		"noticed while its cursor was cleared": {interrupted: func(w *engine.World, net *NetworkSystem) {
			w.Resources.Player.PrepareRestore([]engine.CursorRosterEntry{{Slot: 2, PeerID: 7, Control: component.ControlRemote}})
			w.PushEvent(event.EventCursorDespawnRequest, &event.CursorDespawnRequestPayload{Slot: 2})
			settle(w)
			noticed(w, net)
		}, after: func(w *engine.World, _ *NetworkSystem) { // the boot's request rebuilds the roster
			w.PushEvent(event.EventCursorSpawnRequest, &event.CursorSpawnRequestPayload{Center: true})
			settle(w)
		}, want: []departure{plain}},
		"scheduled, and noticed again":                   {interrupted: scheduled(plain), after: noticed, want: []departure{plain}},
		"discarded before it crossed, and noticed again": {interrupted: noticed, after: noticed, want: []departure{plain}},
		"forged by a guest": {interrupted: func(_ *engine.World, net *NetworkSystem) {
			cross(net, gone)
			net.scheduled[len(net.scheduled)-1].source = 2
		}},
		"held by a guest": {interrupted: scheduled(plain),
			after: func(w *engine.World, _ *NetworkSystem) { w.Resources.Network.Authority.Store(2) }},
		"reproduced": {interrupted: scheduled(plain),
			after: func(_ *engine.World, net *NetworkSystem) { net.pushSessionRecords(nil) }},
		"waiting already": {interrupted: func(w *engine.World, net *NetworkSystem) { apply(w, net, plain, true) }},
		"gone for good":   {interrupted: func(w *engine.World, net *NetworkSystem) { apply(w, net, gone, true) }},
		"taken back since": {interrupted: func(w *engine.World, net *NetworkSystem) {
			apply(w, net, plain, true)
			net.addParticipant(&event.ParticipantJoinedPayload{Participant: 7, Slot: 2})
			settle(w)
		}},
	} {
		w, net, _ := holderWorld(t)
		port := w.Resources.Network.Port
		tc.interrupted(w, net)
		w.Resources.Event.Queue.Consume() // the reset discards what was still queued
		if net.Init(); tc.after != nil {
			tc.after(w, net)
		}
		net.pushSessionRecords(port)
		if got := departures(w); !slices.Equal(got, tc.want) {
			t.Fatalf("%s: the new game crossed %+v, want %+v", name, got, tc.want)
		}
		if net.pushSessionRecords(port); len(departures(w)) != 0 {
			t.Fatalf("%s: a departure announced again was announced twice", name)
		}
	}
}

// TestAnArrivalAResetInterruptedIsCrossedAgain: an arrival a reset finds still
// scheduled is crossed again as it was, once, by the authority, into the Wraith its
// participant left too; one applied already, forged, or held by a guest, is not. It
// stands for a departure ahead of it, and one behind it is crossed after it.
func TestAnArrivalAResetInterruptedIsCrossedAgain(t *testing.T) {
	type world = func(*engine.World, *NetworkSystem)
	arrival := event.ParticipantJoinedPayload{Participant: 7, Slot: 2, Key: "key", Name: "name",
		Record: &event.CursorRecord{Charges: []int{0, 3}, Energy: 4321}}
	left := event.ParticipantDepartedPayload{Participant: 7, Slot: 2}
	cross := func(net *NetworkSystem, t event.EventType, p any) {
		net.schedule(event.GameEvent{Type: t, Payload: p, Origin: event.OriginSession, Domain: core.DomainPlayer})
	}
	scheduled := func(_ *engine.World, net *NetworkSystem) { cross(net, event.EventParticipantJoined, &arrival) }
	leaves := func(_ *engine.World, net *NetworkSystem) { cross(net, event.EventParticipantDeparted, &left) }
	settle := func(w *engine.World, _ *NetworkSystem) { // the cursor system takes what was asked of it
		cursors := ready(NewCursorSystem(w)).(*CursorSystem)
		for _, ev := range w.Resources.Event.Queue.Consume() {
			cursors.HandleEvent(ev)
		}
	}
	waits := func(w *engine.World, net *NetworkSystem) { // its departure applied, its Wraith waits
		leaves(w, net)
		net.applyDue(net.lastApplyTick)
		net.removeParticipant(&left)
		settle(w, net)
	}
	dropped := func(w *engine.World, net *NetworkSystem) { // its link goes while the reset has its Wraith cleared
		net.departed[7] = false // a reset before this one cleared a Wraith's mark
		w.Resources.Player.PrepareRestore([]engine.CursorRosterEntry{{Slot: 2, PeerID: 7, Control: component.ControlRemote, Wraith: true}})
		w.PushEvent(event.EventCursorDespawnRequest, &event.CursorDespawnRequestPayload{Slot: 2})
		settle(w, net)
		net.noticeDeparture(7, false)
	}
	boot := func(w *engine.World, net *NetworkSystem) { // the boot's request rebuilds the roster
		w.PushEvent(event.EventCursorSpawnRequest, &event.CursorSpawnRequestPayload{Center: true})
		settle(w, net)
	}
	all := func(steps ...world) world {
		return func(w *engine.World, net *NetworkSystem) {
			for _, step := range steps {
				step(w, net)
			}
		}
	}
	crossed := func(w *engine.World) (changes []any) {
		for _, ev := range w.Resources.Event.Queue.Consume() {
			switch p := ev.Payload.(type) {
			case *event.ParticipantJoinedPayload:
				changes = append(changes, *p)
			case *event.ParticipantDepartedPayload:
				changes = append(changes, *p)
			}
		}
		return changes
	}
	for name, tc := range map[string]struct {
		interrupted world // what the reset finds
		after       world // what the new game sees before its first flush
		want        []any
	}{
		"scheduled":                      {interrupted: scheduled, want: []any{arrival}},
		"scheduled into its Wraith":      {interrupted: all(waits, scheduled), want: []any{arrival}},
		"scheduled behind its departure": {interrupted: all(leaves, scheduled), want: []any{arrival}},
		"scheduled into its Wraith, ahead of its departure": {
			interrupted: all(waits, scheduled, leaves), want: []any{arrival, left}},
		"scheduled into its Wraith, its link lost as the reset rebuilt it": {
			interrupted: all(waits, scheduled, dropped), after: boot, want: []any{arrival, left}},
		"applied already": {interrupted: func(w *engine.World, net *NetworkSystem) {
			scheduled(w, net)
			net.applyDue(net.lastApplyTick)
		}},
		"forged by a guest": {interrupted: func(w *engine.World, net *NetworkSystem) {
			scheduled(w, net)
			net.scheduled[len(net.scheduled)-1].source = 2
		}},
		"held by a guest": {interrupted: scheduled,
			after: func(w *engine.World, _ *NetworkSystem) { w.Resources.Network.Authority.Store(2) }},
	} {
		w, net, _ := holderWorld(t)
		port := w.Resources.Network.Port
		tc.interrupted(w, net)
		w.Resources.Event.Queue.Consume() // the reset discards what was still queued
		if net.Init(); tc.after != nil {
			tc.after(w, net)
		}
		net.pushSessionRecords(port)
		if changes := crossed(w); !reflect.DeepEqual(changes, tc.want) {
			got, _ := json.Marshal(changes) // a record prints as its pointer otherwise
			want, _ := json.Marshal(tc.want)
			t.Fatalf("%s: the new game crossed %s, want %s", name, got, want)
		}
		if net.pushSessionRecords(port); len(w.Resources.Event.Queue.Consume()) != 0 {
			t.Fatalf("%s: an arrival crossed again was crossed twice", name)
		}
	}
}

// TestADepartureWhileAResetRebuildsKeepsItsPlace: a participant whose cursor a
// reset cleared and is about to rebuild has left nothing yet, whether a departure
// or a dismissal applies or its lost link is noticed in between: the session layer
// hears of it when the new game announces it again, and until then keeps its place
// and takes nobody back into it.
func TestADepartureWhileAResetRebuildsKeepsItsPlace(t *testing.T) {
	for name, leave := range map[string]func(*NetworkSystem){
		"applied": func(net *NetworkSystem) {
			net.removeParticipant(&event.ParticipantDepartedPayload{Participant: 7, Slot: 2})
		},
		"dismissed": func(net *NetworkSystem) {
			net.removeParticipant(&event.ParticipantDepartedPayload{Participant: 7, Slot: 2, Dismissed: true})
		},
		"noticed": func(net *NetworkSystem) { net.noticeDeparture(7, false) },
	} {
		w, net, finals := holderWorld(t)
		w.Resources.Player.PrepareRestore([]engine.CursorRosterEntry{{Slot: 2, PeerID: 7, Control: component.ControlRemote}})
		ready(NewCursorSystem(w)).(*CursorSystem).HandleEvent(event.GameEvent{Type: event.EventCursorDespawnRequest,
			Payload: &event.CursorDespawnRequestPayload{Slot: 2}})
		leave(net)
		if final, told := finals[7]; told {
			t.Fatalf("%s: the session layer was told a participant the reset is rebuilding left (for good: %v)", name, final)
		}
	}
}

// TestCoordinatorLossRaisesLocalStatus covers the failure mode a digest cannot:
// once the host link is gone there is no peer left to disagree with, so the guest
// must report the disconnect directly rather than waiting for DESYNC. The guest
// continues as a local fork, and that fact survives a game reset.
func TestCoordinatorLossRaisesLocalStatus(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	host, guest := network.NewLoopbackPair(1, 2)
	w.Resources.Network = engine.NewNetworkResource(guest)
	w.RunSafe(w.LatchSession)
	net := ready(NewNetworkSystem(w)).(*NetworkSystem)

	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	net.Receive(1)

	want := "Host connection lost; continuing locally from the last authoritative state"
	got := ""
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if ev.Type != event.EventMetaStatusMessageRequest {
			continue
		}
		if p, ok := ev.Payload.(*event.MetaStatusMessagePayload); ok {
			got = p.Message
		}
	}
	if got != want {
		t.Fatalf("coordinator-loss message = %q, want %q", got, want)
	}
	if !net.statHostLost.Load() {
		t.Fatal("coordinator loss did not publish the persistent host-loss state")
	}
	net.Init()
	if !net.statHostLost.Load() {
		t.Fatal("a game reset erased the host-loss state")
	}
}

func TestDismissalCannotAnnounceClosingLinksAsDepartures(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	host, guest := network.NewLoopbackPair(1, 2)
	t.Cleanup(func() { _ = host.Close(); _ = guest.Close() })
	w.Resources.Network = engine.NewNetworkResource(guest)
	w.RunSafe(w.LatchSession)
	dismissed, lost := false, false
	w.Resources.Network.OnDismissed = func(bool, bool) { dismissed = true }
	w.Resources.Network.OnPeerLost = func(uint32) bool { lost = true; return false }
	net := ready(NewNetworkSystem(w)).(*NetworkSystem)
	net.receiveDeparture(3, mustJSON(t, event.ParticipantDepartedPayload{Participant: 1, Slot: 0}))
	if lost || len(w.Resources.Event.Queue.Consume()) != 0 {
		t.Fatal("a neighbour's closing link announced the still-connected host's departure")
	}
	net.removeParticipant(&event.ParticipantDepartedPayload{Participant: 2, Slot: 0, Dismissed: true})
	w.Resources.Event.Queue.Consume()
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	net.DrainOffTick()
	if !dismissed || lost || len(w.Resources.Event.Queue.Consume()) != 0 {
		t.Fatal("dismissal reported host loss or emitted another departure")
	}

	// Already-buffered notices from a retired participant cannot remove survivors.
	w, _, _ = testCursorWorld(t)
	net = ready(NewNetworkSystem(w)).(*NetworkSystem)
	net.removeParticipant(&event.ParticipantDepartedPayload{Participant: 2, Slot: 2})
	net.receiveDeparture(2, mustJSON(t, event.ParticipantDepartedPayload{Participant: 1, Slot: 0}))
	if net.departed[1] {
		t.Fatal("a retired participant announced the host's departure")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
