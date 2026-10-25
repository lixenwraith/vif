package app

import (
	"encoding/json"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	link "github.com/lixenwraith/vif/pkg/network"
)

// routerStandIn is a front door for doors alone, as vif-router places them: a run's
// publication becomes a control connection, a dial on a door's name is spliced to
// the connection the run opens back for it.
func routerStandIn(t *testing.T) (string, *network.TunnelHub) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	addr := ln.Addr().String()
	hub := network.NewTunnelHub(func(id string) string { return "vif://" + addr + "/" + id }, 4)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				first, err := link.DecodeWithin(conn, network.MaxTunnelFrame)
				if err != nil {
					_ = conn.Close()
					return
				}
				switch first.Type {
				case network.MsgSessionPublish:
					var req network.PublishRequest
					_ = json.Unmarshal(first.Payload, &req)
					if d, err := hub.Publish(conn, req); err == nil {
						hub.Serve(d)
					}
				case network.MsgTunnelAccept:
					var ticket network.TunnelTicket
					_ = json.Unmarshal(first.Payload, &ticket)
					if !hub.Accept(conn, ticket.Ticket) {
						_ = conn.Close()
					}
				case network.MsgSessionRoute:
					upstream, session, err := hub.Open(string(first.Payload), parameter.NetworkTunnelOpenTimeout)
					if err != nil {
						network.RefuseJoin(conn, err, time.Second)
						_ = conn.Close()
						return
					}
					_ = link.NewMessage(network.MsgSessionRoute, []byte(session)).Encode(upstream)
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
			}()
		}
	}()
	return addr, hub
}

// TestARelayPublishesItsDoorThroughTheRouter: a guest with -relay vif://<router>
// publishes its door through the router rather than binding one, the chain names the
// link the router gave it, and a person who dials that link joins through the guest,
// the router placing the dial on the connection the guest opened back.
func TestARelayPublishesItsDoorThroughTheRouter(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	addr := freeAddress(t)
	host, _ := driveBot(t, Config{HostAddress: addr, Bots: []string{"roam"}}, "patrol", nil)
	waitForCursors(t, host, 2)
	router, hub := routerStandIn(t)
	relay, _ := driveBot(t, Config{JoinAddress: addr, RelayAddress: "vif://" + router}, "patrol", nil)
	waitForCursors(t, host, 3)
	relayID := link.PeerID(relay.membership.localParticipant())
	var door string
	waitUntil(t, func() bool {
		d, ok := host.authority.Chain().Door(relayID)
		door = d
		return ok
	}, "the host's chain naming the relay's door")
	e, err := link.ParseEndpoint(door)
	if err != nil || e.Addr != router || !hub.Has(e.Name) {
		t.Fatalf("the relay's door is %q (%v); want a link at the router naming a door it carries", door, err)
	}
	person, _ := driveBot(t, Config{JoinAddress: e.Addr, SessionName: e.Name}, "roam", nil)
	for _, a := range []*App{host, relay, person} {
		waitForCursors(t, a, 4)
	}
	if person.membership.joinedThrough != relayID || person.membership.sessionTransport().PeerCount() != 1 {
		t.Fatalf("the person joined through participant %d holding %d links; want through the relay, on its one link",
			person.membership.joinedThrough, person.membership.sessionTransport().PeerCount())
	}
	waitForMotion(t, host, cursorOf(t, host, person.membership.localParticipant()), "the person's cursor on the host")
}
