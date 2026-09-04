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
