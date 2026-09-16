// Criteria for the playout lead: who authors one, what a measurement moves it to,
// and what happens to a link that asks for more than the ceiling can absorb.

package converge

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// stageLead installs what the barrier defers by and what the links ask for, as App
// answers both.
func stageLead(r *run, current, target uint64, overrun ...network.PeerID) {
	r.world.mu.Lock()
	defer r.world.mu.Unlock()
	r.world.leadCurrent, r.world.leadTarget, r.world.leadOverrun = current, target, overrun
}

func published(r *run) []uint64 {
	r.world.mu.Lock()
	defer r.world.mu.Unlock()
	return append([]uint64(nil), r.world.leads...)
}

// TestOnlyTheAuthorityPublishesAPlayoutLead: the lead is session identity, so a
// guest deriving its own from a measurement nobody else took would defer its
// crossings by a lead the rest of the session is not using.
func TestOnlyTheAuthorityPublishesAPlayoutLead(t *testing.T) {
	t.Parallel()
	runs := session(t, 2, [][2]int{{1, 2}})
	for _, r := range runs {
		stageLead(r, 3, 7)
		r.c.Apply()
	}
	if got := published(runs[0]); len(got) == 0 || got[len(got)-1] != 7 {
		t.Fatalf("the authority published %v, want its links' 7", got)
	}
	if got := published(runs[1]); len(got) != 0 {
		t.Fatalf("a guest published %v; only the authority may author a lead", got)
	}
}

// TestASuccessorDoesNotReAnnounceALeadItNeverDerived: a participant that takes the
// term inherits a session already deferring by a value it never chose, so the policy
// reads the barrier rather than its own last copy — announcing that copy would move
// the whole session back to it.
func TestASuccessorDoesNotReAnnounceALeadItNeverDerived(t *testing.T) {
	t.Parallel()
	host := session(t, 2, [][2]int{{1, 2}})[0]
	stageLead(host, 1, 1) // the session narrowed to one tick while somebody else authored
	host.c.Apply()
	if got := published(host); len(got) != 1 || got[0] != 1 {
		t.Fatalf("the new authority announced %v, want the 1 tick the session is on", got)
	}
}

// TestAPlayoutLeadRisesAtOnceAndFallsOnAWindow is the asymmetry the policy is made
// of: an artifact that misses the lead costs a correction and one that clears it
// costs nothing, so widening is immediate and narrowing waits out a window. The
// same window re-announces an unchanged lead, so a change a peer never received
// costs a window rather than the rest of the session.
func TestAPlayoutLeadRisesAtOnceAndFallsOnAWindow(t *testing.T) {
	t.Parallel()
	const window = parameter.NetworkBarrierRenegotiateTicks

	next, _, announce := chooseLead(3, 9, 100, 100, 0)
	if next != 9 || !announce {
		t.Fatalf("a wider measurement gave lead=%d announce=%v, want 9 announced at once", next, announce)
	}

	next, lowSince, announce := chooseLead(9, 2, 100, 100, 0)
	if next != 9 || lowSince != 100 {
		t.Fatalf("a narrower measurement gave lead=%d since=%d, want 9 held from tick 100", next, lowSince)
	}
	if next, _, _ = chooseLead(9, 2, 100+window-1, 100, 100); next != 9 {
		t.Fatalf("the lead narrowed to %d inside the window", next)
	}
	if next, _, announce = chooseLead(9, 2, 100+window, 100, 100); next != 2 || !announce {
		t.Fatalf("after the window the lead is %d announce=%v, want 2 announced", next, announce)
	}

	if _, _, announce = chooseLead(3, 3, 100+window-1, 100, 0); announce {
		t.Fatal("an unchanged lead was re-announced inside its window")
	}
	if _, _, announce = chooseLead(3, 3, 100+window, 100, 0); !announce {
		t.Fatal("an unchanged lead was never re-announced")
	}
}

// TestALinkPastThePlayoutCeilingIsDropped: past the ceiling the lead has stopped
// being an interpolation buffer and become input latency every participant pays
// for one link, so the session loses that participant instead.
func TestALinkPastThePlayoutCeilingIsDropped(t *testing.T) {
	t.Parallel()
	host := session(t, 2, [][2]int{{1, 2}})[0]
	stageLead(host, 3, parameter.NetworkBarrierMaxDelayTicks, 2)
	host.c.Apply()

	host.world.mu.Lock()
	dropped := append([]uint32(nil), host.world.dropped...)
	host.world.mu.Unlock()
	if len(dropped) != 1 || dropped[0] != 2 {
		t.Fatalf("the authority dropped %v, want the participant past the ceiling", dropped)
	}
}
