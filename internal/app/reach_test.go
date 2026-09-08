package app

import (
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/network"
)

// TestSuccessionSkipsALeaf is what the chain buys the election: the roster's
// lowest survivor declared no port, so no survivor could dial it, and under the
// roster rule alone it would take the term and author alone. The chain travels in
// the offer, so every survivor skips it and elects the same next candidate.
func TestSuccessionSkipsALeaf(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}, {1, 3}}, 3)
	localCursors(t, apps)
	primeRetention(t, apps)

	closeParticipant(apps[0])
	survivors := apps[1:]
	if !settleAuthority(t, survivors, func() bool {
		return authorityOf(survivors[0]).Term > network.FirstTerm &&
			authorityOf(survivors[1]).Term > network.FirstTerm
	}) {
		t.Fatalf("no succession: %+v and %+v",
			authorityOf(survivors[0]), authorityOf(survivors[1]))
	}
	for i, a := range survivors {
		if got := authorityOf(a).Authority; got != 3 {
			t.Fatalf("participant %d elected %d, want the first survivor in the chain",
				i+2, got)
		}
	}
	// The skipped participant is in the session and playing; it is only not a
	// candidate. Refusing it outright would turn a firewall into a lockout.
	if authorityOf(survivors[0]).Fork {
		t.Fatal("the leaf forked instead of following the successor")
	}
}

// TestSuccessionSurvivesLosingTheAuthorityAndThenTheSuccessor is gap 5: the
// participants that can still reach each other keep the session rather than each
// forking into a game of their own.
func TestSuccessionSurvivesLosingTheAuthorityAndThenTheSuccessor(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 4,
		[][2]int{{1, 2}, {1, 3}, {1, 4}, {2, 3}, {2, 4}, {3, 4}}, 2, 3, 4)
	localCursors(t, apps)
	primeRetention(t, apps)

	closeParticipant(apps[0])
	survivors := apps[1:]
	if !settleAuthority(t, survivors, func() bool {
		return authorityOf(survivors[0]).Term > network.FirstTerm
	}) {
		t.Fatalf("no first succession: %+v", authorityOf(survivors[0]))
	}
	if got := authorityOf(survivors[1]).Authority; got != 2 {
		t.Fatalf("the first succession elected %d, want participant 2", got)
	}

	// And now the successor itself, which used to fork everyone.
	closeParticipant(survivors[0])
	rest := survivors[1:]
	if !settleAuthority(t, rest, func() bool {
		return authorityOf(rest[0]).Term > network.FirstTerm+1
	}) {
		t.Fatalf("no second succession: %+v and %+v", authorityOf(rest[0]), authorityOf(rest[1]))
	}
	for i, a := range rest {
		got := authorityOf(a)
		if got.Term != network.FirstTerm+2 {
			t.Fatalf("participant %d entered term %d, want two increments", i+3, got.Term)
		}
		if got.Authority != 3 {
			t.Fatalf("participant %d elected %d after the successor went, want 3", i+3, got.Authority)
		}
		if got.Fork {
			t.Fatalf("participant %d forked with a peer it could still reach", i+3)
		}
	}
}

// TestTheSuccessionOrderIsTheOrderTheRuleElects pins what a survivor with no link
// retries down. It walks the same order the election runs in — the chain, then
// any other survivor — so a reconnect and a handoff cannot disagree about who is
// being waited for.
func TestTheSuccessionOrderIsTheOrderTheRuleElects(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 4,
		[][2]int{{1, 2}, {1, 3}, {1, 4}}, 3, 4)
	localCursors(t, apps)

	got := apps[3].authority.SuccessionOrder()
	want := []network.PeerID{3, 2}
	if len(got) != len(want) {
		t.Fatalf("succession order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("succession order = %v, want the chain first: %v", got, want)
		}
	}
}

// TestADeclaredAddressIsCompletedFromTheConnection is why a guest declares a port
// and not an address: it knows which port it bound and not which address the world
// reaches it at, and only the far end of an established stream knows both.
func TestADeclaredAddressIsCompletedFromTheConnection(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		declared, remote, want string
		ok                     bool
	}{
		{":7777", "203.0.113.9:51000", "203.0.113.9:7777", true},
		{"0.0.0.0:7777", "203.0.113.9:51000", "203.0.113.9:7777", true},
		{"10.0.0.4:7777", "203.0.113.9:51000", "10.0.0.4:7777", true},
		{":0", "203.0.113.9:51000", "", false},
		{"nonsense", "203.0.113.9:51000", "", false},
		{":7777", "", "", false},
	} {
		got, ok := resolveDeclared(c.declared, c.remote)
		if ok != c.ok || got != c.want {
			t.Errorf("resolveDeclared(%q, %q) = (%q, %t), want (%q, %t)",
				c.declared, c.remote, got, ok, c.want, c.ok)
		}
	}
}

// TestBindAdvertisedDeclaresWhatItBound covers the default and its fallback: the
// coordinator's own port, an OS-assigned one when that is taken, and the port
// actually bound in either case.
func TestBindAdvertisedDeclaresWhatItBound(t *testing.T) {
	t.Parallel()
	cfg := network.DebugConfig(network.RolePeer, "")

	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer held.Close()
	_, heldPort, _ := net.SplitHostPort(held.Addr().String())

	ln, declared := bindAdvertised("", "127.0.0.1:"+heldPort, cfg)
	if ln == nil {
		t.Fatal("the default bind gave up instead of falling back")
	}
	defer ln.Close()
	_, boundPort, _ := net.SplitHostPort(ln.Addr().String())
	if declared != ":"+boundPort {
		t.Fatalf("declared %q, want the port it bound (:%s)", declared, boundPort)
	}
	if boundPort == heldPort {
		t.Fatal("the fallback bound the port that was already taken")
	}

	// An explicit -listen is declared whole, because the operator named a host the
	// coordinator must not overwrite from the connection.
	pinned, pinnedDeclared := bindAdvertised("127.0.0.1:0", "127.0.0.1:"+heldPort, cfg)
	if pinned == nil {
		t.Fatal("an explicit -listen did not bind")
	}
	defer pinned.Close()
	if pinnedDeclared != pinned.Addr().String() {
		t.Fatalf("declared %q for an explicit -listen, want %q", pinnedDeclared, pinned.Addr())
	}
}

// TestALeafDeclaresNothing is the privacy decision and the failure case in one: a
// participant that will not advertise, and one that could not bind, both play
// normally and publish no address.
func TestALeafDeclaresNothing(t *testing.T) {
	t.Parallel()
	cfg := network.DebugConfig(network.RolePeer, "")
	if ln, declared := bindAdvertised("", "not-an-address", cfg); ln != nil || declared != "" {
		t.Fatalf("a host address with no port bound %v and declared %q", ln, declared)
	}
	a := mustHeadless(t, 3, 80, 24)
	if got := a.reach.declaredAddr(); got != "" {
		t.Fatalf("a solo run declares %q", got)
	}
	if statOf(a, "network.chain") != 0 {
		t.Fatal("a solo run reports succession candidates")
	}
	a.Close()
}

// TestAGuestAdmitsAPeerLink is the failure a live session hit: admission read the
// coordinator's roster, which a guest never fills, so a survivor's link to the
// successor was refused and both forked.
func TestAGuestAdmitsAPeerLink(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {1, 3}}, 2, 3)
	localCursors(t, apps)

	guest := apps[2]
	if err := guest.admitPeerLink(2); err != nil {
		t.Fatalf("a guest refused a participant of its own session: %v", err)
	}
	if err := guest.admitPeerLink(9); err == nil {
		t.Fatal("a guest admitted a participant that is not in the session")
	}
	if err := guest.admitPeerLink(3); err == nil {
		t.Fatal("a guest admitted a link from itself")
	}
}

// TestTheSessionLineReportsTheAddressesItHolds pins a summary field against the
// counter that actually carries it. "confirmed reachable" is the size of the
// address book a survivor dials down, which is network.chain; reading an
// unregistered key would return a detached cell, so the line could never say it
// and every read would count itself late.
func TestTheSessionLineReportsTheAddressesItHolds(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {1, 3}}, 2, 3)
	host := apps[0]
	host.corrections.driveAuthority()

	chain := statOf(host, "network.chain")
	if chain == 0 {
		t.Fatal("a session with a published chain reports no candidates")
	}
	late := statOf(host, "stat.late")
	summary := host.SessionSummary()
	if want := fmt.Sprintf("%d confirmed reachable", chain); !strings.Contains(summary, want) {
		t.Fatalf("session line %q does not report %q", summary, want)
	}
	if got := statOf(host, "stat.late"); got != late {
		t.Fatalf("the session line registered %d late metric(s)", got-late)
	}
}
