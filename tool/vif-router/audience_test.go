package main

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	link "github.com/lixenwraith/vif/pkg/network"
	"github.com/lixenwraith/vif/pkg/websocket"
)

// feedingPod stands in for a session's pod whose door serves a dial that only
// watches: it counts those dials and the scenarios it sent, checks the router's
// ticket where it takes them, and feeds each dial what its test writes.
type feedingPod struct {
	addr      string
	hub       *link.FeedHub
	writer    *link.FeedWriter
	dials     atomic.Int32
	scenarios atomic.Int32
	tickets   chan network.TicketBody

	mu     sync.Mutex
	shown  network.SessionOffer
	refuse error
}

// game is a session as a watching dial is shown it, told from another by its counter.
func game(session uint64) network.SessionOffer {
	return network.SessionOffer{Host: 1, Term: network.FirstTerm,
		Anchor: event.JoinAnchor{Anchor: event.JournalAnchor{ScenarioDigest: "d1", Session: session}}}
}

func startFeedingPod(t *testing.T, router network.Key) *feedingPod {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := link.NewFeedHub(network.FeedConfig(8))
	pod := &feedingPod{addr: listener.Addr().String(), hub: hub, writer: link.NewFeedWriter(hub),
		tickets: make(chan network.TicketBody, 8), shown: game(1)}
	t.Cleanup(func() {
		_ = listener.Close()
		hub.End("the test is over", false)
	})
	door := network.Coordinator{
		Name: routedID,
		Watch: func(network.Admission) (network.SessionOffer, error) {
			pod.mu.Lock()
			defer pod.mu.Unlock()
			return pod.shown, pod.refuse
		},
		Feed: func(conn net.Conn, _ network.Admission) error { return hub.Subscribe(conn) },
		Scenario: func(digest string) ([]byte, error) {
			pod.scenarios.Add(1)
			return []byte("scenario " + digest), nil
		},
	}
	if router.Valid() {
		gate := &network.TicketGate{Keys: [][]byte{router.Public()}, Session: routedID}
		door.Ticket = func(shown *network.Signed, keys []string) (*network.TicketBody, error) {
			body, err := gate.Admit(shown, keys, network.AdmitWatch)
			if err == nil {
				pod.tickets <- *body
			}
			return body, err
		}
	}
	serve := network.WatchAcceptor(door, 2*time.Second)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			pod.dials.Add(1)
			go func() {
				if serve(conn) != nil {
					_ = conn.Close()
				}
			}()
		}
	}()
	return pod
}

// start writes the feed a starting point showing o, at the tick it stands at.
func (p *feedingPod) start(t *testing.T, o network.SessionOffer, tick uint64) []byte {
	t.Helper()
	body, err := network.WatchHead{Offer: o, World: []byte("world")}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	p.writer.Sync(p.writer.Cut(tick), body)
	return body
}

// watchingDoor serves a front door that shows pod's session to watched's spectators.
func watchingDoor(t *testing.T, sessions sessionAllocator, pod *feedingPod, watched *audiences, ticket network.Key) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	router := newTCPRouter(sessions, newHolds(1), network.NewAdmissionLimiterOf(time.Minute, 1000),
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, router.game, _ = net.SplitHostPort(pod.addr)
	router.audiences, router.ticket = watched, ticket
	go router.serve(listener)
	return listener.Addr().String()
}

// spectate dials as vif -spectate does, through dial where the address is a
// socket's, and returns the dial, the session it was shown and why it was refused.
func spectate(addr, id string, dial func() (net.Conn, error)) (*network.PendingJoin, network.SessionOffer, error) {
	cfg := network.DebugConfig(link.RolePeer, "")
	cfg.SessionName, cfg.Dial = id, dial
	return network.WatchSession(addr, cfg)
}

// fed takes a dial through to its feed and returns its port once the feed's
// starting point has arrived, with that starting point.
func fed(t *testing.T, pending *network.PendingJoin, err error) (*link.FeedPort, []byte) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pending.Watch()
	if err != nil {
		t.Fatal(err)
	}
	port := network.NewFeedPort(conn)
	t.Cleanup(func() { _ = port.Close() })
	select {
	case <-port.Heads():
	case <-time.After(2 * time.Second):
		t.Fatalf("no starting point arrived: %v", port.Err())
	}
	return port, port.TakeHead()
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); !ok(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%s never happened", what)
		}
	}
}

// TestEverySpectatorIsFedFromOneSubscription: however many dials watch a session,
// over the front door and the browser route, its pod is dialled once, under a
// ticket of the router's to watch; each dial is shown the session, sent the
// scenario if it asks and fed the pod's records from the router's copy; a dial
// after a new game is shown that game; and the listing counts them.
func TestEverySpectatorIsFedFromOneSubscription(t *testing.T) {
	key := network.NewKey()
	pod := startFeedingPod(t, key)
	sessions := &fakeSessionAllocator{podIP: "127.0.0.1", sessions: []session{{ID: routedID}}}
	watched := newAudiences(8, 0)
	addr := watchingDoor(t, sessions, pod, watched, key)
	server := httptest.NewUnstartedServer(nil)
	api := newAPIServer(sessions, slog.New(slog.NewTextHandler(io.Discard, nil)), nil,
		allocatorConfig{WebOrigin: "http://" + server.Listener.Addr().String()}, newHolds(1), nil)
	_, api.ws.route.game, _ = net.SplitHostPort(pod.addr)
	api.showSessions(watched)
	api.admitWith(key)
	server.Config.Handler = api
	server.Start()
	t.Cleanup(server.Close)
	socket := func() (net.Conn, error) {
		return websocket.Dial(t.Context(), "ws://"+server.Listener.Addr().String()+wsRoutePrefix+routedID, nil)
	}

	first := pod.start(t, game(1), 0)
	var ports []*link.FeedPort
	for _, dial := range []func() (net.Conn, error){nil, nil, socket} {
		pending, shown, err := spectate(addr, routedID, dial)
		if err == nil && shown.Anchor.Anchor.Session != 1 {
			t.Fatalf("a dial was shown session %d, want the pod's", shown.Anchor.Anchor.Session)
		}
		if dial == nil && len(ports) == 1 {
			if body, err := pending.RequestScenario("d1"); err != nil || string(body) != "scenario d1" {
				t.Fatalf("a dial that asked for the scenario was sent %q, %v", body, err)
			}
		}
		port, head := fed(t, pending, err)
		if string(head) != string(first) {
			t.Fatalf("a dial began from %d bytes, want the pod's starting point", len(head))
		}
		ports = append(ports, port)
	}
	if ticket := <-pod.tickets; ticket.Kind != network.AdmitWatch || ticket.Subject != feedSubject || ticket.Session != routedID {
		t.Fatalf("the router's subscription showed the ticket %+v", ticket)
	}
	pod.writer.Frame(network.MsgEvent, []byte("t1"))
	pod.writer.Mark(1)
	pod.writer.Flush()
	for i, port := range ports {
		eventually(t, "a spectator's tick", func() bool { return port.Ready(1) })
		if n := port.Drain(make([]link.Inbound, 4)); n != 1 {
			t.Fatalf("spectator %d was handed %d frame(s) for the tick, want the pod's one", i, n)
		}
	}

	second := pod.start(t, game(2), 1)
	eventually(t, "the new game's starting point", func() bool { return string(ports[0].TakeHead()) == string(second) })
	pending, shown, err := spectate(addr, routedID, nil)
	if err != nil || shown.Anchor.Anchor.Session != 2 {
		t.Fatalf("a dial after a new game was shown session %d, %v; want the game its feed begins from", shown.Anchor.Anchor.Session, err)
	}
	if _, head := fed(t, pending, err); string(head) != string(second) {
		t.Fatal("a dial after a new game began from another starting point than the newest")
	}
	if dials, sent := pod.dials.Load(), pod.scenarios.Load(); dials != 1 || sent != 1 {
		t.Fatalf("four spectators cost the pod %d dial(s) and %d scenario(s), want one of each", dials, sent)
	}

	resp, err := http.Get(server.URL + "/vif/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var listed sessionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil || len(listed.Sessions) != 1 || listed.Sessions[0].Spectators != 4 {
		t.Fatalf("the listing reads %+v, %v; want the session's four spectators", listed.Sessions, err)
	}
}

// TestAnAudienceTakesWhatItsCeilingsAllow: a session is shown to as many dials as
// -spectators names and one address to as many as -spectators-per-address, each
// past it refused as a full audience is; a spectator that leaves frees its place.
func TestAnAudienceTakesWhatItsCeilingsAllow(t *testing.T) {
	pod := startFeedingPod(t, network.Key{})
	pod.start(t, game(1), 0)
	sessions := &fakeSessionAllocator{podIP: "127.0.0.1"}
	full := &network.Refusal{Code: network.RefusalAudience}

	addr := watchingDoor(t, sessions, pod, newAudiences(2, 0), network.Key{})
	pending, _, err := spectate(addr, routedID, nil)
	leaving, _ := fed(t, pending, err)
	pending, _, err = spectate(addr, routedID, nil)
	fed(t, pending, err)
	if _, _, err := spectate(addr, routedID, nil); !errors.Is(err, full) || !strings.Contains(err.Error(), "the 2 spectator(s) it takes") {
		t.Fatalf("a dial past the session's ceiling ended %v", err)
	}
	_ = leaving.Close()
	eventually(t, "a place freed by a spectator that left", func() bool {
		pending, _, err := spectate(addr, routedID, nil)
		if err == nil {
			_ = pending.Close()
		}
		return err == nil
	})

	addr = watchingDoor(t, sessions, pod, newAudiences(8, 1), network.Key{})
	pending, _, err = spectate(addr, routedID, nil)
	fed(t, pending, err)
	if _, _, err := spectate(addr, routedID, nil); !errors.Is(err, full) || !strings.Contains(err.Error(), "this address has the 1 spectator(s)") {
		t.Fatalf("an address's dial past its own ceiling ended %v", err)
	}
}

// TestAnUnwatchedFeedIsDroppedAndAnEndedOneToldAsItWas: the router keeps its
// subscription for the linger after the last spectator left, so a dial within it
// costs the pod nothing, then drops it; a feed its pod ends reaches each spectator
// with the pod's reason, and a dial after is told what the pod then says.
func TestAnUnwatchedFeedIsDroppedAndAnEndedOneToldAsItWas(t *testing.T) {
	pod := startFeedingPod(t, network.Key{})
	pod.start(t, game(1), 0)
	watched := newAudiences(8, 0)
	watched.linger = 100 * time.Millisecond
	addr := watchingDoor(t, &fakeSessionAllocator{podIP: "127.0.0.1"}, pod, watched, network.Key{})

	for range 2 {
		pending, _, err := spectate(addr, routedID, nil)
		port, _ := fed(t, pending, err)
		_ = port.Close()
	}
	if n := pod.dials.Load(); n != 1 {
		t.Fatalf("a dial within the linger cost the pod %d dial(s), want the first alone", n)
	}
	eventually(t, "the unwatched subscription's end", func() bool { return pod.hub.Subscribers() == 0 })
	pending, _, err := spectate(addr, routedID, nil)
	port, _ := fed(t, pending, err)
	if n := pod.dials.Load(); n != 2 {
		t.Fatalf("a dial after the linger found %d pod dial(s), want a subscription of its own", n)
	}

	pod.mu.Lock()
	pod.refuse = &network.Refusal{Code: network.RefusalEnding, Reason: "session is ending: its host closed it"}
	pod.mu.Unlock()
	pod.hub.End("The host closed the session", false)
	select {
	case <-port.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("a spectator never learned its session's feed ended")
	}
	if ended := (link.FeedEnded{}); !errors.As(port.Err(), &ended) || ended.Reason != "The host closed the session" || ended.Again {
		t.Fatalf("the feed ended as %v, want the pod's word", port.Err())
	}
	eventually(t, "the pod's own refusal", func() bool {
		_, _, err := spectate(addr, routedID, nil)
		return errors.Is(err, &network.Refusal{Code: network.RefusalEnding}) && network.RetryOf(err) == network.RetryNever
	})
}

// TestAWatchDialNobodyFeedsIsToldSo: a router that shows no session, and a door a
// run published, which is a guest's and writes no feed, each answer a dial that
// only watches as a door with no feed does, before any pod or run is asked.
func TestAWatchDialNobodyFeedsIsToldSo(t *testing.T) {
	pod := startFeedingPod(t, network.Key{})
	sessions := &fakeSessionAllocator{podIP: "127.0.0.1"}
	if _, _, err := spectate(watchingDoor(t, sessions, pod, nil, network.Key{}), routedID, nil); !errors.Is(err, network.ErrUnwatched) {
		t.Fatalf("a router that shows no session answered %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	addr := listener.Addr().String()
	router := newTCPRouter(sessions, newHolds(1), network.NewAdmissionLimiter(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	router.tunnels = network.NewTunnelHub(func(id string) string { return "vif://" + addr + "/" + id }, 2, 0)
	router.audiences = newAudiences(8, 0)
	_, router.game, _ = net.SplitHostPort(pod.addr)
	go router.serve(listener)
	control, door := publishDoor(t, addr, network.PublishRequest{Session: "match"})
	defer control.Close()
	if _, _, err := spectate(addr, door.ID, nil); !errors.Is(err, network.ErrUnwatched) || !strings.Contains(err.Error(), "no session to watch") {
		t.Fatalf("a watch dial on a published door answered %v", err)
	}
	if n := pod.dials.Load(); n != 0 {
		t.Fatalf("refused watch dials reached the pod %d time(s)", n)
	}
}
