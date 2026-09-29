package main

import (
	"fmt"
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
	_, api.ws.game, _ = net.SplitHostPort(pod.addr)
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

// TestTheFrontDoorRefusesBeforeDialling covers each answer a lookup can give short of
// a pod: none of them may cost that pod a connection, and a dialer that named a
// session is told why rather than left reading a closed stream.
func TestTheFrontDoorRefusesBeforeDialling(t *testing.T) {
	pod := startPod(t)
	for _, tc := range []struct {
		name, id, want string
		routeErr       error
	}{
		{"an identifier no session could have", "not-a-session", "no such session", nil},
		{"an unknown session", routedID, "no such session", errSessionUnknown},
		{"a full session", routedID, "not accepting players", fmt.Errorf("%w: occupied", errSessionRefusing)},
	} {
		addr := startFrontDoor(t, &fakeSessionAllocator{podIP: "127.0.0.1", routeErr: tc.routeErr},
			newHolds(1), network.NewAdmissionLimiter(), pod)
		if err := join(addr, tc.id); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: joined with %v, want a refusal naming %q", tc.name, err, tc.want)
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
	if err := network.NewMessage(network.MsgHeartbeat, nil).Encode(conn); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if reply, err := io.ReadAll(conn); err != nil || len(reply) != 0 {
		t.Errorf("a heartbeat opening was answered with %q, %v", reply, err)
	}
	if n := pod.accepted.Load(); n != 0 {
		t.Fatalf("refused dials reached the pod %d time(s)", n)
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
// sent inside the socket rather than one the allocator wrote, bytes pass both ways,
// and the allocator answers the browser's close.
func TestTheBrowserRouteSplicesTheSocketToThePod(t *testing.T) {
	pod := startPod(t)
	route := startBrowserRoute(t, &fakeSessionAllocator{podIP: "127.0.0.1"}, newHolds(1), pod)
	conn, err := websocket.Dial(t.Context(), route+routedID)
	if err != nil {
		t.Fatal(err)
	}
	if err := network.NewMessage(network.MsgSessionRoute, []byte("named-inside")).Encode(conn); err != nil {
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
	if got := <-pod.names; got != "named-inside" {
		t.Fatalf("the pod was sent %q, want the browser's own frame", got)
	}
	start := time.Now()
	if err := conn.Close(); err != nil || time.Since(start) > time.Second {
		t.Fatalf("Close = %v after %s; the allocator did not answer the close", err, time.Since(start))
	}
}

// TestANativeDialReadsTheBrowserRoutesRefusal: a refusal before the upgrade reaches
// vif -join as the allocator's message, as the front door's join reply does.
func TestANativeDialReadsTheBrowserRoutesRefusal(t *testing.T) {
	route := startBrowserRoute(t, &fakeSessionAllocator{routeErr: errSessionUnknown}, newHolds(1), startPod(t))
	if err := join(route+routedID, routedID); err == nil || err.Error() != "No such session" {
		t.Fatalf("a refused dial returned %v", err)
	}
}

// TestTheSessionCeilingSpansBothRoutes: a browser holding the session's one slot
// refuses a native dial before any pod is dialled, and its close admits the next.
func TestTheSessionCeilingSpansBothRoutes(t *testing.T) {
	pod := startPod(t)
	held := newHolds(1)
	sessions := &fakeSessionAllocator{podIP: "127.0.0.1"}
	addr := startFrontDoor(t, sessions, held, network.NewAdmissionLimiter(), pod)
	browser, err := websocket.Dial(t.Context(), startBrowserRoute(t, sessions, held, pod)+routedID)
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

	api := newAPIServer(sessions, slog.New(slog.NewTextHandler(io.Discard, nil)), nil,
		allocatorConfig{WebOrigin: "https://site.example", ClientAddressHeader: "X-Real-IP"}, newHolds(1), joins)
	for player, want := range map[string]int{"127.0.0.1": http.StatusTooManyRequests, "192.0.2.1": http.StatusNotFound} {
		request := httptest.NewRequest(http.MethodGet, wsRoutePrefix+routedID, nil)
		request.Header.Set("Connection", "Upgrade")
		request.Header.Set("Upgrade", "websocket")
		request.Header.Set("Origin", "https://site.example")
		request.Header.Set("X-Real-IP", player)
		response := httptest.NewRecorder()
		api.ServeHTTP(response, request)
		if response.Code != want {
			t.Errorf("a browser at %s was answered %d, want %d", player, response.Code, want)
		}
	}
}
