package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lixenwraith/vif/internal/network"
	link "github.com/lixenwraith/vif/pkg/network"
)

// TestSuccessionSkipsALeaf is what the chain buys the election: a survivor that
// declared no port is no candidate, since nobody could dial it. The chain travels in
// the offer, so every survivor skips it and follows the same candidate.
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
			t.Fatalf("participant %d follows %d, want the chain's first candidate",
				i+2, got)
		}
	}
	// The skipped participant is in the session and playing; it is only not a
	// candidate. Refusing it outright would turn a firewall into a lockout.
	if authorityOf(survivors[0]).Fork {
		t.Fatal("the leaf forked instead of following the successor")
	}
}

// TestSuccessionSurvivesLosingTheAuthorityAndThenTheSuccessor: the participants
// that can still reach each other keep the session rather than each forking into a
// game of their own.
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

	// And now the successor itself: the rest elect again rather than fork.
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

// TestAGuestListensOnlyWhereItsPlayerLetTheSessionMoveToIt: a guest binds a port and
// declares it, which the session shares with its players, only in a session whose
// host chose migrate and only with Take over as host on or -listen; any other guest
// is a leaf, and the host's chain names nobody.
func TestAGuestListensOnlyWhereItsPlayerLetTheSessionMoveToIt(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	for _, tt := range []struct {
		name     string
		policy   string
		guest    func(*Config)
		declares bool
	}{
		{"a migrate session, nothing asked", AuthorityMigrate, func(*Config) {}, false},
		{"a migrate session, taking over on", AuthorityMigrate, func(c *Config) { c.Play = takingOver() }, true},
		{"a migrate session, -listen", AuthorityMigrate, func(c *Config) { c.Membership.ListenAddress = freeAddress(t) }, true},
		{"a session that ends with its host, taking over on", AuthorityHost, func(c *Config) { c.Play = takingOver() }, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			host := mustHeadless(t, fixtureSeed, BotWidth, BotHeight)
			tickUntilCursor(t, host)
			var err error
			host.World().RunSafe(func() { err = host.ctx.SessionCtl.BeginHosting("127.0.0.1:0", tt.policy) })
			if err != nil {
				t.Fatalf("begin hosting: %v", err)
			}
			stop := tickInBackground(host)
			t.Cleanup(func() { stop(); host.Close() })
			cfg := Config{Membership: Membership{JoinAddress: host.HostAddr()}}
			tt.guest(&cfg)
			guest, _ := driveBot(t, cfg, "patrol", nil)
			waitForCursors(t, host, 2)
			id := link.PeerID(guest.membership.LocalParticipant())
			declared := guest.reach.DeclaredAddr()
			if (declared != "") != tt.declares || boolOf(guest, "network.listening") != tt.declares {
				t.Fatalf("the guest declares %q, listening %t; want an address declared: %t",
					declared, boolOf(guest, "network.listening"), tt.declares)
			}
			if !tt.declares {
				if chain := host.authority.Chain(); len(chain) != 0 {
					t.Fatalf("the host's chain names %v; want nobody", chain)
				}
				return
			}
			waitUntil(t, func() bool { addr, ok := host.authority.Chain().Lookup(id); return ok && addr != "" },
				"the guest's address in the host's chain")
		})
	}
}

// TestALeafDeclaresNothing is the privacy decision: a participant that binds no
// port of its own plays normally and publishes no address.
func TestALeafDeclaresNothing(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 3, 80, 24)
	if got := a.reach.DeclaredAddr(); got != "" {
		t.Fatalf("a solo run declares %q", got)
	}
	if statOf(a, "network.chain") != 0 {
		t.Fatal("a solo run reports succession candidates")
	}
	a.Close()
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
	host.ApplyPendingCorrections()

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
