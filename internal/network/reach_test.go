package network

import (
	"errors"
	"net"
	"slices"
	"testing"
	"time"
)

// TestSuccessionReadsTheChain covers the list survivors walk and the property that
// makes it safe without agreement: a chain and any prefix of it name the same first
// candidates. Nobody away, held or without a port is one: nobody could dial it.
func TestSuccessionReadsTheChain(t *testing.T) {
	roster := []RosterEntry{{ID: 1, Slot: 0}, {ID: 2, Slot: 1}, {ID: 3, Slot: 2},
		{ID: 4, Slot: 3, Away: true}, {ID: 5, Slot: 4, Holder: 3}, {ID: 6, Slot: 5}}
	chain := SuccessionChain{{ID: 3, Addr: "a"}, {ID: 4, Addr: "d"}, {ID: 5, Addr: "e"}, {ID: 2, Addr: "b"}, {ID: 9, Addr: "x"}}

	for _, c := range []struct {
		lost  PeerID
		chain SuccessionChain
		want  []PeerID
	}{
		{1, chain, []PeerID{3, 2}},  // confirmation order, not identity order
		{1, chain[:1], []PeerID{3}}, // a prefix agrees with its extension
		{3, chain, []PeerID{2}},     // losing a candidate with the authority leaves the rest
		{1, nil, nil},               // no port, no candidate
	} {
		if got := SuccessionCandidates(roster, c.lost, c.chain); !slices.Equal(got, c.want) {
			t.Fatalf("candidates after %d's loss with chain %v = %v, want %v", c.lost, c.chain, got, c.want)
		}
	}
}

// TestEachCandidateTakesItsOwnTerm: the k-th candidate's record names the term k
// past the one its receiver holds, so two hosts' records never tie, and one for a
// term no candidate takes or naming another candidate is refused.
func TestEachCandidateTakesItsOwnTerm(t *testing.T) {
	roster := []RosterEntry{{ID: 1, Slot: 0}, {ID: 2, Slot: 1}, {ID: 3, Slot: 2}}
	chain := SuccessionChain{{ID: 3, Addr: "a"}, {ID: 2, Addr: "b"}}
	rec := func(term AuthorityTerm, authority PeerID) HandoffRecord {
		return HandoffRecord{Term: term, Authority: authority, Predecessor: 1, Roster: roster}
	}
	for _, ok := range []HandoffRecord{rec(FirstTerm+1, 3), rec(FirstTerm+2, 2)} {
		if err := ok.Validate(roster, chain, FirstTerm); err != nil {
			t.Fatalf("candidate %d's record for term %d was refused: %v", ok.Authority, ok.Term, err)
		}
	}
	for _, bad := range []HandoffRecord{rec(FirstTerm+1, 2), rec(FirstTerm+3, 2), rec(FirstTerm, 3)} {
		if bad.Validate(roster, chain, FirstTerm) == nil {
			t.Fatalf("a record naming %d for term %d was accepted", bad.Authority, bad.Term)
		}
	}
}

// TestAnAddressIsNotRosterIdentity is why the chain is beside the roster and not
// in it: SameRoster compares RosterEntry by value, so a rebound port would
// fail every handoff for the rest of the session.
func TestAnAddressIsNotRosterIdentity(t *testing.T) {
	roster := []RosterEntry{{ID: 1, Slot: 0}, {ID: 2, Slot: 1}}
	rec := HandoffRecord{
		Term: FirstTerm + 1, Authority: 2, Predecessor: 1,
		Roster: roster,
		Chain:  SuccessionChain{{ID: 2, Addr: "10.0.0.2:7777"}},
	}
	if err := rec.Validate(roster, rec.Chain, FirstTerm); err != nil {
		t.Fatalf("a valid record was refused: %v", err)
	}
	rec.Chain = SuccessionChain{{ID: 2, Addr: "10.0.0.2:41235"}}
	if err := rec.Validate(roster, rec.Chain, FirstTerm); err != nil {
		t.Fatalf("a record whose peer rebound its port was refused: %v", err)
	}
	rec.Roster = []RosterEntry{{ID: 1, Slot: 1}, {ID: 2, Slot: 0}}
	if err := rec.Validate(roster, rec.Chain, FirstTerm); err == nil {
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
	if !dialer.Send(2, uint8(MsgEvent), nil) {
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
