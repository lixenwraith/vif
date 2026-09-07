package network

import (
	"errors"
	"net"
	"testing"
	"time"
)

// TestAnAddressIsNotRosterIdentity is the reason the two tables are separate.
//
// SameRoster compares SessionParticipant by value and HandoffRecord.Validate
// refuses a roster that is not byte-identical to the one the session closed on. An
// address field on that struct would make a guest that rebound its port — an
// ephemeral port is different every time — look like a different roster and fail
// every handoff for the rest of the session.
func TestAnAddressIsNotRosterIdentity(t *testing.T) {
	roster := []SessionParticipant{{ID: 1, Slot: 0}, {ID: 2, Slot: 1}, {ID: 3, Slot: 2}}
	rec := HandoffRecord{
		Term: FirstTerm + 1, Authority: 2, Predecessor: 1,
		Roster: roster, BarrierDelayTicks: 3,
		Addresses: PeerAddresses{{ID: 2, Addr: "10.0.0.2:7777"}},
	}
	if err := rec.Validate(roster, nil); err != nil {
		t.Fatalf("a valid record was refused: %v", err)
	}
	rec.Addresses = PeerAddresses{{ID: 2, Addr: "10.0.0.2:41235"}}
	if err := rec.Validate(roster, nil); err != nil {
		t.Fatalf("a record whose peer rebound its port was refused: %v", err)
	}
	// The roster itself is still exactly as strict as it was.
	rec.Roster = []SessionParticipant{{ID: 1, Slot: 0}, {ID: 2, Slot: 2}, {ID: 3, Slot: 1}}
	if err := rec.Validate(roster, nil); err == nil {
		t.Fatal("a record carrying different slot assignments was accepted")
	}
}

// TestTheSuccessorMustBeOneTheSessionConfirmed is the eligibility half of the map.
//
// A guest behind NAT, behind a firewall, or that chose not to advertise takes the
// term under the roster rule alone and then authors alone, because nobody can dial
// it. With the confirmed set it plays normally and is skipped.
func TestTheSuccessorMustBeOneTheSessionConfirmed(t *testing.T) {
	roster := []SessionParticipant{{ID: 1, Slot: 0}, {ID: 2, Slot: 1}, {ID: 3, Slot: 2}}

	// Nothing confirmed is "nothing was ever confirmed" rather than "nobody is
	// eligible": a session of leaves, or a build that predates the map, gets the
	// rule it always had.
	if got, ok := DesignatedSuccessor(roster, 1, nil); !ok || got != 2 {
		t.Fatalf("successor with no confirmed set = %d (ok=%t), want the roster rule's 2", got, ok)
	}
	// The lowest survivor is a leaf, so the next one takes it.
	if got, ok := DesignatedSuccessor(roster, 1, []PeerID{3}); !ok || got != 3 {
		t.Fatalf("successor over an unconfirmed 2 = %d (ok=%t), want 3", got, ok)
	}
	// Confirmed and lowest is still the answer.
	if got, ok := DesignatedSuccessor(roster, 1, []PeerID{2, 3}); !ok || got != 2 {
		t.Fatalf("successor over a confirmed roster = %d (ok=%t), want 2", got, ok)
	}
	// A confirmed set naming only participants that are gone falls back rather than
	// electing nobody: the alternative is a session that could have continued and
	// did not because a table was stale.
	if got, ok := DesignatedSuccessor(roster, 1, []PeerID{1}); !ok || got != 2 {
		t.Fatalf("successor with a stale confirmed set = %d (ok=%t), want the fallback 2", got, ok)
	}

	// And the receiver checks it for itself, which is the half of the split-brain
	// rule a receiver can make.
	rec := HandoffRecord{
		Term: FirstTerm + 1, Authority: 2, Predecessor: 1,
		Roster: roster, BarrierDelayTicks: 3,
	}
	if err := rec.Validate(roster, []PeerID{3}); err == nil {
		t.Fatal("a record naming a participant this instance's confirmed set skips was accepted")
	}
	rec.Authority = 3
	if err := rec.Validate(roster, []PeerID{3}); err != nil {
		t.Fatalf("the confirmed successor's own record was refused: %v", err)
	}
}

func TestAddressMapNormalizesToOneValue(t *testing.T) {
	m := PeerAddresses{{ID: 3, Addr: "c:1"}, {ID: 1, Addr: "a:1"}, {ID: 0, Addr: "x:1"}, {ID: 2, Addr: ""}}
	got := m.Normalized()
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 3 {
		t.Fatalf("normalized map = %+v, want identities 1 and 3 in order", got)
	}
	got = got.With(2, "b:1")
	if addr, ok := got.Lookup(2); !ok || addr != "b:1" || got[1].ID != 2 {
		t.Fatalf("map after With = %+v", got)
	}
	got = got.Without(1)
	if _, ok := got.Lookup(1); ok {
		t.Fatalf("a departed participant survived Without: %+v", got)
	}
}

// TestPeerLinkAdmitsAParticipantAndAnswersAProbe covers both directions of the
// handshake two participants use on each other, which is deliberately not the
// coordinator's: nothing is allocated, offered or captured, because both ends
// already hold identities the coordinator assigned.
func TestPeerLinkAdmitsAParticipantAndAnswersAProbe(t *testing.T) {
	var admitted []PeerID
	accept := PeerAcceptor(PeerGate{
		Local: 2,
		Admit: func(from PeerID, term AuthorityTerm) error {
			if from != 3 {
				return errors.New("not in this session")
			}
			admitted = append(admitted, from)
			return nil
		},
	}, time.Second)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	results := make(chan error, 3)
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

	// The bind confirmation: a completed round trip that must not become a link,
	// because the authority making it is already connected to the far end.
	conn, from, err := DialPeerLink(ln.Addr().String(), cfg, 1, FirstTerm, true)
	if err != nil || conn != nil || from != 2 {
		t.Fatalf("probe = (%v, %d, %v), want no connection from participant 2", conn, from, err)
	}
	if err := <-results; !errors.Is(err, errPeerProbe) {
		t.Fatalf("the acceptor reported %v for a probe, want it recognised as one", err)
	}

	// A real link from a participant the gate knows.
	conn, from, err = DialPeerLink(ln.Addr().String(), cfg, 3, FirstTerm, false)
	if err != nil || from != 2 {
		t.Fatalf("peer link = (%d, %v), want participant 2", from, err)
	}
	_ = conn.Close()
	if err := <-results; err != nil {
		t.Fatalf("the acceptor refused a participant it knows: %v", err)
	}
	if len(admitted) != 1 || admitted[0] != 3 {
		t.Fatalf("admitted = %v, want the one caller the gate knows", admitted)
	}

	// And one it does not, refused with a reason rather than a closed stream.
	if _, _, err = DialPeerLink(ln.Addr().String(), cfg, 4, FirstTerm, false); err == nil {
		t.Fatal("a participant the gate does not know opened a link")
	}
	<-results
}

// TestBindPeerFallsBackWhenThePortIsTaken is the collision case: two guests on one
// machine, or a guest on the host's own machine, cannot both bind the default.
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

// TestAPeerDialBecomesALink is the mesh half at transport level: a participant
// that bound a port of its own is dialled by another and the stream becomes an
// ordinary peer, carrying session frames in both directions.
//
// It is what makes `-authority migrate` mean anything. Before it a successor
// authored and nobody could reach it, because the only listener in a session was
// the coordinator's and the only handshake was the join.
func TestAPeerDialBecomesALink(t *testing.T) {
	ln, err := BindPeer("127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	// The participant that listens: a guest, whose port serves peer links and
	// never the coordinator's join handshake.
	listenCfg := DebugConfig(RolePeer, "")
	listenCfg.ParticipantID = 2
	listenCfg.PreboundListener = ln
	listenCfg.AcceptPeer = PeerAcceptor(PeerGate{
		Local: 2,
		Admit: func(from PeerID, _ AuthorityTerm) error {
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

	// The participant that dials. It has a port of its own too — every guest in a
	// migrate session does — which is also what makes it a legal transport with
	// nothing to dial at startup: the link this test is about is opened afterwards,
	// from the address map.
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

	if err := dialer.DialPeer(ln.Addr().String(), FirstTerm); err != nil {
		t.Fatalf("peer dial: %v", err)
	}
	if got := dialer.PeerCount(); got != 1 {
		t.Fatalf("dialer holds %d peers, want the one it opened", got)
	}
	if !dialer.Connected(2) {
		t.Fatal("the dialer does not hold the participant that answered")
	}
	waitForPeer(t, func() bool { return listener.Connected(3) },
		"the listener never admitted the participant that dialled it")

	// And it is an ordinary link: what travels on it is session traffic, not a
	// second handshake.
	if !dialer.Send(2, uint8(MsgHeartbeat), nil) {
		t.Fatal("the new link refused a frame")
	}
}

// waitForPeer polls a condition an accept goroutine satisfies.
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
