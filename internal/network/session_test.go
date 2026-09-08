package network

import (
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

func testOffer() SessionOffer {
	return SessionOffer{
		Anchor: event.JoinAnchor{Anchor: event.JournalAnchor{Schema: event.JournalSchema, Seed: 7}},
		Host:   1, Assigned: 2, Term: FirstTerm, BarrierDelayTicks: 3,
		Participants: []SessionParticipant{{ID: 1, Slot: 0}, {ID: 2, Slot: 1}},
	}
}

func TestSilentPeerBecomesDisconnectNotification(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DisconnectTimeout = 20 * time.Millisecond
	cfg.HeartbeatInterval = 0
	pm := NewPeerManager(cfg)
	disconnected := make(chan PeerID, 1)
	pm.SetHandlers(nil, func(id PeerID) { disconnected <- id }, nil)
	local, remote := net.Pipe()
	defer remote.Close()
	defer pm.Close()
	if _, err := pm.AddConnectionAs(local, 2); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-disconnected:
		if id != 2 {
			t.Fatalf("disconnected peer = %d, want 2", id)
		}
	case <-time.After(time.Second):
		t.Fatal("silent peer did not time out")
	}
}

func TestSessionRejectionReturnsTheJoinErrorUnchanged(t *testing.T) {
	offer := testOffer()
	hostCfg := DebugConfig(RoleHost, "127.0.0.1:0")
	hostCfg.ParticipantID = offer.Host
	hostCfg.AcceptSession = HostAcceptor(Coordinator{Assign: func() (SessionOffer, error) { return offer, nil }}, time.Second)
	host := NewSocketPort(hostCfg)
	defer host.Close()
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	pending, _, err := DialSession(host.Addr().String(), DebugConfig(RolePeer, ""))
	if err != nil {
		t.Fatal(err)
	}

	want := errors.New("join mismatch: seed recorded 7, this run has 8")
	if got := pending.Complete(want, JoinerReport{}); got != want {
		t.Fatalf("Complete(rejection) = %v, want original error %v", got, want)
	}
	select {
	case got := <-host.Errors():
		if got.Error() != want.Error() {
			t.Fatalf("host rejection = %v, want %v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("host did not receive join rejection")
	}
	if host.PeerCount() != 0 || !host.IsRunning() {
		t.Fatalf("rejected host state = peers %d running %t", host.PeerCount(), host.IsRunning())
	}
}

func TestSocketSessionHandshakeAndDisconnect(t *testing.T) {
	offer := testOffer()
	hostCfg := DebugConfig(RoleHost, "127.0.0.1:0")
	hostCfg.ParticipantID = offer.Host
	hostCfg.AcceptSession = HostAcceptor(Coordinator{Assign: func() (SessionOffer, error) { return offer, nil }}, time.Second)
	host := NewSocketPort(hostCfg)
	defer host.Close()
	if err := host.Start(); err != nil {
		t.Fatalf("host start: %v", err)
	}

	pending, gotOffer, err := DialSession(host.Addr().String(), DebugConfig(RolePeer, ""))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer pending.Close()
	if gotOffer.Assigned != 2 || gotOffer.Host != 1 {
		t.Fatalf("offer assignment = host %d guest %d", gotOffer.Host, gotOffer.Assigned)
	}
	if err := pending.Complete(nil, JoinerReport{}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	waitFor(t, func() bool { return host.PeerCount() == 1 }, host.Changes(), "host peer")
	final, err := json.Marshal(offer)
	if err != nil {
		t.Fatal(err)
	}
	if !host.Send(2, uint8(MsgStart), final) {
		t.Fatal("host could not send start gate")
	}
	if got, err := pending.WaitStart(); err != nil {
		t.Fatalf("start gate: %v", err)
	} else if len(got.Participants) != len(offer.Participants) || got.Assigned != offer.Assigned {
		t.Fatalf("start roster = %#v, want the offered roster", got)
	}
	if err := pending.Ready(); err != nil {
		t.Fatalf("ready: %v", err)
	}
	waitFor(t, func() bool { return host.ReadyCount() == 1 }, host.Changes(), "guest ready")

	guest := NewSocketPort(pending.TransportConfig())
	if err := guest.Start(); err != nil {
		t.Fatalf("guest start: %v", err)
	}
	waitFor(t, func() bool { return guest.PeerCount() == 1 }, guest.Changes(), "guest peer")

	body := []byte("framed payload")
	if !guest.Send(1, uint8(MsgEvent), body) {
		t.Fatal("guest send failed")
	}
	waitInbound(t, host, func(in Inbound) bool {
		return in.Kind == InboundMessage && in.Peer == 2 && in.Msg != nil && string(in.Msg.Payload) == string(body)
	})

	if err := guest.Close(); err != nil {
		t.Fatalf("guest close: %v", err)
	}
	waitFor(t, func() bool { return host.PeerCount() == 0 }, host.Changes(), "host disconnect")
	if !host.IsRunning() {
		t.Fatal("peer disconnect stopped the host listener")
	}
	waitInbound(t, host, func(in Inbound) bool { return in.Kind == InboundDisconnect && in.Peer == 2 })
}

func waitFor(t *testing.T, ready func() bool, changes <-chan struct{}, what string) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for !ready() {
		select {
		case <-changes:
		case <-deadline.C:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func waitInbound(t *testing.T, p *SocketPort, match func(Inbound) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var buf [16]Inbound
	for time.Now().Before(deadline) {
		for _, in := range buf[:p.Drain(buf[:])] {
			if match(in) {
				return
			}
		}
		select {
		case <-p.Changes():
		case <-time.After(time.Millisecond):
		}
	}
	t.Fatal("timed out waiting for inbound notification")
}

func TestANamedSessionAdmitsOnlyTheDialerThatNamedIt(t *testing.T) {
	offer := testOffer()
	hostCfg := DebugConfig(RoleHost, "127.0.0.1:0")
	hostCfg.ParticipantID = offer.Host
	hostCfg.AcceptSession = HostAcceptor(Coordinator{
		Assign: func() (SessionOffer, error) { return offer, nil },
		Name:   "7f3c1a",
	}, time.Second)
	host := NewSocketPort(hostCfg)
	defer host.Close()
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}

	stale := DebugConfig(RolePeer, "")
	stale.SessionName = "1a3c7f"
	if _, _, err := DialSession(host.Addr().String(), stale); err == nil {
		t.Fatal("a dial naming another session was admitted")
	}

	named := DebugConfig(RolePeer, "")
	named.SessionName = "7f3c1a"
	pending, got, err := DialSession(host.Addr().String(), named)
	if err != nil {
		t.Fatalf("a dial naming this session was refused: %v", err)
	}
	defer pending.Close()
	if got.Assigned != offer.Assigned {
		t.Fatalf("offer assigned %d, want %d", got.Assigned, offer.Assigned)
	}
}

func admitFrom(host string, port int) net.Addr {
	return &net.TCPAddr{IP: net.ParseIP(host), Port: port}
}

// TestAdmissionSpendsABudgetPerDiallingHost covers the amplification this bounds:
// a join costs the session a whole world read and the dialer one connect, so a
// peer that cycles is free to it and expensive to everything else.
func TestAdmissionSpendsABudgetPerDiallingHost(t *testing.T) {
	t.Parallel()
	l := NewAdmissionLimiter()

	for i := range parameter.NetworkAdmitBurst {
		if err := l.Admit(admitFrom("10.0.0.1", 7777)); err != nil {
			t.Fatalf("admission %d of the burst was refused: %v", i+1, err)
		}
	}
	if err := l.Admit(admitFrom("10.0.0.1", 7777)); err == nil {
		t.Fatal("the budget did not run out")
	}
	if err := l.Admit(admitFrom("10.0.0.2", 7777)); err != nil {
		t.Fatalf("a second host was refused on the first host's budget: %v", err)
	}
}

// TestAdmissionIgnoresThePort pins the key: the port is what a dialer changes for
// free, so counting it would count nothing.
func TestAdmissionIgnoresThePort(t *testing.T) {
	t.Parallel()
	l := NewAdmissionLimiter()
	for i := range parameter.NetworkAdmitBurst {
		if err := l.Admit(admitFrom("10.0.0.3", 40000+i)); err != nil {
			t.Fatalf("admission %d was refused: %v", i+1, err)
		}
	}
	if err := l.Admit(admitFrom("10.0.0.3", 55555)); err == nil {
		t.Fatal("a new source port bought a fresh budget")
	}
}

// TestAdmissionRefillsWithItsWindow is the non-vacuous half: a budget that never
// refilled would turn one crash-and-reconnect into a permanent exclusion.
func TestAdmissionRefillsWithItsWindow(t *testing.T) {
	t.Parallel()
	l := NewAdmissionLimiter()
	l.window = time.Millisecond

	for range l.burst {
		if err := l.Admit(admitFrom("10.0.0.4", 7777)); err != nil {
			t.Fatalf("burst refused: %v", err)
		}
	}
	if err := l.Admit(admitFrom("10.0.0.4", 7777)); err == nil {
		t.Fatal("the budget did not run out")
	}
	time.Sleep(3 * time.Millisecond)
	if err := l.Admit(admitFrom("10.0.0.4", 7777)); err != nil {
		t.Fatalf("the window did not refill: %v", err)
	}
}

// TestAdmissionDoesNotGrowWithoutBound keeps the defence from becoming the leak:
// a spray of addresses costs one window's worth of keys rather than one per dial.
func TestAdmissionDoesNotGrowWithoutBound(t *testing.T) {
	t.Parallel()
	l := NewAdmissionLimiter()
	l.window = time.Millisecond

	for i := range l.max * 4 {
		_ = l.Admit(&net.TCPAddr{IP: net.IPv4(10, byte(i>>16), byte(i>>8), byte(i)), Port: 1})
		if i%256 == 0 {
			time.Sleep(2 * time.Millisecond) // let the window pass so the sweep has work
		}
	}
	l.mu.Lock()
	tracked := len(l.seen)
	l.mu.Unlock()
	if tracked > l.max {
		t.Fatalf("the limiter tracks %d hosts, ceiling is %d", tracked, l.max)
	}
}
