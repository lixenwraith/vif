package system

import (
	"encoding/json"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
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
	net := NewNetworkSystem(w).(*NetworkSystem)

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

// TestDepartureReleasesTheSlotSyncSequence covers slot reuse. Sequences are
// per-sender and restart at one, so a slot still holding its predecessor's high
// water mark would silently discard every sync from the participant that replaces it.
func TestDepartureReleasesTheSlotSyncSequence(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	remote := spawnRemoteCursor(t, w, 2, 25, 5, 7)
	net := NewNetworkSystem(w).(*NetworkSystem)

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
	net := NewNetworkSystem(w).(*NetworkSystem)

	net.noticeDeparture(7)
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

// TestCoordinatorLossRaisesLocalStatus covers the failure mode a digest cannot:
// once the host link is gone there is no peer left to disagree with, so the guest
// must report the disconnect directly rather than waiting for DESYNC. The guest
// continues as a local fork, and that fact survives a game reset.
func TestCoordinatorLossRaisesLocalStatus(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	host, guest := network.NewLoopbackPair(1, 2)
	w.Resources.Network = engine.NewNetworkResource(guest)
	net := NewNetworkSystem(w).(*NetworkSystem)

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

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
