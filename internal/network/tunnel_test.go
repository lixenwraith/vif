package network

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	link "github.com/lixenwraith/vif/pkg/network"
)

// doorRouter is a front door for doors alone, as a router's places them: a
// publication becomes a control connection, a route frame opens the door it names
// and splices the dial to the connection the run opens back for it.
type doorRouter struct {
	addr string
	hub  *TunnelHub
	open time.Duration
}

func startDoorRouter(t *testing.T, max int) *doorRouter {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	r := &doorRouter{addr: ln.Addr().String(), open: 2 * time.Second}
	r.hub = NewTunnelHub(func(id string) string { return "vif://" + r.addr + "/" + id }, max, 0)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go r.place(conn)
		}
	}()
	return r
}

func (r *doorRouter) place(conn net.Conn) {
	first, err := link.DecodeWithin(conn, MaxTunnelFrame)
	if err != nil {
		_ = conn.Close()
		return
	}
	switch first.Type {
	case MsgSessionPublish:
		var req PublishRequest
		_ = json.Unmarshal(first.Payload, &req)
		if d, err := r.hub.Publish(conn, "", req); err == nil {
			r.hub.Serve(d)
		}
	case MsgTunnelAccept:
		var ticket TunnelTicket
		_ = json.Unmarshal(first.Payload, &ticket)
		if !r.hub.Accept(conn, ticket.Ticket) {
			_ = conn.Close()
		}
	case MsgSessionRoute:
		upstream, session, err := r.hub.Open(string(first.Payload), r.open)
		if err != nil {
			RefuseJoin(conn, err, time.Second)
			_ = conn.Close()
			return
		}
		_ = link.NewMessage(MsgSessionRoute, []byte(session)).Encode(upstream)
		var done sync.WaitGroup
		done.Add(2)
		pipe := func(dst, src net.Conn) {
			defer done.Done()
			_, _ = io.Copy(dst, src)
			if half, ok := dst.(interface{ CloseWrite() error }); ok {
				_ = half.CloseWrite()
			}
		}
		go pipe(upstream, conn)
		go pipe(conn, upstream)
		done.Wait()
		_ = conn.Close()
		_ = upstream.Close()
	default:
		_ = conn.Close()
	}
}

// dialDoor dials a door by name as a joiner does and returns the stream past the
// route frame, or the refusal read back.
func dialDoor(t *testing.T, addr, id string) (net.Conn, error) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	if err := link.NewMessage(MsgSessionRoute, []byte(id)).Encode(conn); err != nil {
		t.Fatal(err)
	}
	return conn, nil
}

// TestADoorPublishedThroughARouterTakesADial: a run's door is named and linked by the
// router, a dial on that name reaches the connection the run opens back carrying the
// name the run's door answers to, and bytes pass both ways.
func TestADoorPublishedThroughARouterTakesADial(t *testing.T) {
	t.Parallel()
	r := startDoorRouter(t, 2)
	served := make(chan net.Conn, 1)
	published := make(chan string, 4)
	tunnel := &DoorTunnel{Router: r.addr, Session: "match"}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go tunnel.Run(ctx, func(door string) { published <- door }, func(conn net.Conn) { served <- conn }, nil)
	var door string
	select {
	case door = <-published:
	case <-time.After(5 * time.Second):
		t.Fatal("the door was not published")
	}
	e, err := link.ParseEndpoint(door)
	if err != nil || e.Addr != r.addr || !validTunnelID(e.Name) || !r.hub.Has(e.Name) || tunnel.Door() != door {
		t.Fatalf("published %q (%v); want a link at the router naming a door it carries", door, err)
	}
	dial, _ := dialDoor(t, r.addr, e.Name)
	defer dial.Close()
	var back net.Conn
	select {
	case back = <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("the run was asked for no connection back")
	}
	defer back.Close()
	_ = back.SetReadDeadline(time.Now().Add(2 * time.Second))
	if name, err := ReadSessionRoute(back); err != nil || name != "match" {
		t.Fatalf("the door read %q, %v; want the name it answers to", name, err)
	}
	if _, err := dial.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(back, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("the door read %q, %v", buf, err)
	}
	if _, err := back.Write([]byte("pong")); err != nil {
		t.Fatal(err)
	}
	_ = dial.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(dial, buf); err != nil || string(buf) != "pong" {
		t.Fatalf("the dialer read %q, %v", buf, err)
	}
}

// TestADoorResumesItsNameAndRefusesAnotherRun: a run whose control connection ended
// publishes again with its name and token and keeps the name; a run naming it with
// another token is refused outright, and the router carries at most its doors.
func TestADoorResumesItsNameAndRefusesAnotherRun(t *testing.T) {
	t.Parallel()
	r := startDoorRouter(t, 1)
	published := make(chan string, 8)
	tunnel := &DoorTunnel{Router: r.addr}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go tunnel.Run(ctx, func(door string) { published <- door }, nil, nil)
	first := <-published
	e, _ := link.ParseEndpoint(first)
	// The router loses the control connection: the run comes back under its name.
	r.hub.mu.Lock()
	d := r.hub.doors[e.Name]
	r.hub.mu.Unlock()
	_ = d.control.Close()
	select {
	case again := <-published:
		if again != first {
			t.Fatalf("published again as %q, want %q kept", again, first)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the door was not published again")
	}
	if !r.hub.Has(e.Name) || r.hub.Count() != 1 {
		t.Fatalf("the router carries %d doors and %q is %t; want the one, kept", r.hub.Count(), e.Name, r.hub.Has(e.Name))
	}
	// Another run naming the door with another token, and one more than the router
	// carries, are refused with a kind the dialer acts on.
	for _, tc := range []struct {
		name string
		req  PublishRequest
		want RefusalCode
	}{
		{"the name with another token", PublishRequest{ID: e.Name, Resume: "0000"}, RefusalIdentity},
		{"a door past the router's count", PublishRequest{}, RefusalFull},
	} {
		conn, err := net.Dial("tcp", r.addr)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(tc.req)
		_ = link.NewMessage(MsgSessionPublish, body).Encode(conn)
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		msg, err := link.DecodeWithin(conn, MaxTunnelFrame)
		_ = conn.Close()
		var ans Published
		if err != nil || msg.Type != MsgSessionPublished || json.Unmarshal(msg.Payload, &ans) != nil {
			t.Fatalf("%s: answered %v, %v", tc.name, msg, err)
		}
		var refused *Refusal
		if err := ans.Refused(); !errors.As(err, &refused) || refused.Code != tc.want {
			t.Fatalf("%s: refused with %v, want kind %d", tc.name, err, tc.want)
		}
	}
}

// TestADialOnASilentDoorIsRefusedInTime: a run that opens no connection back within
// the open timeout refuses the dial with a reason it may retry on, and the ticket
// nobody answered is forgotten.
func TestADialOnASilentDoorIsRefusedInTime(t *testing.T) {
	t.Parallel()
	r := startDoorRouter(t, 1)
	r.open = 200 * time.Millisecond
	control, router := net.Pipe()
	defer control.Close()
	go func() { // the run reads its answer and the open, and does nothing
		_, _ = link.DecodeWithin(control, MaxTunnelFrame)
		_, _ = link.DecodeWithin(control, MaxTunnelFrame)
	}()
	d, err := r.hub.Publish(router, "", PublishRequest{Session: "quiet"})
	if err != nil {
		t.Fatal(err)
	}
	go r.hub.Serve(d)
	dial, _ := dialDoor(t, r.addr, d.ID())
	defer dial.Close()
	_ = dial.SetReadDeadline(time.Now().Add(2 * time.Second))
	msg, err := link.DecodeWithin(dial, link.MaxPayloadSize)
	if err != nil || msg.Type != MsgJoinReply {
		t.Fatalf("the dialer read %v, %v; want a join reply refusing it", msg, err)
	}
	if !strings.Contains(string(msg.Payload), "did not open") {
		t.Fatalf("the refusal reads %s; want the door's silence named", msg.Payload)
	}
	r.hub.mu.Lock()
	waiting := len(r.hub.waiting)
	r.hub.mu.Unlock()
	if waiting != 0 {
		t.Fatalf("%d tickets wait after the dial was refused, want none", waiting)
	}
	if conn, err := net.Dial("tcp", r.addr); err == nil {
		body, _ := json.Marshal(TunnelTicket{Ticket: fmt.Sprintf("%032x", 1)})
		_ = link.NewMessage(MsgTunnelAccept, body).Encode(conn)
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, err := conn.Read(make([]byte, 1)); err != io.EOF {
			t.Fatalf("a connection back on a ticket nobody waits for read %v, want it closed", err)
		}
		_ = conn.Close()
	}
}

// TestADoorNameIsResumedOnlyByItsRun: a name is resumed by the run that was issued
// it, live or within the window of losing its control connection; a name the router
// never issued, a pod's or anyone's, is nobody's to take, and one lost longer ago
// than the window is gone.
func TestADoorNameIsResumedOnlyByItsRun(t *testing.T) {
	t.Parallel()
	r := startDoorRouter(t, 2)
	r.hub.resumeWindow = 300 * time.Millisecond
	publish := func(req PublishRequest) (net.Conn, Published) {
		t.Helper()
		conn, err := net.Dial("tcp", r.addr)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(req)
		_ = link.NewMessage(MsgSessionPublish, body).Encode(conn)
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		msg, err := link.DecodeWithin(conn, MaxTunnelFrame)
		var ans Published
		if err != nil || msg.Type != MsgSessionPublished || json.Unmarshal(msg.Payload, &ans) != nil {
			t.Fatalf("answered %v, %v", msg, err)
		}
		_ = conn.SetReadDeadline(time.Time{})
		return conn, ans
	}
	refused := func(ans Published, what string) {
		t.Helper()
		var refusal *Refusal
		if err := ans.Refused(); !errors.As(err, &refusal) || refusal.Code != RefusalIdentity {
			t.Fatalf("%s: answered %+v (%v), want a refusal of the name", what, ans, err)
		}
	}
	gone := func(id string) {
		t.Helper()
		for deadline := time.Now().Add(2 * time.Second); r.hub.Has(id); time.Sleep(5 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal("the lost door was not removed")
			}
		}
	}
	conn, ans := publish(PublishRequest{ID: "0123456789abcdef", Resume: "deadbeef"})
	_ = conn.Close()
	refused(ans, "a name never issued here")

	control, first := publish(PublishRequest{Session: "mine"})
	if err := first.Refused(); err != nil {
		t.Fatalf("published: %v", err)
	}
	_ = control.Close()
	gone(first.ID)
	control, again := publish(PublishRequest{ID: first.ID, Resume: first.Resume})
	if err := again.Refused(); err != nil || again.ID != first.ID {
		t.Fatalf("resumed as %+v (%v), want %q kept", again, err, first.ID)
	}
	conn, ans = publish(PublishRequest{ID: first.ID, Resume: "0000"})
	_ = conn.Close()
	refused(ans, "the name with another token")

	_ = control.Close()
	gone(first.ID)
	time.Sleep(400 * time.Millisecond)
	conn, ans = publish(PublishRequest{ID: first.ID, Resume: first.Resume})
	_ = conn.Close()
	refused(ans, "a name lost longer ago than the window")
}

// TestAnAddressPublishesAsManyDoorsAsItMay: one address holds at most the doors the
// router allows it, under the router's own count, and may publish again once one of
// its doors has gone; another address is counted apart.
func TestAnAddressPublishesAsManyDoorsAsItMay(t *testing.T) {
	t.Parallel()
	hub := NewTunnelHub(func(id string) string { return id }, 8, 2)
	publish := func(from string) (*PublishedDoor, error) {
		t.Helper()
		run, router := net.Pipe()
		go func() { _, _ = link.DecodeWithin(run, MaxTunnelFrame) }()
		d, err := hub.Publish(router, from, PublishRequest{})
		if err != nil {
			_ = run.Close()
		}
		return d, err
	}
	var doors []*PublishedDoor
	for i := range 2 {
		d, err := publish("10.0.0.1")
		if err != nil {
			t.Fatalf("door %d: %v", i+1, err)
		}
		doors = append(doors, d)
	}
	if _, err := publish("10.0.0.1"); !errors.Is(err, ErrDoorsPerAddress) {
		t.Fatalf("a third door from one address: %v, want the per-address refusal", err)
	}
	if _, err := publish("10.0.0.2"); err != nil {
		t.Fatalf("another address: %v", err)
	}
	hub.remove(doors[0])
	if _, err := publish("10.0.0.1"); err != nil {
		t.Fatalf("after one of its doors went: %v", err)
	}
	if hub.Count() != 3 {
		t.Fatalf("the hub carries %d doors, want 3", hub.Count())
	}
}
