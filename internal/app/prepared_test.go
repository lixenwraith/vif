package app

import (
	"testing"

	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
)

// TestASeatTakesOnlyTheGameItsWorldWasBuiltOn: a press seats its run in the game its
// world was built on, at whatever tick that game has reached: the seed, the session
// counter and the scenario its anchor named, reached through the same relay, under
// the same authority. Any one of them moved is another game, which a retry builds a
// world for.
func TestASeatTakesOnlyTheGameItsWorldWasBuiltOn(t *testing.T) {
	t.Parallel()
	built := network.SessionOffer{Relay: 3, Anchor: event.JoinAnchor{Anchor: event.JournalAnchor{
		Seed: 0x3036, Session: 2, ScenarioDigest: "sha256:0a0a", Tick: 40}}}
	m := &membership{sessionOffer: built}
	for _, tt := range []struct {
		name  string
		moved func(*network.SessionOffer)
		same  bool
	}{
		{"the game further on", func(o *network.SessionOffer) { o.Anchor.Anchor.Tick += 100 }, true},
		{"another seed", func(o *network.SessionOffer) { o.Anchor.Anchor.Seed++ }, false},
		{"the session's next game", func(o *network.SessionOffer) { o.Anchor.Anchor.Session++ }, false},
		{"another scenario", func(o *network.SessionOffer) { o.Anchor.Anchor.ScenarioDigest = "sha256:0b0b" }, false},
		{"reached at the host", func(o *network.SessionOffer) { o.Relay = 0 }, false},
		{"its authority pinned", func(o *network.SessionOffer) { o.FixedAuthority = true }, false},
	} {
		o := built
		tt.moved(&o)
		if got := m.preparedFor(o); got != tt.same {
			t.Errorf("%s: prepared for it %t, want %t", tt.name, got, tt.same)
		}
	}
}
