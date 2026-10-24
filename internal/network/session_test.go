package network

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

func testOffer() SessionOffer {
	return SessionOffer{
		Anchor: event.JoinAnchor{Anchor: event.JournalAnchor{Schema: event.JournalSchema, Seed: 7}},
		Host:   1, Assigned: 2, Term: FirstTerm,
		Roster: []RosterEntry{{ID: 1, Slot: 0}, {ID: 2, Slot: 1}},
	}
}

func TestSilentPeerBecomesDisconnectNotification(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ReadTimeout = 20 * time.Millisecond
	pm := NewPeerManager(cfg)
	disconnected := make(chan PeerID, 1)
	pm.SetHandlers(nil, func(id PeerID) { disconnected <- id }, nil)
	local, remote := net.Pipe()
	defer remote.Close()
	defer pm.Close()
	if _, err := pm.AddConnectionAs(local, 2, true); err != nil {
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

// TestProbesKeepAQuietLinkAlive: a lobby and then a session carry nothing but
// probes for longer than the read timeout, and neither end calls the other lost.
func TestProbesKeepAQuietLinkAlive(t *testing.T) {
	quiet := 5 * parameter.NetworkProbeInterval
	offer := testOffer()
	hostCfg := DebugConfig(RoleHost, "127.0.0.1:0")
	hostCfg.ParticipantID = offer.Host
	hostCfg.ReadTimeout = quiet
	hostCfg.AcceptSession = HostAcceptor(Coordinator{Assign: func([]string) (SessionOffer, error) { return offer, nil }}, time.Second)
	host := NewSocketPort(hostCfg)
	defer host.Close()
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	joinCfg := DebugConfig(RolePeer, "")
	joinCfg.ReadTimeout = quiet
	pending, _, err := DialSession(host.Addr().String(), joinCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Close()
	guestCfg := pending.TransportConfig()
	if err := pending.Complete(nil, JoinerReport{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return host.PeerCount() == 1 }, host.Changes(), "host peer")

	start, err := json.Marshal(offer)
	if err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(2*quiet, func() { host.Send(2, uint8(MsgStart), start) })
	if _, err := pending.WaitStart(); err != nil {
		t.Fatalf("a lobby longer than the read timeout ended: %v", err)
	}
	if err := pending.Ready(); err != nil {
		t.Fatal(err)
	}
	guest := NewSocketPort(guestCfg)
	defer guest.Close()
	if err := guest.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return guest.PeerCount() == 1 }, guest.Changes(), "guest peer")

	time.Sleep(2 * quiet)
	if !host.Connected(2) || !guest.Connected(1) {
		t.Fatalf("a quiet session dropped its link: the host holds the guest %t, the guest holds the host %t",
			host.Connected(2), guest.Connected(1))
	}
}

// TestTheCoordinatorAssignsWithTheClaimsADialNames: a place is assigned knowing
// exactly the tokens the dialer keeps, and a dialer naming one of another form is
// assigned nothing.
func TestTheCoordinatorAssignsWithTheClaimsADialNames(t *testing.T) {
	offer := testOffer()
	assigned := make(chan []string, 1)
	accept := HostAcceptor(Coordinator{Assign: func(claims []string) (SessionOffer, error) {
		assigned <- claims
		return offer, nil
	}}, time.Second)
	hostCfg := DebugConfig(RoleHost, "127.0.0.1:0")
	hostCfg.ParticipantID = offer.Host
	hostCfg.AcceptSession = accept
	host := NewSocketPort(hostCfg)
	defer host.Close()
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	for _, kept := range [][]string{nil, {NewClaimToken(), NewClaimToken()}} {
		cfg := DebugConfig(RolePeer, "")
		cfg.Claims = kept
		pending, _, err := DialSession(host.Addr().String(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		_ = pending.Close()
		if got := <-assigned; !slices.Equal(got, kept) {
			t.Fatalf("a dial keeping %q was assigned knowing %q", kept, got)
		}
	}

	conn := &scripted{in: bytes.NewReader(frames(MsgJoinClaims, []string{ClaimDigest("x")[:10]}))}
	if _, err := accept(conn); err == nil || len(assigned) != 0 {
		t.Fatalf("a dial naming a claim of another form: %v, assigned %d", err, len(assigned))
	}
}

func TestSessionRejectionReturnsTheJoinErrorUnchanged(t *testing.T) {
	offer := testOffer()
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

// TestARefusalCrossesTheWireAsItsKind: the reader acts on the kind it reads and
// adds that kind's advice once; a kind a later build added reads as its reason.
func TestARefusalCrossesTheWireAsItsKind(t *testing.T) {
	read := func(body []byte) error {
		var reply sessionReply
		if err := json.Unmarshal(body, &reply); err != nil {
			t.Fatal(err)
		}
		return reply.Refused()
	}
	starting := &Refusal{Code: RefusalStarting, Reason: "session is starting"}
	var sent sessionReply
	sent.Refuse(fmt.Errorf("refused locally: %w", starting))
	body, err := json.Marshal(sent)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(body); !errors.Is(got, starting) || got.Error() != starting.Error() || RetryOf(got) != RetrySoon {
		t.Fatalf("refusal read back as %q (retry %d), want %q", got, RetryOf(got), starting)
	}
	if got := read([]byte(`{"code":200,"error":"a kind from a later build"}`)); got.Error() != "a kind from a later build" || RetryOf(got) != RetryLater {
		t.Fatalf("unknown kind read back as %q (retry %d)", got, RetryOf(got))
	}
}

// TestAFailedBindOrDialSaysWhatThePlayerCanDo: the common failures read as the
// player's next step rather than the system's words, and keep their cause.
func TestAFailedBindOrDialSaysWhatThePlayerCanDo(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	host := NewSocketPort(DebugConfig(RoleHost, busy.Addr().String()))
	defer host.Close()
	if err := host.Start(); err == nil || !strings.Contains(err.Error(), "choose another") || !errors.Is(err, syscall.EADDRINUSE) {
		t.Fatalf("binding a port in use: %v", err)
	}
	// Allowed where the system lets this user bind it; then there is nothing to say
	low := NewSocketPort(DebugConfig(RoleHost, "127.0.0.1:1"))
	defer low.Close()
	if err := low.Start(); err != nil && !strings.Contains(err.Error(), "below 1024") {
		t.Fatalf("binding a privileged port: %v", err)
	}

	gone, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := gone.Addr().String()
	gone.Close()
	if _, err := dial(addr, DefaultConfig()); err == nil || !strings.Contains(err.Error(), "nothing is listening") {
		t.Fatalf("dialling a closed port: %v", err)
	}
}

func TestSocketSessionHandshakeAndDisconnect(t *testing.T) {
	offer := testOffer()
	hostCfg := DebugConfig(RoleHost, "127.0.0.1:0")
	hostCfg.ParticipantID = offer.Host
	hostCfg.AcceptSession = HostAcceptor(Coordinator{Assign: func([]string) (SessionOffer, error) { return offer, nil }}, time.Second)
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
	guestCfg := pending.TransportConfig() // before the stream opens, as the app takes it
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
	} else if len(got.Roster) != len(offer.Roster) || got.Assigned != offer.Assigned {
		t.Fatalf("start roster = %#v, want the offered roster", got)
	}
	if err := pending.Ready(); err != nil {
		t.Fatalf("ready: %v", err)
	}
	waitFor(t, func() bool { return host.Confirmed(2) }, host.Changes(), "guest ready")

	guest := NewSocketPort(guestCfg)
	if err := guest.Start(); err != nil {
		t.Fatalf("guest start: %v", err)
	}
	waitFor(t, func() bool { return guest.PeerCount() == 1 }, guest.Changes(), "guest peer")

	// The gate again compresses against bytes only the join's decoder has seen, so
	// the port has to read on through that decoder.
	if !host.Send(2, uint8(MsgEvent), final) {
		t.Fatal("host send failed")
	}
	waitInbound(t, guest, func(in Inbound) bool {
		return in.Kind == InboundMessage && in.Peer == 1 && in.Msg != nil && string(in.Msg.Payload) == string(final)
	})

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

// pressHost serves offer through a coordinator that previews it, counting the
// places it assigns and gives back, with window as its ready period.
func pressHost(t *testing.T, offer SessionOffer, window time.Duration) (host *SocketPort, assigned, released *atomic.Int32) {
	t.Helper()
	preview := offer
	preview.Assigned = 0
	assigned, released = new(atomic.Int32), new(atomic.Int32)
	hostCfg := DebugConfig(RoleHost, "127.0.0.1:0")
	hostCfg.ParticipantID = offer.Host
	hostCfg.AcceptSession = HostAcceptor(Coordinator{
		Preview: func([]string) (SessionOffer, error) { return preview, nil },
		Assign: func([]string) (SessionOffer, error) {
			assigned.Add(1)
			return offer, nil
		},
		Release:     func(PeerID) { released.Add(1) },
		PressWindow: window,
	}, time.Second)
	host = NewSocketPort(hostCfg)
	t.Cleanup(func() { _ = host.Close() })
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	return host, assigned, released
}

// TestAPreparingDialHoldsNothingUntilItsSeat: a dial reads the session, prepares and
// waits for its press holding no place, and is assigned one at its seat, which hands
// a port built before it the stream and the identity; the order before the press
// assigned the place at the dial.
func TestAPreparingDialHoldsNothingUntilItsSeat(t *testing.T) {
	offer := testOffer()
	host, assigned, _ := pressHost(t, offer, 0)
	pending, preview, err := PreviewSession(host.Addr().String(), DebugConfig(RolePeer, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Close()
	unseated := pending.TransportConfig()
	if preview.Assigned != 0 || preview.Host != offer.Host || unseated.ParticipantID != 0 || unseated.preconnected != nil {
		t.Fatalf("a preview assigned %d, and a port built on it holds identity %d", preview.Assigned, unseated.ParticipantID)
	}
	if err := pending.Prepared(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond) // the host reads the prepared frame and waits on
	if n := assigned.Load(); n != 0 {
		t.Fatalf("%d place(s) assigned before the seat", n)
	}
	seated, err := pending.Seat()
	if err != nil {
		t.Fatal(err)
	}
	if seated.Assigned != offer.Assigned || assigned.Load() != 1 {
		t.Fatalf("seated as %d with %d assignment(s)", seated.Assigned, assigned.Load())
	}
	guest := NewSocketPort(unseated)
	defer guest.Close()
	if err := guest.TakeJoin(pending); err != nil {
		t.Fatal(err)
	}
	if guest.ParticipantID() != uint32(offer.Assigned) {
		t.Fatalf("the port took identity %d at the seat, want %d", guest.ParticipantID(), offer.Assigned)
	}
	if err := pending.Complete(nil, JoinerReport{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return host.PeerCount() == 1 }, host.Changes(), "host peer")
	final, err := json.Marshal(offer)
	if err != nil {
		t.Fatal(err)
	}
	if !host.Send(uint32(offer.Assigned), uint8(MsgStart), final) {
		t.Fatal("host could not send start gate")
	}
	if _, err := pending.WaitStart(); err != nil {
		t.Fatal(err)
	}
	if err := pending.Ready(); err != nil {
		t.Fatal(err)
	}
	if err := guest.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return guest.Connected(uint32(offer.Host)) }, guest.Changes(), "the port on the join's stream")
}

// TestAPressAfterTheReadyPeriodLeavesNothingHeld: a prepared dial nobody presses for
// is dropped when its ready period ends, with nothing assigned or given back, and a
// press after that is told so; one past the period as this end measures it does not
// try the stream at all.
func TestAPressAfterTheReadyPeriodLeavesNothingHeld(t *testing.T) {
	host, assigned, released := pressHost(t, testOffer(), 50*time.Millisecond)
	pending, _, err := PreviewSession(host.Addr().String(), DebugConfig(RolePeer, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Close()
	if err := pending.Prepared(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-host.Errors():
		if !errors.Is(err, &Refusal{Code: RefusalUnpressed}) {
			t.Fatalf("the host ended the handshake with %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the host held a handshake past its ready period")
	}
	if assigned.Load() != 0 || released.Load() != 0 || host.PeerCount() != 0 {
		t.Fatalf("a dial nobody pressed for: %d assigned, %d released, %d peer(s)", assigned.Load(), released.Load(), host.PeerCount())
	}
	// Told on the stream, or finding it closed: either way the press dials again.
	if _, err := pending.Seat(); err == nil || RetryOf(err) == RetryNever {
		t.Fatalf("a press after the ready period: %v", err)
	}

	late, _, err := PreviewSession(host.Addr().String(), DebugConfig(RolePeer, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer late.Close()
	if err := late.Prepared(); err != nil {
		t.Fatal(err)
	}
	late.prepared = time.Now().Add(-parameter.NetworkJoinPressWindow)
	if _, err := late.Seat(); !errors.Is(err, ErrPressLapsed) || assigned.Load() != 0 {
		t.Fatalf("a press this end saw lapse: %v, %d assigned", err, assigned.Load())
	}
}

// TestAPreviewRefusesAnotherBuildBeforeAnySeat: a build that cannot share the session
// learns it from the preview, before it builds a world, and the host assigns it
// nothing; the order before the press told it in the offer, a place already held.
func TestAPreviewRefusesAnotherBuildBeforeAnySeat(t *testing.T) {
	offer := testOffer()
	offer.Identity = sampleIdentity()
	offer.Identity.Protocol++
	host, assigned, _ := pressHost(t, offer, 0)
	cfg := DebugConfig(RolePeer, "")
	cfg.Identity = sampleIdentity()
	if _, _, err := PreviewSession(host.Addr().String(), cfg); !errors.Is(err, &Refusal{Code: RefusalIdentity}) {
		t.Fatalf("another build previewed the session: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if n := assigned.Load(); n != 0 {
		t.Fatalf("another build was assigned %d place(s)", n)
	}
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

// TestANamedSessionAdmitsOnlyTheDialerThatNamedIt joins a host's own ws://
// listener as the native client does: the name is the URL's last path segment,
// and the host answers on any path before it.
func TestANamedSessionAdmitsOnlyTheDialerThatNamedIt(t *testing.T) {
	offer := testOffer()
	hostCfg := DebugConfig(RoleHost, "ws://127.0.0.1:0")
	hostCfg.ParticipantID = offer.Host
	hostCfg.AcceptSession = HostAcceptor(Coordinator{
		Assign: func([]string) (SessionOffer, error) { return offer, nil },
		Name:   "7f3c1a",
	}, time.Second)
	host := NewSocketPort(hostCfg)
	defer host.Close()
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	join := func(name string) (*PendingJoin, SessionOffer, error) {
		route := host.Addr().String() + "/vif/ws/" + name
		e, err := ParseEndpoint(route)
		if err != nil {
			t.Fatal(err)
		}
		cfg := DebugConfig(RolePeer, "")
		cfg.SessionName = e.Name
		return DialSession(route, cfg)
	}

	if _, _, err := join("1a3c7f"); err == nil {
		t.Fatal("a dial naming another session was admitted")
	}
	pending, got, err := join("7f3c1a")
	if err != nil {
		t.Fatalf("a dial naming this session was refused: %v", err)
	}
	defer pending.Close()
	if got.Assigned != offer.Assigned {
		t.Fatalf("offer assigned %d, want %d", got.Assigned, offer.Assigned)
	}
}

// TestALinkAClientCannotTakeJoinsByTheRouteItsSiteLists: a link whose route never
// answers is looked up by its session's name on the site that issued it, whose list
// is the word on whether that session exists.
func TestALinkAClientCannotTakeJoinsByTheRouteItsSiteLists(t *testing.T) {
	offer := testOffer()
	hostCfg := DebugConfig(RoleHost, "127.0.0.1:0")
	hostCfg.ParticipantID = offer.Host
	hostCfg.AcceptSession = HostAcceptor(Coordinator{
		Assign: func([]string) (SessionOffer, error) { return offer, nil },
		Name:   "7f3c1a",
	}, time.Second)
	host := NewSocketPort(hostCfg)
	defer host.Close()
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/vif/api/sessions" {
			_, _ = fmt.Fprintf(w, `{"sessions":[{"id":"7f3c1a","join_target":"vif://%s/7f3c1a"}]}`, host.Addr())
			return
		}
		if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
			_ = conn.Close()
		}
	}))
	defer site.Close()
	join := func(name string) (*PendingJoin, SessionOffer, error) {
		cfg := DebugConfig(RolePeer, "")
		cfg.SessionName = name
		return DialSession("ws"+strings.TrimPrefix(site.URL, "http")+"/vif/ws/"+name, cfg)
	}

	pending, got, err := join("7f3c1a")
	if err != nil {
		t.Fatalf("a route that never answered did not join by the one its site lists: %v", err)
	}
	defer pending.Close()
	if got.Assigned != offer.Assigned {
		t.Fatalf("offer assigned %d, want %d", got.Assigned, offer.Assigned)
	}
	if _, _, err := join("1a3c7f"); err == nil || !strings.Contains(err.Error(), "lists no session 1a3c7f") {
		t.Fatalf("joining a session the site does not list: %v", err)
	}
}

// TestALinkIsLookedUpWhereItsSiteAnswers: a front door's site is its host's https
// origin, and a WebSocket route's is its own, then that host's.
func TestALinkIsLookedUpWhereItsSiteAnswers(t *testing.T) {
	for addr, want := range map[string][]string{
		"lixen.com:7777":            {"https://lixen.com"},
		"[::1]:7777":                {"https://[::1]"},
		"wss://lixen.com/vif/ws/a1": {"https://lixen.com"},
		"wss://lixen.com:7777/a1":   {"https://lixen.com:7777", "https://lixen.com"},
		"ws://127.0.0.1:8080/a1":    {"http://127.0.0.1:8080", "https://127.0.0.1"},
	} {
		if got := linkSites(addr); !slices.Equal(got, want) {
			t.Errorf("linkSites(%q) = %q, want %q", addr, got, want)
		}
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

// TestAdmissionIgnoresWhatADialerChangesFreely pins the key: the port, and an IPv6
// address's low half, cost a dialer nothing to change, so counting them would
// count nothing.
func TestAdmissionIgnoresWhatADialerChangesFreely(t *testing.T) {
	t.Parallel()
	for _, hosts := range [][2]string{{"10.0.0.3", "10.0.0.3"}, {"2001:db8:1:2::a", "2001:db8:1:2:ffff::b"}} {
		l := NewAdmissionLimiter()
		for i := range parameter.NetworkAdmitBurst {
			if err := l.Admit(admitFrom(hosts[0], 40000+i)); err != nil {
				t.Fatalf("admission %d from %s was refused: %v", i+1, hosts[0], err)
			}
		}
		if err := l.Admit(admitFrom(hosts[1], 55555)); err == nil {
			t.Fatalf("%s bought a fresh budget beside %s", hosts[1], hosts[0])
		}
	}
}

// TestAnInProcessStreamSpendsNoBudget: a holder opens one for each of its bots, and
// it has no dialler whose budget it could spend.
func TestAnInProcessStreamSpendsNoBudget(t *testing.T) {
	t.Parallel()
	l := NewAdmissionLimiter()
	near, far := Pipe()
	defer near.Close()
	defer far.Close()
	for i := range parameter.NetworkAdmitBurst + 1 {
		if err := l.Admit(near.RemoteAddr()); err != nil {
			t.Fatalf("in-process admission %d was refused: %v", i+1, err)
		}
	}
}

// TestAdmissionRefillsWithItsWindow is the non-vacuous half: a budget that never
// refilled would turn one crash-and-reconnect into a permanent exclusion.
func TestAdmissionRefillsWithItsWindow(t *testing.T) {
	t.Parallel()
	l := NewAdmissionLimiter()
	// Wide enough that a loaded race-detector run spends the burst inside one window
	l.window = 100 * time.Millisecond

	for range l.burst {
		if err := l.Admit(admitFrom("10.0.0.4", 7777)); err != nil {
			t.Fatalf("burst refused: %v", err)
		}
	}
	if err := l.Admit(admitFrom("10.0.0.4", 7777)); err == nil {
		t.Fatal("the budget did not run out")
	}
	time.Sleep(l.window + 10*time.Millisecond)
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

// TestAParticipantIsACursorNotARosterEntry pins what a count of participants counts.
// A dedicated coordinator holds a roster entry and no slot, so a session it serves to
// one guest has one participant however many entries the roster carries.
func TestAParticipantIsACursorNotARosterEntry(t *testing.T) {
	t.Parallel()

	served := SessionOffer{Roster: []RosterEntry{
		{ID: 1, Slot: parameter.NoPlayerSlot},
		{ID: 2, Slot: 0},
	}}
	if got := served.ParticipantCount(); got != 1 {
		t.Fatalf("a dedicated host serving one guest counted %d participants, want 1", got)
	}
	hosted := SessionOffer{Roster: []RosterEntry{{ID: 1, Slot: 0}, {ID: 2, Slot: 1}}}
	if got := hosted.ParticipantCount(); got != 2 {
		t.Fatalf("an interactive host and one guest counted %d participants, want 2", got)
	}
}

// TestAWssHostIsJoinedByADialerThatTrustsItsCertificate serves a session over wss
// from one binary: the door speaks TLS with the certificate it was given, a dialer
// that trusts none of the host's is turned away before the offer, and one that does
// is admitted as a ws dialer is. A wss door opens only with a certificate.
func TestAWssHostIsJoinedByADialerThatTrustsItsCertificate(t *testing.T) {
	certFile := filepath.Join(t.TempDir(), "host.pem")
	pool := writeSelfSigned(t, certFile)
	serverTLS, err := LoadTLS(certFile, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Listen("wss://127.0.0.1:0", DebugConfig(RoleHost, "")); err == nil {
		t.Fatal("a wss door opened with no certificate to serve")
	}

	offer := testOffer()
	hostCfg := DebugConfig(RoleHost, "wss://127.0.0.1:0")
	hostCfg.TLS = serverTLS
	hostCfg.ParticipantID = offer.Host
	hostCfg.AcceptSession = HostAcceptor(Coordinator{
		Assign: func([]string) (SessionOffer, error) { return offer, nil },
	}, time.Second)
	host := NewSocketPort(hostCfg)
	defer host.Close()
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	route := host.Addr().String() + "/"
	if !strings.HasPrefix(route, "wss://") {
		t.Fatalf("the door is %s, want a wss:// address", route)
	}

	if _, _, err := DialSession(route, DebugConfig(RolePeer, "")); err == nil {
		t.Fatal("a dial trusting none of the host's certificate was admitted")
	} else if err.Error() != "the host's certificate is not one this machine trusts" {
		t.Fatalf("an untrusted certificate read %q", err)
	}
	trusting := DebugConfig(RolePeer, "")
	trusting.TLS = &tls.Config{RootCAs: pool}
	if byFile, err := TrustTLS(certFile); err != nil || byFile.RootCAs == nil {
		t.Fatalf("-ca reading the certificate's own file: %v", err)
	}
	pending, got, err := DialSession(route, trusting)
	if err != nil {
		t.Fatalf("a dial trusting the host's certificate was refused: %v", err)
	}
	defer pending.Close()
	if got.Assigned != offer.Assigned {
		t.Fatalf("offer assigned %d, want %d", got.Assigned, offer.Assigned)
	}
}

// writeSelfSigned writes a certificate for 127.0.0.1 and its key to one PEM file,
// as -cert alone reads them, and returns the pool that trusts it.
func writeSelfSigned(t *testing.T, file string) *x509.CertPool {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	var pemFile bytes.Buffer
	_ = pem.Encode(&pemFile, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	_ = pem.Encode(&pemFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(file, pemFile.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return pool
}

// TestAHostServesEveryDoorItNames: a tcp door and a ws door in one -host serve one
// session, and a dialer at either is offered its place.
func TestAHostServesEveryDoorItNames(t *testing.T) {
	offer := testOffer()
	hostCfg := DebugConfig(RoleHost, "127.0.0.1:0,ws://127.0.0.1:0")
	hostCfg.ParticipantID = offer.Host
	hostCfg.AcceptSession = HostAcceptor(Coordinator{
		Assign: func([]string) (SessionOffer, error) { return offer, nil },
	}, time.Second)
	host := NewSocketPort(hostCfg)
	defer host.Close()
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	doors := host.Doors()
	if len(doors) != 2 || doors[0].Network() != "tcp" || !strings.HasPrefix(doors[1].String(), "ws://") {
		t.Fatalf("the host answers at %v; want a tcp door and a ws door", doors)
	}
	for _, door := range []string{doors[0].String(), doors[1].String() + "/"} {
		pending, got, err := DialSession(door, DebugConfig(RolePeer, ""))
		if err != nil {
			t.Fatalf("a dial at %s was refused: %v", door, err)
		}
		pending.Close()
		if got.Assigned != offer.Assigned {
			t.Fatalf("%s offered %d, want %d", door, got.Assigned, offer.Assigned)
		}
	}
	for _, list := range []string{":7777/name", "wss://:7777/vif/ws", "127.0.0.1:0,", "https://site.example"} {
		if _, err := Doors(list); err == nil {
			t.Errorf("Doors(%q) was accepted", list)
		}
	}
	for addr, hostless := range map[string]bool{":7777": true, "0.0.0.0:7777": true, "ws://:7778": true, "wss://[::]:7778": true,
		"127.0.0.1:7777": false, "host.example:7777": false, "ws://host.example:7778": false} {
		e, err := ParseEndpoint(addr)
		if err != nil || e.Hostless() != hostless {
			t.Errorf("%s hostless: %v, %v; want %v", addr, e.Hostless(), err, hostless)
		}
	}
}

// TestASelfSignedDoorIsJoinedByItsFingerprint: a wss door made for the run is
// trusted by nobody's authority, and a dialer pins its fingerprint instead; one that
// pins another is told the certificate is not the one it names.
func TestASelfSignedDoorIsJoinedByItsFingerprint(t *testing.T) {
	serverTLS, err := SelfSignedTLS([]string{"host.example"})
	if err != nil {
		t.Fatal(err)
	}
	offer := testOffer()
	hostCfg := DebugConfig(RoleHost, "wss://127.0.0.1:0")
	hostCfg.TLS = serverTLS
	hostCfg.ParticipantID = offer.Host
	hostCfg.AcceptSession = HostAcceptor(Coordinator{
		Assign: func([]string) (SessionOffer, error) { return offer, nil },
	}, time.Second)
	host := NewSocketPort(hostCfg)
	defer host.Close()
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	route := host.Addr().String() + "/"

	pin := Fingerprint(serverTLS)
	if !strings.HasPrefix(pin, "sha256:") || len(pin) != len("sha256:")+64 {
		t.Fatalf("fingerprint %q", pin)
	}
	wrong := DebugConfig(RolePeer, "")
	if wrong.TLS, err = TrustTLS("sha256:" + strings.Repeat("00", 32)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := DialSession(route, wrong); err == nil || err.Error() != "the host's certificate is not the one -ca pins" {
		t.Fatalf("a dial pinning another certificate: %v", err)
	}
	pinned := DebugConfig(RolePeer, "")
	if pinned.TLS, err = TrustTLS(pin); err != nil {
		t.Fatal(err)
	}
	pending, got, err := DialSession(route, pinned)
	if err != nil {
		t.Fatalf("a dial pinning the door's certificate was refused: %v", err)
	}
	defer pending.Close()
	if got.Assigned != offer.Assigned {
		t.Fatalf("offer assigned %d, want %d", got.Assigned, offer.Assigned)
	}
	if _, err := TrustTLS("sha256:abc"); err == nil {
		t.Fatal("a short fingerprint was accepted")
	}
}
