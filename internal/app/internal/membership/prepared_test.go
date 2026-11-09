package membership

import (
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/lixenwraith/vif/internal/app/internal/config"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/resource"
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
	m := &Membership{sessionOffer: built}
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

// TestAJoinTakesItsScenarioFromTheDownloadsBeforeAskingTheHost: a join whose roots
// lack the session's bytes and whose downloads hold them plays that copy under the
// session's name without asking the host, which the nil handshake here would show.
func TestAJoinTakesItsScenarioFromTheDownloadsBeforeAskingTheHost(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(paths.SystemRootsEnv, "")
	sc, err := resource.ReadScenario("duel", fstest.MapFS{
		paths.ScenarioFile: {Data: []byte("[regions.main]\nfile = \"main.toml\"\n")},
		"main.toml":        {Data: []byte("[states.MainStart]\nparent = \"Root\"\n")},
	}, paths.ScenarioFile)
	if err != nil {
		t.Fatal(err)
	}
	downloads := filepath.Join(t.TempDir(), "downloads")
	if _, err := resource.SaveDownload(downloads, sc); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Resources: resource.Options{Dir: t.TempDir(), Scenario: "duel", Downloads: downloads}}
	offer := network.SessionOffer{Anchor: event.JoinAnchor{Anchor: event.JournalAnchor{
		ScenarioID: "duel", ScenarioDigest: sc.Digest()}}}
	if notice, err := ResolveJoinScenario(&cfg, nil, offer); err != nil || notice != "" {
		t.Fatalf("resolve = %q, %v", notice, err)
	}
	if got := cfg.Resources.Provided; got == nil || got.Digest() != sc.Digest() || got.Name != "duel" {
		t.Fatalf("the join holds %+v; want the downloads' copy, named duel", got)
	}
}
