package network

import (
	"slices"
	"testing"
)

// TestALinkIsLookedUpWhereItsSiteAnswers: a front door's site is its host's https
// origin, and a WebSocket route's is its own, then that host's.
func TestALinkIsLookedUpWhereItsSiteAnswers(t *testing.T) {
	for addr, want := range map[string][]string{
		"lixen.com:7777":            {"https://lixen.com"},
		"[::1]:7777":                {"https://[::1]"},
		"wss://lixen.com/vif/ws/a1": {"https://lixen.com"},
		"wss://lixen.com:7777/a1":   {"https://lixen.com:7777", "https://lixen.com"},
		"ws://127.0.0.1:8080/a1":    {"http://127.0.0.1:8080", "https://127.0.0.1"},
	} {
		if got := linkSites(addr); !slices.Equal(got, want) {
			t.Errorf("linkSites(%q) = %q, want %q", addr, got, want)
		}
	}
}
