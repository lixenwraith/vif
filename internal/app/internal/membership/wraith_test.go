// The membership's reading of the Wraiths a world holds, over a world that holds
// nothing else.

package membership

import (
	"slices"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	link "github.com/lixenwraith/vif/pkg/network"
)

// wraithWorld is a membership over a bare world whose cursors stand in the slots
// named, each the participant and claim given, a Wraith that has waited as long as
// waited says, or no Wraith for a negative wait.
func wraithWorld(cursors ...component.CursorComponent) func(waited ...time.Duration) *Membership {
	return func(waited ...time.Duration) *Membership {
		w := engine.NewWorld()
		ctx := engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock()) // the resources a world's context gives it
		for i, c := range cursors {
			e := w.CreateEntity(core.DomainShared)
			w.Components.Cursor.SetComponent(e, c)
			if waited[i] >= 0 {
				w.Components.Wraith.SetComponent(e, component.WraithComponent{Waited: waited[i]})
			}
			w.Resources.Player.Bind(c.Slot, e)
		}
		return &Membership{world: w, ctx: ctx}
	}
}

// TestAWraithWaitsUnderTheClaimItsRosterNames: a Wraith keeps the participant and
// holder it left with and takes the claim the roster names it under, as a session a
// run opens names its bots' afresh; an entry naming somebody else leaves it as it was.
func TestAWraithWaitsUnderTheClaimItsRosterNames(t *testing.T) {
	t.Parallel()
	const slot, id = 1, 2
	left := network.ClaimDigest(network.NewClaimToken())
	m := wraithWorld(component.CursorComponent{Slot: slot, PeerID: id, Claim: left})(time.Second)
	cursor := func() component.CursorComponent {
		c, _ := m.world.Components.Cursor.GetComponent(m.world.Resources.Player.Slot(slot))
		return c
	}
	bind := func(p network.RosterEntry) {
		m.bindCursorOwnersLocked([]network.RosterEntry{p}, engine.CoordinatorParticipant)
	}

	bind(network.RosterEntry{ID: id + 1, Slot: slot, Claim: network.ClaimDigest(network.NewClaimToken())})
	if c := cursor(); c.Claim != left || c.PeerID != id {
		t.Fatalf("an entry naming somebody else left the Wraith as participant %d under %q; want %d under its own claim", c.PeerID, c.Claim, id)
	}
	carried := network.ClaimDigest(network.NewClaimToken())
	bind(network.RosterEntry{ID: id, Slot: slot, Holder: 9, Claim: carried})
	waiting := m.world.Components.Wraith.HasEntity(m.world.Resources.Player.Slot(slot))
	if c := cursor(); c.Claim != carried || c.PeerID != id || c.Holder != 0 || !waiting {
		t.Fatalf("the Wraith is participant %d held by %d under %q, a Wraith %t; want %d held by nobody under %q, still waiting",
			c.PeerID, c.Holder, c.Claim, waiting, id, carried)
	}
}

// TestASuccessorOrdersWhoIsAwayByTheirWaits: a session taken over gives a newcomer
// the place of whoever has waited longest, read off the Wraiths, not the roster: an
// entry away whose cursor is no Wraith, or that has no slot, is nobody's to give, nor
// is one the roster does not count away.
func TestASuccessorOrdersWhoIsAwayByTheirWaits(t *testing.T) {
	t.Parallel()
	m := wraithWorld(
		component.CursorComponent{Slot: 0, PeerID: 1},
		component.CursorComponent{Slot: 1, PeerID: 2},
		component.CursorComponent{Slot: 2, PeerID: 3},
		component.CursorComponent{Slot: 3, PeerID: 4},
	)(30*time.Second, 4*time.Second, 9*time.Second, -1)
	roster := []network.RosterEntry{
		{ID: 1, Slot: 0}, // not away, whatever its cursor
		{ID: 2, Slot: 1, Away: true},
		{ID: 3, Slot: 2, Away: true},
		{ID: 4, Slot: 3, Away: true}, // back in its cursor, which no longer waits
		{ID: 5, Slot: parameter.NoPlayerSlot, Away: true},
	}
	if got, want := m.awayOrderLocked(roster), []link.PeerID{3, 2}; !slices.Equal(got, want) {
		t.Fatalf("away longest first reads %v, want %v", got, want)
	}
}

// TestASessionARunOpensIsAwayOnlyFromWhomItKept: whom a run kept a place for is who
// is away from the session it opens, longest first; who was away from the one it
// left, on the way back to it, or asked to give a place up there stays with that
// session.
func TestASessionARunOpensIsAwayOnlyFromWhomItKept(t *testing.T) {
	t.Parallel()
	m := wraithWorld(
		component.CursorComponent{Slot: 1, PeerID: 2},
		component.CursorComponent{Slot: 2, PeerID: 3},
	)(4*time.Second, 9*time.Second)
	m.awayOrder, m.yielding = []link.PeerID{5}, 5
	m.returning = map[link.PeerID]returnPlace{6: {entry: network.RosterEntry{ID: 6, Slot: 4, Away: true}}}
	m.dismissWraith.Store(5)
	m.openAwayLocked([]network.RosterEntry{{ID: 2, Slot: 1, Away: true}, {ID: 3, Slot: 2, Away: true}})
	if want := []link.PeerID{3, 2}; !slices.Equal(m.awayOrder, want) || m.yielding != 0 || len(m.returning) != 0 || m.dismissWraith.Load() != 0 {
		t.Fatalf("the session opened with %v away, %d yielding, %d on the way back and %d to dismiss; want %v away and nobody else",
			m.awayOrder, m.yielding, len(m.returning), m.dismissWraith.Load(), want)
	}
}
