// Criteria for the addresses a participant declares and the port it binds them on.

package converge

import (
	"net"
	"testing"

	"github.com/lixenwraith/vif/internal/network"
)

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

	ln, declared := BindAdvertised("", "127.0.0.1:"+heldPort, cfg)
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

	// A host address with no port is the failure case, and it is a leaf rather than
	// an error: the participant plays and declares nothing.
	if ln, declared := BindAdvertised("", "not-an-address", cfg); ln != nil || declared != "" {
		t.Fatalf("a host address with no port bound %v and declared %q", ln, declared)
	}

	// An explicit -listen is declared whole, because the operator named a host the
	// coordinator must not overwrite from the connection.
	pinned, pinnedDeclared := BindAdvertised("127.0.0.1:0", "127.0.0.1:"+heldPort, cfg)
	if pinned == nil {
		t.Fatal("an explicit -listen did not bind")
	}
	defer pinned.Close()
	if pinnedDeclared != pinned.Addr().String() {
		t.Fatalf("declared %q for an explicit -listen, want %q", pinnedDeclared, pinned.Addr())
	}
}
