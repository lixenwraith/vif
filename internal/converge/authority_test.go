// Criteria for authority continuity: which record a term accepts, what the
// succession elects, and where a survivor that can elect nobody ends up.

package converge

import (
	"slices"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// chainOf renders identities as chain entries; an in-process session needs no
// addresses, because its links already exist.
func chainOf(ids ...network.PeerID) network.SuccessionChain {
	var out network.SuccessionChain
	for _, id := range ids {
		out = append(out, network.ChainEntry{ID: id, Addr: "mesh"})
	}
	return out
}

// record is the handoff a criterion hands to an instance, carrying the membership
// that instance closed on — a record whose roster is not byte-identical is refused
// before anything is read from it.
func record(u *Authority, term network.AuthorityTerm, authority, predecessor network.PeerID) network.HandoffRecord {
	held := u.State()
	return network.HandoffRecord{
		Term: term, Authority: authority, Predecessor: predecessor,
		Roster: held.Roster, Anchor: held.Anchor, BarrierDelayTicks: held.Delay,
	}
}

// TestASecondHandoffForOneTermIsRefused: whatever two candidates believe, a
// participant adopts one record per term — and a record that skips a term, or
// carries a roster this session never closed on, is refused for the same reason.
func TestASecondHandoffForOneTermIsRefused(t *testing.T) {
	t.Parallel()
	guest := session(t, 3, [][2]int{{1, 2}, {2, 3}, {1, 3}})[2]

	base := record(guest.u, network.FirstTerm+1, 2, 1)
	if err := guest.u.adopt(base, 0); err != nil {
		t.Fatalf("the first record for a term must be adopted: %v", err)
	}
	rival := base
	rival.Authority = 3
	if err := guest.u.adopt(rival, 0); err == nil {
		t.Fatal("a second, different record for one term was adopted")
	}
	if got := guest.u.State(); got.Authority != 2 || got.Term != network.FirstTerm+1 {
		t.Fatalf("the refused record moved the authority anyway: %+v", got)
	}

	skipped := base
	skipped.Term = network.FirstTerm + 3
	if err := guest.u.adopt(skipped, 0); err == nil {
		t.Fatal("a record entering a term two generations ahead was adopted")
	}
	foreign := base
	foreign.Term = network.FirstTerm + 2
	foreign.Roster = append(slices.Clone(base.Roster), network.RosterEntry{ID: 4, Slot: 3})
	if err := guest.u.adopt(foreign, 0); err == nil {
		t.Fatal("a record carrying a roster this session never closed on was adopted")
	}
}

// TestTheTermGateRefusesWhatNobodyHandedThisInstance is the wire rule a fork runs
// under: older is ignored, equal acted on, newer refused rather than adopted, and
// a handoff that skips the term the fork missed is not a way back in.
func TestTheTermGateRefusesWhatNobodyHandedThisInstance(t *testing.T) {
	t.Parallel()
	fork := session(t, 3, [][2]int{{1, 2}, {2, 3}, {1, 3}})[2]

	// A succession nothing can elect: the window passes with no record.
	fork.u.beginSuccession(1)
	fork.u.giveUp()
	if got := fork.u.State(); !got.Fork {
		t.Fatalf("the instance did not become a local fork: %+v", got)
	}

	if fork.u.admit(network.FirstTerm+2, 1) {
		t.Fatal("a fork adopted an artifact from a term it was never handed")
	}
	if fork.stat("network.term_refused") == 0 {
		t.Fatal("the refusal was not counted")
	}
	if fork.u.admit(network.FirstTerm-1, 1) {
		t.Fatal("an artifact from a term the session has left was acted on")
	}
	if !fork.u.admit(network.FirstTerm, 1) {
		t.Fatal("an artifact from the term this instance holds was refused")
	}
	if got := fork.u.State(); got.Term != network.FirstTerm || !got.Fork {
		t.Fatalf("the refused artifacts moved the fork's authority: %+v", got)
	}
	if err := fork.u.adopt(record(fork.u, network.FirstTerm+2, 2, 1), 1); err == nil {
		t.Fatal("a fork adopted a handoff that skipped the term it missed")
	}
}

// TestAPinnedAuthorityDoesNotMove is the option a deployment needs and a person
// hosting a game does not. The policy is the coordinator's and travels in the
// offer, because a disagreement about it is one instance electing and one refusing.
// A pinned survivor still stops holding a cursor nobody will move again — that is
// the roster rule, not the authority one.
func TestAPinnedAuthorityDoesNotMove(t *testing.T) {
	t.Parallel()
	guest := newRun(t, 2, nil, roster(2))
	guest.u.Open(network.SessionOffer{
		Host: 1, Assigned: 2, Term: network.FirstTerm,
		Roster: roster(2), Chain: chainOf(1, 2), FixedAuthority: true,
		BarrierDelayTicks: parameter.NetworkBarrierDelayTicks,
	}, 2)

	// The succession still opens — opening it is what floods the loss to survivors
	// a relay away — and ends one window earlier than a fruitless one, because no
	// record is coming.
	guest.u.peerLost(1)

	got := guest.u.State()
	if !got.Fork {
		t.Fatalf("a pinned session did not fork on losing its authority: %+v", got)
	}
	if got.Authority == 2 || got.Term != network.FirstTerm {
		t.Fatalf("a pinned session moved its authority: %+v", got)
	}
	if !guest.reg.Bools.Get("network.host_lost").Load() {
		t.Fatal("the survivor did not report the loss")
	}
	if len(guest.world.abandoned) == 0 {
		t.Fatal("a survivor with no link kept the cursors nobody will move again")
	}
}

// TestTheSuccessionOrderIsTheOrderTheRuleElects pins what a survivor with no link
// retries down. It walks the same order the election runs in — the chain, then any
// other survivor — so a reconnect and a handoff cannot disagree about who is being
// waited for.
func TestTheSuccessionOrderIsTheOrderTheRuleElects(t *testing.T) {
	t.Parallel()
	r := newRun(t, 4, nil, roster(4))
	r.u.Open(network.SessionOffer{
		Host: 1, Assigned: 4, Term: network.FirstTerm,
		Roster: roster(4), Chain: chainOf(3, 4),
		BarrierDelayTicks: parameter.NetworkBarrierDelayTicks,
	}, 4)

	got := r.u.successionOrder()
	want := []network.PeerID{3, 2}
	if !slices.Equal(got, want) {
		t.Fatalf("succession order = %v, want the chain first: %v", got, want)
	}
}

// TestAGuestAdmitsAPeerLink is the failure a live session hit: admission read the
// coordinator's roster, which a guest never fills, so a survivor's link to the
// successor was refused and both forked. Admission is roster membership, and
// deliberately not the term — a survivor still electing is exactly who needs to
// open one.
func TestAGuestAdmitsAPeerLink(t *testing.T) {
	t.Parallel()
	guest := session(t, 3, [][2]int{{1, 2}, {1, 3}})[2]

	if err := guest.r.admitPeerLink(2); err != nil {
		t.Fatalf("a guest refused a participant of its own session: %v", err)
	}
	if err := guest.r.admitPeerLink(9); err == nil {
		t.Fatal("a guest admitted a participant that is not in the session")
	}
	if err := guest.r.admitPeerLink(3); err == nil {
		t.Fatal("a guest admitted a link from itself")
	}
}
