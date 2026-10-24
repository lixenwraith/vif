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

// TestABotIsNotRosterIdentityAcrossAHandoff: a seat crosses at its holder's word,
// which a receiver behind its relay may hold unapplied when the record reaches it,
// so a record is validated against the players alone.
func TestABotIsNotRosterIdentityAcrossAHandoff(t *testing.T) {
	players := []RosterEntry{{ID: 1, Slot: 0}, {ID: 2, Slot: 1}}
	rec := HandoffRecord{
		Term: FirstTerm + 1, Authority: 2, Predecessor: 1,
		Roster: append(slices.Clone(players), RosterEntry{ID: 3, Slot: 2, Holder: 2}),
		Chain:  SuccessionChain{{ID: 2, Addr: "a"}},
	}
	if err := rec.Validate(players, rec.Chain, FirstTerm); err != nil {
		t.Fatalf("a record carrying a seat the receiver has not spawned was refused: %v", err)
	}
	rec.Roster = append(slices.Clone(players), RosterEntry{ID: 4, Slot: 2})
	if err := rec.Validate(players, rec.Chain, FirstTerm); err == nil {
		t.Fatal("a record carrying a player the receiver does not hold was accepted")
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
		Local:    func() PeerID { return 2 },
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

	ln, err := BindPeer(held.Addr().String())
	if err != nil {
		t.Fatalf("bind fell over instead of falling back: %v", err)
	}
	defer ln.Close()
	if ln.Addr().String() == held.Addr().String() {
		t.Fatal("two listeners bound the same address")
	}
}

// TestAParticipantsPortTakesPeerLinksAndJoins: a participant that bound a port of
// its own is dialled by another and the stream becomes an ordinary peer, and a join
// at that port runs the session's handshake, each told by the frame it opens with.
// Only the join is served and reaches the join gate, and one that named another
// session is refused as such. The port names the addresses it answers at.
func TestAParticipantsPortTakesPeerLinksAndJoins(t *testing.T) {
	offer := testOffer()
	offer.Host, offer.Assigned = 2, 4
	offer.Roster = []RosterEntry{{ID: 2, Slot: 0}, {ID: 3, Slot: 1}, {ID: 4, Slot: 2}}
	admitted := make(chan PeerID, 4)
	port := func(bind string, local PeerID, gate func(PeerID) error) *SocketPort {
		ln, err := BindPeer(bind)
		if err != nil {
			t.Fatalf("bind: %v", err)
		}
		cfg := DebugConfig(RolePeer, "")
		cfg.ParticipantID = local
		cfg.PreboundListener = ln
		cfg.AcceptPeer = PeerAcceptor(PeerGate{Local: func() PeerID { return local }, Admit: gate}, time.Second)
		cfg.AcceptSession = HostAcceptor(Coordinator{
			Assign: func([]string) (SessionOffer, error) { return offer, nil },
		}, time.Second)
		cfg.OnAdmit = func(id PeerID) { admitted <- id }
		p := NewSocketPort(cfg)
		if err := p.Start(); err != nil {
			t.Fatalf("start: %v", err)
		}
		t.Cleanup(func() { _ = p.Close() })
		return p
	}
	listener := port("127.0.0.1:0", 2, func(from PeerID) error {
		if from != 3 {
			return errors.New("not in this session")
		}
		return nil
	})
	dialer := port("127.0.0.1:0", 3, nil)

	if err := dialer.DialPeer(listener.ListenAddr()); err != nil {
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

	pending, got, err := DialSession(listener.ListenAddr(), DebugConfig(RolePeer, ""))
	if err != nil || got.Assigned != 4 {
		t.Fatalf("a join at the port = (%+v, %v), want the offer to participant 4", got, err)
	}
	defer pending.Close()
	if err := pending.Complete(nil, JoinerReport{}); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-admitted:
		if id != 4 {
			t.Fatalf("the join gate ran for participant %d, want the joiner", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the join never reached the join gate")
	}
	in := make([]Inbound, 8)
	served := map[PeerID]bool{}
	for _, e := range in[:listener.Drain(in)] {
		if e.Kind == InboundConnect {
			served[e.Peer] = e.Served
		}
	}
	if s, ok := served[3]; !ok || s {
		t.Fatalf("the peer link connected as %v (present %v), want unserved", s, ok)
	}
	if !served[4] {
		t.Fatal("the join did not connect as served")
	}

	named := DebugConfig(RolePeer, "")
	named.SessionName = "7f3c1a"
	if _, _, err := DialSession(listener.ListenAddr(), named); !errors.Is(err, &Refusal{Code: RefusalUnknown}) {
		t.Fatalf("a join naming a session this port does not serve: %v", err)
	}
	select {
	case id := <-admitted:
		t.Fatalf("the join gate ran for participant %d past the one join", id)
	default:
	}

	_, own, _ := net.SplitHostPort(listener.ListenAddr())
	wild := port(":0", 5, nil)
	_, every, _ := net.SplitHostPort(wild.ListenAddr())
	for _, c := range []struct {
		port *SocketPort
		addr string
		want bool
	}{
		{listener, ":" + own, true},
		{listener, "tcp://127.0.0.1:" + own, true},
		{listener, "0.0.0.0:" + own, true},
		{listener, "127.0.0.2:" + own, false},
		{listener, "ws://127.0.0.1:" + own, false},
		{listener, "127.0.0.1:" + every, false},
		{wild, "127.0.0.2:" + every, true},
	} {
		if got := c.port.ListensOn(c.addr); got != c.want {
			t.Errorf("a port bound at %s answers at %s: %v; want %v", c.port.ListenAddr(), c.addr, got, c.want)
		}
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
