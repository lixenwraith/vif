package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"time"

	"github.com/lixenwraith/vif/internal/network"
)

const (
	// gamePort is the session's own listener, the one its Service also targets.
	gamePort = "7777"
	// A dialer writes its route frame before it reads, so this bounds a connection
	// that opened and said nothing, the lookup behind it, and a refusal left unread.
	routeTimeout = 5 * time.Second
	// routePlacing bounds the connections still being placed, as MaxHandshakes does
	// in a session: past it a dial is refused rather than queued behind silent ones.
	routePlacing = 32
	// Once one direction of a splice has ended the other gets this long, so a peer
	// that vanished without a FIN does not hold its session's ceiling for minutes.
	spliceLinger = 10 * time.Second
	// spliceStall bounds one write, so a peer that stopped reading ends its splice
	// rather than holding it, and its session's ceiling, while TCP retransmits.
	spliceStall = 10 * time.Second
)

var (
	errRouteBusy      = errors.New("this session is carrying as many connections as it accepts")
	errRouteAdmission = errors.New("this address has joined too often; try again in a minute")
)

// tcpRouter is the native front door: one port for every session, which places a
// connection by the name its dialer sends before reading. It reads that one frame
// and nothing after it; see doc/kubernetes-fleet.md §9.
type tcpRouter struct {
	sessions sessionAllocator
	holds    *holds
	admit    *network.AdmissionLimiter
	log      *slog.Logger
	game     string // the pod port dialled; a test points it at a stand-in
	placing  chan struct{}
}

func newTCPRouter(sessions sessionAllocator, held *holds, joins *network.AdmissionLimiter, logger *slog.Logger) *tcpRouter {
	return &tcpRouter{sessions: sessions, holds: held, admit: joins,
		log: logger, game: gamePort, placing: make(chan struct{}, routePlacing)}
}

// serve accepts until the listener is closed.
func (r *tcpRouter) serve(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			// Descriptor exhaustion and its kin pass; spinning on them would not.
			r.log.Warn("session route accept failed", "error", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		select {
		case r.placing <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}
		go func() {
			upstream, release := r.place(conn)
			<-r.placing
			if upstream != nil {
				defer release()
				splice(conn, upstream)
			}
		}()
	}
}

// place answers the route frame with the session's pod, the frame replayed to it,
// or with a refusal the dialer reads as its reason. Every refusal precedes a dial.
func (r *tcpRouter) place(client net.Conn) (net.Conn, func()) {
	_ = client.SetDeadline(time.Now().Add(routeTimeout))
	id, err := network.ReadSessionRoute(client)
	if err != nil {
		// Not a vif dialer, or one that said nothing: no reply would be read.
		_ = client.Close()
		return nil, nil
	}
	refuse := func(reason error) (net.Conn, func()) {
		network.RefuseJoin(client, reason, routeTimeout)
		_ = client.Close()
		return nil, nil
	}
	if !sessionIDPattern.MatchString(id) {
		return refuse(errSessionUnknown)
	}
	// Keyed on the dialler, whose address the host forwards unchanged, and charged
	// before the lookup: that costs the API server a list and the pod a probe. The
	// browser route charges the same budget.
	if r.admit.Admit(client.RemoteAddr()) != nil {
		return refuse(errRouteAdmission)
	}
	lookup, cancel := context.WithTimeout(context.Background(), routeTimeout)
	podIP, err := r.sessions.routeSession(lookup, id)
	cancel()
	if err != nil {
		if !errors.Is(err, errSessionUnknown) && !errors.Is(err, errSessionRefusing) &&
			!errors.Is(err, errSessionUnroutable) {
			r.log.Error("route session", "session", id, "error", err)
			err = errors.New("could not resolve the session")
		}
		return refuse(err)
	}
	release, ok := r.holds.hold(id)
	if !ok {
		return refuse(errRouteBusy)
	}
	upstream, err := net.DialTimeout("tcp", net.JoinHostPort(podIP, r.game), 2*time.Second)
	if err == nil {
		// Re-encoded rather than copied: the pod reads the name, and nothing else a
		// stranger put in that header reaches it.
		_ = upstream.SetWriteDeadline(time.Now().Add(routeTimeout))
		if err = network.NewMessage(network.MsgSessionRoute, []byte(id)).Encode(upstream); err != nil {
			_ = upstream.Close()
		}
		_ = upstream.SetWriteDeadline(time.Time{})
	}
	if err != nil {
		release()
		r.log.Warn("session route dial failed", "session", id, "error", err)
		return refuse(errSessionUnroutable)
	}
	_ = client.SetDeadline(time.Time{})
	r.log.Info("session route opened", "session", id)
	return upstream, release
}

// splice copies both ways, passing each side's end on as a half-close, and closes
// both once both directions are done.
func splice(client, upstream net.Conn) {
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		_, _ = io.Copy(stallBounded{dst}, src)
		if half, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = half.CloseWrite()
		}
		done <- struct{}{}
	}
	go pipe(upstream, client)
	go pipe(client, upstream)
	<-done
	linger := time.Now().Add(spliceLinger)
	_ = client.SetReadDeadline(linger)
	_ = upstream.SetReadDeadline(linger)
	<-done
	_ = client.Close()
	_ = upstream.Close()
}

// stallBounded renews the write deadline before each write; see spliceStall.
type stallBounded struct{ net.Conn }

func (s stallBounded) Write(p []byte) (int, error) {
	_ = s.SetWriteDeadline(time.Now().Add(spliceStall))
	return s.Conn.Write(p)
}
