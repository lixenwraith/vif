package network

import (
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/event"
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
