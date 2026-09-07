package network

import (
	"errors"
	"net"
	"testing"
	"time"
)

// TestSuccessionReadsTheChain covers the rule and the property that makes it safe
// without agreement: a chain and any prefix of it name the same first survivor.
func TestSuccessionReadsTheChain(t *testing.T) {
	roster := []SessionParticipant{{ID: 1, Slot: 0}, {ID: 2, Slot: 1}, {ID: 3, Slot: 2}}
	chain := SuccessionChain{{ID: 3, Addr: "a"}, {ID: 2, Addr: "b"}}

	// An empty chain is "nobody was ever confirmed", not "nobody is eligible".
	if got, ok := DesignatedSuccessor(roster, 1, nil); !ok || got != 2 {
		t.Fatalf("successor with no chain = %d (ok=%t), want the roster rule's 2", got, ok)
	}
	// Confirmation order, not identity order: 3 was confirmed first.
	if got, ok := DesignatedSuccessor(roster, 1, chain); !ok || got != 3 {
		t.Fatalf("successor = %d (ok=%t), want the chain's first survivor 3", got, ok)
	}
	// A prefix agrees with its extension, which is why an appended chain needs no
	// agreement step.
	if got, _ := DesignatedSuccessor(roster, 1, chain[:1]); got != 3 {
		t.Fatalf("prefix elected %d, want the same 3", got)
	}
	// Losing the authority and the successor together still elects.
	if got, ok := DesignatedSuccessor(roster, 3, chain); !ok || got != 2 {
		t.Fatalf("successor to 3 = %d (ok=%t), want 2", got, ok)
	}
	// A chain naming only participants that are gone falls back to the roster.
	gone := SuccessionChain{{ID: 9, Addr: "x"}}
	if got, ok := DesignatedSuccessor(roster, 1, gone); !ok || got != 2 {
		t.Fatalf("stale chain elected %d (ok=%t), want the fallback 2", got, ok)
	}
	if _, ok := DesignatedSuccessor([]SessionParticipant{{ID: 1}}, 1, nil); ok {
		t.Fatal("a roster with no survivor designated one")
	}
}

// TestAnAddressIsNotRosterIdentity is why the chain is beside the roster and not
// in it: SameRoster compares SessionParticipant by value, so a rebound port would
// fail every handoff for the rest of the session.
func TestAnAddressIsNotRosterIdentity(t *testing.T) {
	roster := []SessionParticipant{{ID: 1, Slot: 0}, {ID: 2, Slot: 1}}
	rec := HandoffRecord{
		Term: FirstTerm + 1, Authority: 2, Predecessor: 1,
		Roster: roster, BarrierDelayTicks: 3,
		Chain: SuccessionChain{{ID: 2, Addr: "10.0.0.2:7777"}},
	}
	if err := rec.Validate(roster, rec.Chain); err != nil {
		t.Fatalf("a valid record was refused: %v", err)
	}
	rec.Chain = SuccessionChain{{ID: 2, Addr: "10.0.0.2:41235"}}
	if err := rec.Validate(roster, rec.Chain); err != nil {
		t.Fatalf("a record whose peer rebound its port was refused: %v", err)
	}
	rec.Roster = []SessionParticipant{{ID: 1, Slot: 1}, {ID: 2, Slot: 0}}
	if err := rec.Validate(roster, rec.Chain); err == nil {
		t.Fatal("a record carrying different slot assignments was accepted")
	}
}

func TestChainAppendKeepsPosition(t *testing.T) {
	c := SuccessionChain(nil).Append(3, "c").Append(2, "b").Append(3, "c2")
	if len(c) != 2 || c[0].ID != 3 || c[0].Addr != "c2" || c[1].ID != 2 {
		t.Fatalf("chain = %+v, want 3 first with its new address", c)
	}
	if c = c.Without(3); len(c) != 1 || c[0].ID != 2 {
		t.Fatalf("chain after a departure = %+v", c)
	}
	if _, ok := c.Lookup(3); ok {
		t.Fatal("a departed participant survived Without")
	}
}

// TestPeerLinkAdmitsOnlyAParticipant covers the handshake two participants use on
// each other: it names the far end and refuses anyone the gate does not know.
func TestPeerLinkAdmitsOnlyAParticipant(t *testing.T) {
	// The gate holds the whole identity and the dial carries only the build half,
	// which is what a live session does: verifying more than the build refused
	// every peer link there was.
	accept := PeerAcceptor(PeerGate{
		Local:    2,
		Identity: PeerIdentity{Protocol: ProtocolVersion, Seed: 42, Session: 7},
		Admit: func(from PeerID) error {
			if from != 3 {
				return errors.New("not in this session")
			}
			return nil
		},
	}, time.Second)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	results := make(chan error, 2)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_, err = accept(conn)
			_ = conn.Close()
			results <- err
		}
	}()

	cfg := DebugConfig(RolePeer, "")
	cfg.ConnectTimeout = time.Second
	cfg.Identity = PeerIdentity{Protocol: ProtocolVersion}

	conn, from, err := DialPeerLink(ln.Addr().String(), cfg, 3)
	if err != nil || from != 2 {
		t.Fatalf("peer link = (%d, %v), want participant 2", from, err)
	}
	_ = conn.Close()
	if err := <-results; err != nil {
		t.Fatalf("the acceptor refused a participant it knows: %v", err)
	}

	if _, _, err = DialPeerLink(ln.Addr().String(), cfg, 4); err == nil {
		t.Fatal("a participant the gate does not know opened a link")
	}
	<-results
}

// TestBindPeerFallsBackWhenThePortIsTaken is the collision two guests on one
// machine hit.
func TestBindPeerFallsBackWhenThePortIsTaken(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer held.Close()

	ln, err := BindPeer(held.Addr().String(), DebugConfig(RolePeer, ""))
	if err != nil {
		t.Fatalf("bind fell over instead of falling back: %v", err)
	}
	defer ln.Close()
	if ln.Addr().String() == held.Addr().String() {
		t.Fatal("two listeners bound the same address")
	}
}

// TestAPeerDialBecomesALink proves a participant that bound a port of its own is
// dialled by another and the stream becomes an ordinary peer.
func TestAPeerDialBecomesALink(t *testing.T) {
	ln, err := BindPeer("127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	listenCfg := DebugConfig(RolePeer, "")
	listenCfg.ParticipantID = 2
	listenCfg.PreboundListener = ln
	listenCfg.AcceptPeer = PeerAcceptor(PeerGate{
		Local: 2,
		Admit: func(from PeerID) error {
			if from != 3 {
				return errors.New("not in this session")
			}
			return nil
		},
	}, time.Second)
	listener := NewSocketPort(listenCfg)
	if err := listener.Start(); err != nil {
		t.Fatalf("listener start: %v", err)
	}
	defer listener.Close()

	dialLn, err := BindPeer("127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	dialCfg := DebugConfig(RolePeer, "")
	dialCfg.ParticipantID = 3
	dialCfg.PreboundListener = dialLn
	dialCfg.AcceptPeer = PeerAcceptor(PeerGate{Local: 3}, time.Second)
	dialer := NewSocketPort(dialCfg)
	if err := dialer.Start(); err != nil {
		t.Fatalf("dialer start: %v", err)
	}
	defer dialer.Close()

	if err := dialer.DialPeer(ln.Addr().String()); err != nil {
		t.Fatalf("peer dial: %v", err)
	}
	if !dialer.Connected(2) {
		t.Fatal("the dialer does not hold the participant that answered")
	}
	waitForPeer(t, func() bool { return listener.Connected(3) },
		"the listener never admitted the participant that dialled it")
	if !dialer.Send(2, uint8(MsgHeartbeat), nil) {
		t.Fatal("the new link refused a frame")
	}
}

func waitForPeer(t *testing.T, done func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(msg)
}
