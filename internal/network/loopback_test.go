package network

import "testing"

func TestLoopbackCloseDisconnectsBothEnds(t *testing.T) {
	a, b := NewLoopbackPair(1, 2)
	a.Close()

	if a.IsRunning() || b.IsRunning() || a.PeerCount() != 0 || b.PeerCount() != 0 {
		t.Fatalf("closed pair state = (%t/%d, %t/%d), want both down",
			a.IsRunning(), a.PeerCount(), b.IsRunning(), b.PeerCount())
	}
	for _, tc := range []struct {
		name     string
		endpoint *Loopback
	}{{"a", a}, {"b", b}} {
		var inbound [4]Inbound
		n := tc.endpoint.Drain(inbound[:])
		seen := false
		for _, in := range inbound[:n] {
			seen = seen || in.Kind == InboundDisconnect
		}
		if !seen {
			t.Errorf("endpoint %s drained %#v without a disconnect", tc.name, inbound[:n])
		}
	}
}
