package network

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/event"
)

func sampleIdentity() PeerIdentity {
	return PeerIdentity{
		Protocol: ProtocolVersion, Simulation: "abc123", CaptureSchema: 4,
		JournalSchema: 11, TickIntervalNS: 50_000_000,
		Seed: 0x5EED, Session: 1, ScenarioDigest: "sha256:2604b13fd259",
		Build: "1154b8a6",
	}
}

// TestVerifyNamesTheFirstDifference covers the message a person acts on. One field,
// named, with both values, and another build named on both sides: a peer that
// differs in nine fields is a different build rather than nine problems, and the
// message says which builds they are and which field to look at first. A name
// alone is no difference.
func TestVerifyNamesTheFirstDifference(t *testing.T) {
	t.Parallel()
	local := sampleIdentity()

	renamed := sampleIdentity()
	renamed.Build = "9b6df60e"
	if err := local.Verify(renamed); err != nil {
		t.Fatalf("two peers differing in their names alone were refused: %v", err)
	}

	cases := map[string]func(*PeerIdentity){
		"protocol":        func(p *PeerIdentity) { p.Protocol = 99 },
		"simulation":      func(p *PeerIdentity) { p.Simulation = "deadbeef" },
		"capture_schema":  func(p *PeerIdentity) { p.CaptureSchema = 3 },
		"journal_schema":  func(p *PeerIdentity) { p.JournalSchema = 10 },
		"tick_ns":         func(p *PeerIdentity) { p.TickIntervalNS = 33_000_000 },
		"seed":            func(p *PeerIdentity) { p.Seed = 1 },
		"session":         func(p *PeerIdentity) { p.Session = 2 },
		"scenario_digest": func(p *PeerIdentity) { p.ScenarioDigest = "sha256:deadbeef0000" },
	}
	for field, break_ := range cases {
		remote := renamed
		break_(&remote)
		err := local.Verify(remote)
		if err == nil {
			t.Fatalf("%s: a differing peer was accepted", field)
		}
		if !strings.Contains(err.Error(), field) {
			t.Fatalf("%s: refusal %q does not name the field", field, err)
		}
		if !errors.Is(err, &Refusal{Code: RefusalIdentity}) {
			t.Fatalf("%s: refusal %q is not an identity refusal", field, err)
		}
		const builds = "vif 1154b8a6 here and vif 9b6df60e on the other side"
		if local.VerifyBuild(remote) != nil && !strings.Contains(err.Error(), builds) {
			t.Fatalf("%s: refusal %q does not name both builds", field, err)
		}
	}

	older := sampleIdentity()
	older.Build, older.Protocol = "", ProtocolVersion-1
	if err := local.VerifyBuild(older); err == nil || !strings.Contains(err.Error(), "an older vif on the other side") {
		t.Fatalf("a build that predates naming itself was refused with %v", err)
	}
}

// TestARefusalIsToldInItsReadersTerms: each side reads its own build as the one
// here, on a join its host refuses after the joiner's reply, which the joiner reads
// off the compressed stream it opened, and on a peer link.
func TestARefusalIsToldInItsReadersTerms(t *testing.T) {
	ours, theirs := sampleIdentity(), sampleIdentity()
	theirs.Build, theirs.Simulation = "9b6df60e", "deadbeef"
	const readsOurs = "vif 1154b8a6 here and vif 9b6df60e on the other side"
	const readsTheirs = "vif 9b6df60e here and vif 1154b8a6 on the other side"
	check := func(side string, err error, want string) {
		t.Helper()
		if !errors.Is(err, &Refusal{Code: RefusalIdentity}) || !strings.Contains(err.Error(), want) {
			t.Errorf("the %s read %v, want a refusal naming %q", side, err, want)
		}
	}

	// The joiner skipped its own check, as one configured with no identity does.
	offer := testOffer()
	offer.Identity = ours
	hostCfg := DebugConfig(RoleHost, "127.0.0.1:0")
	hostCfg.ParticipantID = offer.Host
	hostCfg.AcceptSession = HostAcceptor(Coordinator{Assign: func([]string) (SessionOffer, error) { return offer, nil }}, time.Second)
	host := NewSocketPort(hostCfg)
	defer host.Close()
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	pending, _, err := DialSession(host.Addr().String(), DebugConfig(RolePeer, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Close()
	if err := pending.Complete(nil, JoinerReport{Identity: theirs}); err != nil {
		t.Fatal(err)
	}
	_, err = pending.WaitStart()
	check("joiner", err, readsTheirs)
	select {
	case err := <-host.Errors():
		check("host", err, readsOurs)
	case <-time.After(2 * time.Second):
		t.Fatal("the host never refused the joiner")
	}

	accept := PeerAcceptor(PeerGate{Local: func() PeerID { return 2 }, Identity: ours}, time.Second)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			_, err = accept(conn)
			_ = conn.Close()
		}
		accepted <- err
	}()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, err = PeerDialer(theirs, time.Second)(conn, 3)
	check("dialler", err, readsTheirs)
	check("acceptor", <-accepted, readsOurs)
}

// TestVerifyBuildIgnoresWhatAPeerCannotKnowYet is the whole reason the check has
// two halves. A dialer compares before it has a world, so its seed, configuration
// and corpus do not exist; asking it to match them would refuse every join.
func TestVerifyBuildIgnoresWhatAPeerCannotKnowYet(t *testing.T) {
	t.Parallel()
	local := sampleIdentity()
	remote := sampleIdentity()
	remote.Seed, remote.Session, remote.ScenarioDigest = 0, 0, ""

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
		ScenarioID: "embedded", ScenarioDigest: "sha256:2604b13fd259",
	})

	if got.JournalSchema != 11 || got.TickIntervalNS != 50_000_000 {
		t.Fatalf("the anchor overwrote a build field: %+v", got)
	}
	if got.Simulation != "abc123" || got.CaptureSchema != 4 || got.Protocol != ProtocolVersion {
		t.Fatalf("a build field was lost: %+v", got)
	}
	if got.Seed != 0x5EED || got.Session != 3 || got.ScenarioDigest != "sha256:2604b13fd259" {
		t.Fatalf("a session field was not adopted: %+v", got)
	}
}
