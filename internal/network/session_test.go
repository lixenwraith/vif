package network

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
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
	hostCfg.AcceptSession = HostAcceptor(Coordinator{Assign: func() (SessionOffer, error) { return offer, nil }}, time.Second)
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
		Assign: func() (SessionOffer, error) { return offer, nil },
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
		Assign: func() (SessionOffer, error) { return offer, nil },
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
