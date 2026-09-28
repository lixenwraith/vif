package main

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
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
func startFrontDoor(t *testing.T, sessions sessionAllocator, held *holds, pod *testPod) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	router := newTCPRouter(sessions, held, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, router.game, _ = net.SplitHostPort(pod.addr)
	go router.serve(listener)
	return listener.Addr().String()
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
			newHolds(1), pod)
		if err := join(addr, tc.id); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: joined with %v, want a refusal naming %q", tc.name, err, tc.want)
		}
	}

	// A stream that does not open with a route frame is no vif dialer, so it is
	// closed with nothing written back.
	addr := startFrontDoor(t, &fakeSessionAllocator{podIP: "127.0.0.1"}, newHolds(1), pod)
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
	addr := startFrontDoor(t, &fakeSessionAllocator{podIP: "127.0.0.1"}, newHolds(1), pod)
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

// TestTheSessionCeilingSpansBothRoutes: a browser holding the session's one slot
// refuses a native dial before any pod is dialled, and releasing it admits the next.
func TestTheSessionCeilingSpansBothRoutes(t *testing.T) {
	pod := startPod(t)
	held := newHolds(1)
	addr := startFrontDoor(t, &fakeSessionAllocator{podIP: "127.0.0.1"}, held, pod)
	release, ok := held.hold(routedID)
	if !ok {
		t.Fatal("an empty session refused its first connection")
	}
	if err := join(addr, routedID); err == nil || !strings.Contains(err.Error(), "as many connections") {
		t.Fatalf("a dial past the ceiling returned %v", err)
	}
	if n := pod.accepted.Load(); n != 0 {
		t.Fatalf("a dial past the ceiling reached the pod %d time(s)", n)
	}

	release()
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

// TestTheFrontDoorBoundsAdmissionsPerAddress holds the game's own budget against
// the dialler's address, which the pod no longer sees: past it a dialer is refused
// before a lookup it could otherwise make the fleet repeat.
func TestTheFrontDoorBoundsAdmissionsPerAddress(t *testing.T) {
	pod := startPod(t)
	addr := startFrontDoor(t, &fakeSessionAllocator{routeErr: errSessionUnknown}, newHolds(1), pod)
	for i := range parameter.NetworkAdmitBurst {
		if err := join(addr, routedID); err == nil || !strings.Contains(err.Error(), "no such session") {
			t.Fatalf("dial %d of the budget returned %v", i+1, err)
		}
	}
	if err := join(addr, routedID); err == nil || !strings.Contains(err.Error(), "too often") {
		t.Fatalf("a dial past the budget returned %v", err)
	}
}
