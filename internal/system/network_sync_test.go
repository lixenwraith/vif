package system

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

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
	w.Resources.Network.OnDismissed = func() { dismissed = true }
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
