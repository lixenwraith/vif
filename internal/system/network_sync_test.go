package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
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

// TestPeerDespawnReleasesTheSlotSyncSequence covers slot reuse. Sequences are
// per-sender and restart at one, so a slot still holding its predecessor's high
// water mark would silently discard every sync from the participant that replaces it.
func TestPeerDespawnReleasesTheSlotSyncSequence(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	remote := spawnRemoteCursor(t, w, 2, 25, 5, 7)
	net := NewNetworkSystem(w).(*NetworkSystem)

	net.writeCursorState(syncFor(remote, 2, 5, 40))
	if c, _ := w.Components.Energy.GetComponent(remote); c.Current != 40 {
		t.Fatalf("remote energy after sync = %d, want 40", c.Current)
	}

	net.despawnPeer(7)
	if net.lastSync[2] != 0 {
		t.Fatalf("slot 2 sync sequence = %d after despawn, want 0", net.lastSync[2])
	}

	// The successor's first message starts over at one and must still be admitted.
	net.writeCursorState(syncFor(remote, 2, 1, 63))
	if c, _ := w.Components.Energy.GetComponent(remote); c.Current != 63 {
		t.Fatalf("remote energy after slot reuse = %d, want 63", c.Current)
	}
}
