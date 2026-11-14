package network

import (
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// TestASilentDialerDoesNotStallTheAcceptLoop is the defect this fixes.
//
// The handshake reads from a socket the far end controls. Run on the accept
// goroutine, one dialer that connects and never writes held every other join for a
// whole ConnectTimeout — the cheapest denial there is, one socket per five seconds
// of lobby.
func TestASilentDialerDoesNotStallTheAcceptLoop(t *testing.T) {
	t.Parallel()

	var admitted atomic.Int64
	cfg := DebugConfig(RoleHost, "127.0.0.1:0")
	cfg.ConnectTimeout = 30 * time.Second // long enough that a stall would fail the test
	cfg.AcceptSession = func(conn net.Conn) (PeerID, error) {
		// The silent dialer is the one that sends nothing: this read is what used
		// to hold the loop. A dialer that writes a byte is served immediately.
		one := make([]byte, 1)
		_ = conn.SetReadDeadline(time.Now().Add(cfg.ConnectTimeout))
		if _, err := conn.Read(one); err != nil {
			return 0, err
		}
		return PeerID(admitted.Add(1)), nil
	}

	tr := NewTransport(cfg)
	if err := tr.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = tr.Stop() }()

	addr := tr.Addr().String()
	silent, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial silent: %v", err)
	}
	defer silent.Close()

	// A talker behind it. Without the fix this connect is accepted but its
	// handshake never runs until the silent one times out.
	talker, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial talker: %v", err)
	}
	defer talker.Close()
	if _, err := talker.Write([]byte{1}); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if tr.PeerCount() == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("a talking dialer was not admitted while a silent one held a handshake")
}

// TestTheHandshakeBudgetIsBounded pins what a flood of silent dialers costs: this
// many goroutines and buffered readers, and no more. Past the budget a dial is
// refused rather than queued, because queueing would put the accept loop back
// behind a read the far end controls.
func TestTheHandshakeBudgetIsBounded(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	var inFlight, peak atomic.Int64
	cfg := DebugConfig(RoleHost, "127.0.0.1:0")
	cfg.MaxHandshakes = 2
	cfg.AcceptSession = func(net.Conn) (PeerID, error) {
		n := inFlight.Add(1)
		for {
			if p := peak.Load(); n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		<-release
		inFlight.Add(-1)
		return 0, errors.New("held")
	}

	tr := NewTransport(cfg)
	if err := tr.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = tr.Stop() }()

	addr := tr.Addr().String()
	for range 8 {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			continue // refused past the budget is the behaviour under test
		}
		defer c.Close()
	}

	time.Sleep(150 * time.Millisecond)
	if got := peak.Load(); got > int64(cfg.MaxHandshakes) {
		t.Fatalf("%d handshakes ran at once, budget is %d", got, cfg.MaxHandshakes)
	}
	close(release)
}

// TestStopDoesNotWaitOutAHandshakeDeadline covers shutdown. A handshake blocked on
// its own read deadline would otherwise add that deadline to the time between a
// signal and the process exiting.
func TestStopDoesNotWaitOutAHandshakeDeadline(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 1)
	cfg := DebugConfig(RoleHost, "127.0.0.1:0")
	cfg.ConnectTimeout = time.Minute
	cfg.AcceptSession = func(conn net.Conn) (PeerID, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		// No deadline of its own: only Stop closing the connection ends this read.
		_, err := conn.Read(make([]byte, 1))
		return 0, err
	}

	tr := NewTransport(cfg)
	if err := tr.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	c, err := net.Dial("tcp", tr.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the handshake never started")
	}

	done := make(chan struct{})
	go func() { _ = tr.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop waited on a handshake blocked in its read")
	}
}

// TestAStartGateConfirmationBelongsToOneLink pins what a confirmation names. Keyed
// by participant, a peer repeating MsgReady cannot satisfy a gate waiting on
// another; dropped with the link, an identity that returns to the pool has to
// install its own world before the next gate admits it.
func TestAStartGateConfirmationBelongsToOneLink(t *testing.T) {
	t.Parallel()

	port := NewSocketPort(testConfigFor(RoleHost, "127.0.0.1:0"))
	defer func() { _ = port.Close() }()

	port.Inject(2, uint8(testReady), nil)
	if !port.Confirmed(2) {
		t.Fatal("the peer that confirmed is not recorded")
	}
	if port.Confirmed(3) {
		t.Fatal("a peer that sent nothing counts as confirmed")
	}
	// A released identity dials back with its own world to install, so the
	// confirmation goes with the link rather than with the participant number.
	port.onDisconnect(2)
	if port.Confirmed(2) {
		t.Fatal("a departed peer's confirmation outlived its link")
	}
}

// TestBindPeerFallsBackWhenThePortIsTaken is the collision two guests on one
// machine hit.
func TestBindPeerFallsBackWhenThePortIsTaken(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer held.Close()

	ln, err := BindPeer(held.Addr().String())
	if err != nil {
		t.Fatalf("bind fell over instead of falling back: %v", err)
	}
	defer ln.Close()
	if ln.Addr().String() == held.Addr().String() {
		t.Fatal("two listeners bound the same address")
	}
}

// TestADoorStartsTheLinkHistoryOverOnlyWhereNobodyCouldDial: a host's port at no door
// reads no link once its last stream has gone, and the door Serve opens there waits
// for its first joiner as a fresh host's does. A stream that stands as the door opens,
// or links while it opens, keeps the history, so its going reads as a link that went
// down; and so does a second door, opened on a port that already listened.
func TestADoorStartsTheLinkHistoryOverOnlyWhereNobodyCouldDial(t *testing.T) {
	for _, tt := range []struct {
		name string
		open func(t *testing.T, port *SocketPort, link func() (release func()), serve func())
		want ConnState
	}{
		{"its stream gone before the door", func(t *testing.T, port *SocketPort, link func() func(), serve func()) {
			link()()
			linked(t, port, 1)
			port.Disconnect(2)
			linked(t, port, 0)
			if got := port.ConnectionState(); got != StateIdle {
				t.Fatalf("a host at no door whose stream went reads %d, want idle", got)
			}
			serve()
		}, StateConnecting},
		{"its stream standing at the door", func(t *testing.T, port *SocketPort, link func() func(), serve func()) {
			link()()
			linked(t, port, 1)
			serve()
		}, StateDisconnected},
		{"its stream linking as the door opens", func(t *testing.T, port *SocketPort, link func() func(), serve func()) {
			release := link()
			// Held here, Serve stands where it has yet to read the links: the stream
			// registers there, before the history could start over
			port.transport.listenMu.Lock()
			served := make(chan struct{})
			go func() { serve(); close(served) }()
			time.Sleep(20 * time.Millisecond)
			release()
			linked(t, port, 1)
			port.transport.listenMu.Unlock()
			<-served
		}, StateDisconnected},
		{"a second door", func(t *testing.T, port *SocketPort, link func() func(), serve func()) {
			serve()
			link()()
			linked(t, port, 1)
			port.Disconnect(2)
			linked(t, port, 0)
			serve()
		}, StateDisconnected},
	} {
		t.Run(tt.name, func(t *testing.T) {
			port := NewSocketPort(testConfigFor(RoleHost, ""))
			if err := port.Start(); err != nil {
				t.Fatalf("start: %v", err)
			}
			defer port.Close()
			link := func() (release func()) {
				gate := make(chan struct{})
				near, far := Pipe()
				t.Cleanup(func() { _ = far.Close() })
				if err := port.ServeConn(near, func(net.Conn) (PeerID, error) { <-gate; return 2, nil }, nil); err != nil {
					t.Fatalf("link: %v", err)
				}
				return func() { close(gate) }
			}
			serve := func() {
				ln, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Errorf("listen: %v", err)
					return
				}
				if err := port.Serve(ln); err != nil {
					t.Errorf("serve: %v", err)
				}
			}
			tt.open(t, port, link, serve)
			if port.PeerCount() > 0 {
				port.Disconnect(2)
				linked(t, port, 0)
			}
			if got := port.ConnectionState(); got != tt.want {
				t.Fatalf("the door, with nobody linked since, reads %d, want %d (connecting %d is waiting, disconnected %d is down)",
					got, tt.want, StateConnecting, StateDisconnected)
			}
		})
	}
}

// linked waits for the port to hold n links.
func linked(t *testing.T, port *SocketPort, n int) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); port.PeerCount() != n; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the port holds %d links, want %d", port.PeerCount(), n)
		}
	}
}
