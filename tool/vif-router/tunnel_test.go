package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/network"
	link "github.com/lixenwraith/vif/pkg/network"
)

// startDoorFrontDoor serves a front door whose pods resolve to pod and which carries
// up to doors published doors.
func startDoorFrontDoor(t *testing.T, pod *testPod, doors int) (string, *network.TunnelHub) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	addr := listener.Addr().String()
	hub := network.NewTunnelHub(func(id string) string { return "vif://" + addr + "/" + id }, doors, 0)
	router := newTCPRouter(&fakeSessionAllocator{routeErr: errSessionUnknown}, newHolds(1), network.NewAdmissionLimiter(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	router.tunnels = hub
	router.opens = 500 * time.Millisecond
	_, router.game, _ = net.SplitHostPort(pod.addr)
	go router.serve(listener)
	return addr, hub
}

// publishDoor publishes a door on the front door by hand and returns its control
// connection and the answer.
func publishDoor(t *testing.T, addr string, req network.PublishRequest) (net.Conn, network.Published) {
	t.Helper()
	control, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(req)
	if err := link.NewMessage(network.MsgSessionPublish, body).Encode(control); err != nil {
		t.Fatal(err)
	}
	_ = control.SetReadDeadline(time.Now().Add(2 * time.Second))
	msg, err := link.DecodeWithin(control, network.MaxTunnelFrame)
	var ans network.Published
	if err != nil || msg.Type != network.MsgSessionPublished || json.Unmarshal(msg.Payload, &ans) != nil {
		t.Fatalf("published: %v, %v", msg, err)
	}
	_ = control.SetReadDeadline(time.Time{})
	return control, ans
}

// TestTheFrontDoorPlacesADialOnAPublishedDoor: a run publishes its door and is handed
// a link at the front door's own address; a dial on it is placed with the front
// door's own checks, the run opens a connection back for its ticket, the door reads
// the name its run answers to and the bytes pass; a door nobody answers for refuses
// the dial in time; the ceiling spans doors as it spans pods.
func TestTheFrontDoorPlacesADialOnAPublishedDoor(t *testing.T) {
	pod := startPod(t)
	addr, hub := startDoorFrontDoor(t, pod, 2)
	control, ans := publishDoor(t, addr, network.PublishRequest{Session: "match"})
	defer control.Close()
	if err := ans.Refused(); err != nil || ans.ID == "" || !strings.HasPrefix(ans.Link, "vif://"+addr+"/") || !hub.Has(ans.ID) {
		t.Fatalf("published %+v, %v; want a door named at the front door", ans, err)
	}
	// The run answers each open with a connection back naming the ticket.
	go func() {
		for {
			msg, err := link.DecodeWithin(control, network.MaxTunnelFrame)
			if err != nil {
				return
			}
			if msg.Type != network.MsgTunnelOpen {
				continue
			}
			var ticket network.TunnelTicket
			_ = json.Unmarshal(msg.Payload, &ticket)
			back, err := net.Dial("tcp", addr)
			if err != nil {
				return
			}
			body, _ := json.Marshal(ticket)
			_ = link.NewMessage(network.MsgTunnelAccept, body).Encode(back)
			go func() {
				defer back.Close()
				name, err := network.ReadSessionRoute(back)
				if err != nil {
					return
				}
				pod.names <- name
				_, _ = io.Copy(back, back)
			}()
		}
	}()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := link.NewMessage(network.MsgSessionRoute, []byte(ans.ID)).Encode(conn); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("read back %q, %v; want the door's echo", buf, err)
	}
	if got := <-pod.names; got != "match" {
		t.Fatalf("the door was sent %q, want the name its run answers to", got)
	}
	if n := pod.accepted.Load(); n != 0 {
		t.Fatalf("a dial on a door reached the pod %d time(s)", n)
	}
	// The one connection the ceiling allows is held: a second dial is refused before
	// the run is asked.
	if err := join(addr, ans.ID); err == nil || !strings.Contains(err.Error(), "as many connections") {
		t.Fatalf("a second dial on a door at its ceiling: %v; want the ceiling's refusal", err)
	}

	// A door whose run never opens refuses the dial once the open times out.
	silent, quiet := publishDoor(t, addr, network.PublishRequest{Session: "quiet"})
	defer silent.Close()
	if err := quiet.Refused(); err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	err = join(addr, quiet.ID)
	if err == nil || !strings.Contains(err.Error(), "did not open") || network.RetryOf(err) != network.RetryLater {
		t.Fatalf("a dial on a silent door: %v; want it refused for a retry once the open timed out", err)
	}
	if waited := time.Since(before); waited < 500*time.Millisecond || waited > 5*time.Second {
		t.Fatalf("the dial waited %v on the silent door, want about the open timeout", waited)
	}
}
