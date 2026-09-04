package app

import (
	"net"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

func testAddr(t *testing.T, host string) net.Addr {
	t.Helper()
	return &net.TCPAddr{IP: net.ParseIP(host), Port: 7777}
}

// TestAdmissionSpendsABudgetPerDiallingHost covers the amplification this bounds.
//
// A join costs the session a whole world read, encoded and sent; it costs the
// dialer one connect. A peer that joins and leaves in a loop is therefore free to
// it and expensive to everything else, and the budget is what makes the two
// comparable.
func TestAdmissionSpendsABudgetPerDiallingHost(t *testing.T) {
	t.Parallel()
	l := newAdmissionLimiter()

	for i := range parameter.NetworkAdmitBurst {
		if err := l.admit(testAddr(t, "10.0.0.1")); err != nil {
			t.Fatalf("admission %d of the burst was refused: %v", i+1, err)
		}
	}
	if err := l.admit(testAddr(t, "10.0.0.1")); err == nil {
		t.Fatal("the budget did not run out")
	}

	// Per host, which is the whole point: one peer cycling does not lock out the
	// rest of the roster.
	if err := l.admit(testAddr(t, "10.0.0.2")); err != nil {
		t.Fatalf("a second host was refused on the first host's budget: %v", err)
	}
}

// TestAdmissionIgnoresThePort pins the key. The port is what a dialer changes for
// free, so counting it would count nothing.
func TestAdmissionIgnoresThePort(t *testing.T) {
	t.Parallel()
	l := newAdmissionLimiter()
	for i := range parameter.NetworkAdmitBurst {
		addr := &net.TCPAddr{IP: net.ParseIP("10.0.0.3"), Port: 40000 + i}
		if err := l.admit(addr); err != nil {
			t.Fatalf("admission %d was refused: %v", i+1, err)
		}
	}
	if err := l.admit(&net.TCPAddr{IP: net.ParseIP("10.0.0.3"), Port: 55555}); err == nil {
		t.Fatal("a new source port bought a fresh budget")
	}
}

// TestAdmissionRefillsWithItsWindow is the non-vacuous half: a budget that never
// refilled would turn one crash-and-reconnect into a permanent exclusion.
func TestAdmissionRefillsWithItsWindow(t *testing.T) {
	t.Parallel()
	l := newAdmissionLimiter()
	l.window = time.Millisecond

	for range l.burst {
		if err := l.admit(testAddr(t, "10.0.0.4")); err != nil {
			t.Fatalf("burst refused: %v", err)
		}
	}
	if err := l.admit(testAddr(t, "10.0.0.4")); err == nil {
		t.Fatal("the budget did not run out")
	}
	time.Sleep(3 * time.Millisecond)
	if err := l.admit(testAddr(t, "10.0.0.4")); err != nil {
		t.Fatalf("the window did not refill: %v", err)
	}
}

// TestAdmissionDoesNotGrowWithoutBound keeps the defence from becoming the leak:
// the table is swept when an unseen host arrives, so a spray of addresses costs
// one window's worth of keys rather than one per dial.
func TestAdmissionDoesNotGrowWithoutBound(t *testing.T) {
	t.Parallel()
	l := newAdmissionLimiter()
	l.window = time.Millisecond

	for i := range l.max * 4 {
		_ = l.admit(&net.TCPAddr{IP: net.IPv4(10, byte(i>>16), byte(i>>8), byte(i)), Port: 1})
		if i%256 == 0 {
			time.Sleep(2 * time.Millisecond) // let the window pass so the sweep has work
		}
	}
	l.mu.Lock()
	tracked := len(l.seen)
	l.mu.Unlock()
	if tracked > l.max {
		t.Fatalf("the limiter tracks %d hosts, ceiling is %d", tracked, l.max)
	}
}
