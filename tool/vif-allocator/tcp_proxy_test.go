package main

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/pkg/websocket"
)

const routedID = "0123456789abcdef"

// testPod stands in for a session's game port: it records the name it is sent,
// then echoes the stream until the far side ends it.
type testPod struct {
	addr     string
	accepted atomic.Int32
	names    chan string
}

func startPod(t *testing.T) *testPod {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	pod := &testPod{addr: listener.Addr().String(), names: make(chan string, 8)}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			pod.accepted.Add(1)
			go func() {
				defer conn.Close()
				name, err := network.ReadSessionRoute(conn)
				if err != nil {
					return
				}
				pod.names <- name
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	return pod
}

// startFrontDoor serves a front door whose every session resolves to pod.
func startFrontDoor(t *testing.T, sessions sessionAllocator, held *holds, joins *network.AdmissionLimiter, pod *testPod) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	router := newTCPRouter(sessions, held, joins, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, router.game, _ = net.SplitHostPort(pod.addr)
	go router.serve(listener)
	return listener.Addr().String()
}

// startBrowserRoute serves a browser route this allocator terminates, whose every
// session resolves to pod, and returns its ws:// prefix. The origin it answers is
// its own URL's, the one a native client sends.
func startBrowserRoute(t *testing.T, sessions sessionAllocator, held *holds, pod *testPod) string {
	t.Helper()
	server := httptest.NewUnstartedServer(nil)
	api := newAPIServer(sessions, slog.New(slog.NewTextHandler(io.Discard, nil)), nil,
		allocatorConfig{WebOrigin: "http://" + server.Listener.Addr().String()}, held, nil)
	_, api.ws.route.game, _ = net.SplitHostPort(pod.addr)
	server.Config.Handler = api
	server.Start()
	t.Cleanup(server.Close)
	return "ws://" + server.Listener.Addr().String() + wsRoutePrefix
}

// released waits for held to carry nothing, as it does once every splice has ended.
func released(t *testing.T, held *holds) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		held.mu.Lock()
		n := len(held.n)
		held.mu.Unlock()
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("a closed connection kept its session's slot")
		}
	}
}

// join dials as vif -join vif://<addr>/<id> does and returns why it was refused.
func join(addr, id string) error {
	cfg := network.DebugConfig(network.RolePeer, "")
	cfg.SessionName = id
	pending, _, err := network.DialSession(addr, cfg)
	if err == nil {
		_ = pending.Close()
	}
	return err
}

// TestEitherRouteRefusesBeforeDialling covers each answer short of a pod over the
// front door and the browser route alike: none of them may cost that pod a
// connection, and a dialer that named a session is told why in the stream, and
// whether a retry can help, which a page reads where it is never shown a status.
func TestEitherRouteRefusesBeforeDialling(t *testing.T) {
	pod := startPod(t)
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	refusing := &testPod{addr: closed.Addr().String()}
	_ = closed.Close()
	for _, tc := range []struct {
		name, id, want string
		routeErr       error
		pod            *testPod
		retry          network.Retry
	}{
		{"an identifier no session could have", "not-a-session", "no such session", nil, pod, network.RetryNever},
		{"an unknown session", routedID, "no such session", errSessionUnknown, pod, network.RetryNever},
		{"a full session", routedID, "not accepting players", sessionRefusing("occupied"), pod, network.RetryLater},
		{"a draining session", routedID, "not accepting players", sessionRefusing("draining"), pod, network.RetryNever},
		{"a session with no reachable pod", routedID, "no reachable pod", errSessionUnroutable, pod, network.RetryLater},
		{"a pod that refuses the dial", routedID, "no reachable pod", nil, refusing, network.RetryLater},
	} {
		sessions := &fakeSessionAllocator{podIP: "127.0.0.1", routeErr: tc.routeErr}
		for route, addr := range map[string]string{
			"front door":    startFrontDoor(t, sessions, newHolds(1), network.NewAdmissionLimiter(), tc.pod),
			"browser route": startBrowserRoute(t, sessions, newHolds(1), tc.pod) + tc.id,
		} {
			err := join(addr, tc.id)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tc.want) {
				t.Errorf("%s over the %s: joined with %v, want a refusal naming %q", tc.name, route, err, tc.want)
			}
			if route == "browser route" && tc.id != routedID {
				continue // a malformed path is answered before the upgrade
			}
			if status := new(websocket.HandshakeError); errors.As(err, &status) {
				t.Errorf("%s over the %s: refused with %d before the upgrade", tc.name, route, status.Status)
			}
			if got := network.RetryOf(err); got != tc.retry {
				t.Errorf("%s over the %s: %v reads as retry %d, want %d", tc.name, route, err, got, tc.retry)
			}
		}
	}

	// A stream that does not open with a route frame is no vif dialer, so it is
	// closed with nothing written back.
	addr := startFrontDoor(t, &fakeSessionAllocator{podIP: "127.0.0.1"}, newHolds(1), network.NewAdmissionLimiter(), pod)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := network.NewMessage(network.MsgEvent, nil).Encode(conn); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if reply, err := io.ReadAll(conn); err != nil || len(reply) != 0 {
		t.Errorf("an epoch opening was answered with %q, %v", reply, err)
	}
	if n := pod.accepted.Load(); n != 0 {
		t.Fatalf("refused dials reached the pod %d time(s)", n)
	}
}

// TestAStreamPastThePlacementBoundIsClosedAtOnce: both routes place with place, so
// neither queues a stream behind silent ones it is still reading.
func TestAStreamPastThePlacementBoundIsClosedAtOnce(t *testing.T) {
	router := newTCPRouter(&fakeSessionAllocator{}, newHolds(1), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	router.placing = make(chan struct{}, 1)
	silent, placing := net.Pipe()
	defer silent.Close()
	go router.place(placing, nil)
	for deadline := time.Now().Add(time.Second); len(router.placing) == 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("a silent stream was never counted as being placed")
		}
	}
	past, dialler := net.Pipe()
	if upstream, _ := router.place(past, nil); upstream != nil {
		t.Fatal("a stream past the bound was placed")
	}
	_ = dialler.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := dialler.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("a stream past the bound read %v, want it closed", err)
	}
}

// TestTheFrontDoorReplaysTheNameAndSplicesBothWays: the pod reads the name the
// dialer sent, the bytes after it arrive unread by the door, and each side's end
// reaches the other as a half-close.
func TestTheFrontDoorReplaysTheNameAndSplicesBothWays(t *testing.T) {
	pod := startPod(t)
	addr := startFrontDoor(t, &fakeSessionAllocator{podIP: "127.0.0.1"}, newHolds(1), network.NewAdmissionLimiter(), pod)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := network.NewMessage(network.MsgSessionRoute, []byte(routedID)).Encode(conn); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if echoed, err := io.ReadAll(conn); err != nil || string(echoed) != "ping" {
		t.Fatalf("read back %q, %v; want ping and the pod's end", echoed, err)
	}
	if got := <-pod.names; got != routedID {
		t.Fatalf("the pod was sent %q, want %q", got, routedID)
	}
}

// TestTheBrowserRouteSplicesTheSocketToThePod: the pod reads the name the browser
// sent inside the socket, bytes pass both ways, and the allocator answers the
// browser's close.
func TestTheBrowserRouteSplicesTheSocketToThePod(t *testing.T) {
	pod := startPod(t)
	route := startBrowserRoute(t, &fakeSessionAllocator{podIP: "127.0.0.1"}, newHolds(1), pod)
	conn, err := websocket.Dial(t.Context(), route+routedID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := network.NewMessage(network.MsgSessionRoute, []byte(routedID)).Encode(conn); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	echoed := make([]byte, 4)
	if _, err := io.ReadFull(conn, echoed); err != nil || string(echoed) != "ping" {
		t.Fatalf("read back %q, %v", echoed, err)
	}
	if got := <-pod.names; got != routedID {
		t.Fatalf("the pod was sent %q, want the name the browser sent", got)
	}
	start := time.Now()
	if err := conn.Close(); err != nil || time.Since(start) > time.Second {
		t.Fatalf("Close = %v after %s; the allocator did not answer the close", err, time.Since(start))
	}
}

// TestTheSessionCeilingSpansBothRoutes: a browser holding the session's one slot
// refuses a native dial before any pod is dialled, and its close admits the next.
func TestTheSessionCeilingSpansBothRoutes(t *testing.T) {
	pod := startPod(t)
	held := newHolds(1)
	sessions := &fakeSessionAllocator{podIP: "127.0.0.1"}
	addr := startFrontDoor(t, sessions, held, network.NewAdmissionLimiter(), pod)
	browser, err := websocket.Dial(t.Context(), startBrowserRoute(t, sessions, held, pod)+routedID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := network.NewMessage(network.MsgSessionRoute, []byte(routedID)).Encode(browser); err != nil {
		t.Fatal(err)
	}
	<-pod.names
	if err := join(addr, routedID); err == nil || !strings.Contains(err.Error(), "as many connections") {
		t.Fatalf("a dial past the ceiling returned %v", err)
	}
	if n := pod.accepted.Load(); n != 1 {
		t.Fatalf("the pod was dialled %d time(s), want the browser's alone", n)
	}

	_ = browser.Close()
	released(t, held)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := network.NewMessage(network.MsgSessionRoute, []byte(routedID)).Encode(conn); err != nil {
		t.Fatal(err)
	}
	select {
	case <-pod.names:
	case <-time.After(2 * time.Second):
		t.Fatal("a released slot did not admit the next dial")
	}
}

// TestAnAddressHasOneJoinBudgetOverBothRoutes holds the game's own budget against
// the player's address, which the pod no longer sees: the front door keys it on the
// dialler and the browser route on the edge's header, each before a lookup.
func TestAnAddressHasOneJoinBudgetOverBothRoutes(t *testing.T) {
	sessions := &fakeSessionAllocator{routeErr: errSessionUnknown}
	joins := network.NewAdmissionLimiterOf(time.Minute, 1)
	addr := startFrontDoor(t, sessions, newHolds(1), joins, startPod(t))
	if err := join(addr, routedID); err == nil || !strings.Contains(err.Error(), "no such session") {
		t.Fatalf("the budget's one dial returned %v", err)
	}
	if err := join(addr, routedID); err == nil || !strings.Contains(err.Error(), "too often") {
		t.Fatalf("a dial past the budget returned %v", err)
	}

	var player atomic.Value
	server := httptest.NewUnstartedServer(nil)
	api := newAPIServer(sessions, slog.New(slog.NewTextHandler(io.Discard, nil)), nil,
		allocatorConfig{WebOrigin: "http://" + server.Listener.Addr().String(), ClientAddressHeader: "X-Real-IP"},
		newHolds(1), joins)
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Real-IP", player.Load().(string))
		api.ServeHTTP(w, r)
	})
	server.Start()
	defer server.Close()
	route := "ws://" + server.Listener.Addr().String() + wsRoutePrefix + routedID
	for _, tc := range []struct{ player, want string }{{"127.0.0.1", "too often"}, {"192.0.2.1", "no such session"}} {
		player.Store(tc.player)
		if err := join(route, routedID); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("a browser at %s was answered %v, want a refusal naming %q", tc.player, err, tc.want)
		}
	}
}
