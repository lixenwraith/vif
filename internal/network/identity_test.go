package network

import (
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/event"
)

func sampleIdentity() PeerIdentity {
	return PeerIdentity{
		Protocol: ProtocolVersion, Simulation: "abc123", CaptureSchema: 4,
		JournalSchema: 11, TickIntervalNS: 50_000_000,
		Seed: 0x5EED, Session: 1, ConfigID: "embedded", ContentID: "embedded",
		ContentFiles: 1, ContentBlocks: 14, ContentLines: 46,
	}
}

// TestVerifyNamesTheFirstDifference covers the message a person acts on. One field,
// named, with both values: a peer that differs in nine fields is a different build
// rather than nine problems, and the message says which one to look at first.
func TestVerifyNamesTheFirstDifference(t *testing.T) {
	t.Parallel()
	local := sampleIdentity()

	if err := local.Verify(sampleIdentity()); err != nil {
		t.Fatalf("two identical peers were refused: %v", err)
	}

	cases := map[string]func(*PeerIdentity){
		"protocol":       func(p *PeerIdentity) { p.Protocol = 99 },
		"simulation":     func(p *PeerIdentity) { p.Simulation = "deadbeef" },
		"capture_schema": func(p *PeerIdentity) { p.CaptureSchema = 3 },
		"journal_schema": func(p *PeerIdentity) { p.JournalSchema = 10 },
		"tick_ns":        func(p *PeerIdentity) { p.TickIntervalNS = 33_000_000 },
		"seed":           func(p *PeerIdentity) { p.Seed = 1 },
		"session":        func(p *PeerIdentity) { p.Session = 2 },
		"config_id":      func(p *PeerIdentity) { p.ConfigID = "wad/game/td/game.toml" },
		"content_id":     func(p *PeerIdentity) { p.ContentID = "elsewhere" },
		"content_pin":    func(p *PeerIdentity) { p.ContentPin = "tutorial.toml" },
		"content_files":  func(p *PeerIdentity) { p.ContentFiles = 2 },
		"content_blocks": func(p *PeerIdentity) { p.ContentBlocks = 15 },
		"content_lines":  func(p *PeerIdentity) { p.ContentLines = 47 },
	}
	for field, break_ := range cases {
		remote := sampleIdentity()
		break_(&remote)
		err := local.Verify(remote)
		if err == nil {
			t.Fatalf("%s: a differing peer was accepted", field)
		}
		if !strings.Contains(err.Error(), field) {
			t.Fatalf("%s: refusal %q does not name the field", field, err)
		}
		if !IsIdentityRefusal(err) {
			t.Fatalf("%s: refusal %q is not recognisable across the wire", field, err)
		}
	}
}

// TestVerifyBuildIgnoresWhatAPeerCannotKnowYet is the whole reason the check has
// two halves. A dialer compares before it has a world, so its seed, configuration
// and corpus do not exist; asking it to match them would refuse every join.
func TestVerifyBuildIgnoresWhatAPeerCannotKnowYet(t *testing.T) {
	t.Parallel()
	local := sampleIdentity()
	remote := sampleIdentity()
	remote.Seed, remote.Session, remote.ConfigID = 0, 0, ""
	remote.ContentID, remote.ContentFiles, remote.ContentBlocks, remote.ContentLines = "", 0, 0, 0

	if err := local.VerifyBuild(remote); err != nil {
		t.Fatalf("a peer with no world yet failed the build check: %v", err)
	}
	if err := local.Verify(remote); err == nil {
		t.Fatal("the full check accepted a peer running a different session")
	}

	remote = sampleIdentity()
	remote.Simulation = "deadbeef"
	if err := local.VerifyBuild(remote); err == nil {
		t.Fatal("the build check accepted a different simulation")
	}
}

// TestSessionFromKeepsTheBuildLocal is the rule that makes the check mean
// anything: the values a peer is measured against are this build's own, never the
// ones it sent. Reading them out of the anchor would compare a claim with itself.
func TestSessionFromKeepsTheBuildLocal(t *testing.T) {
	t.Parallel()
	build := PeerIdentity{
		Protocol: ProtocolVersion, Simulation: "abc123", CaptureSchema: 4,
		JournalSchema: 11, TickIntervalNS: 50_000_000,
	}
	got := build.SessionFrom(event.JournalAnchor{
		Schema: 999, TickInterval: 1, Seed: 0x5EED, Session: 3,
		ConfigID: "embedded", ContentID: "embedded",
		ContentFiles: 1, ContentBlocks: 14, ContentLines: 46,
	})

	if got.JournalSchema != 11 || got.TickIntervalNS != 50_000_000 {
		t.Fatalf("the anchor overwrote a build field: %+v", got)
	}
	if got.Simulation != "abc123" || got.CaptureSchema != 4 || got.Protocol != ProtocolVersion {
		t.Fatalf("a build field was lost: %+v", got)
	}
	if got.Seed != 0x5EED || got.Session != 3 || got.ContentBlocks != 14 {
		t.Fatalf("a session field was not adopted: %+v", got)
	}
}
