package network

import (
	"crypto/tls"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// acceptRetryDelay paces the accept loop after a transient failure such as a
// descriptor limit, so the goroutine cannot busy-spin on a listener that keeps
// failing without being closed.
const acceptRetryDelay = 10 * time.Millisecond

// Transport handles network I/O for a specific role
type Transport struct {
	config   *Config
	listener net.Listener
	peers    *PeerManager

	running atomic.Bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewTransport creates a transport with the given configuration
func NewTransport(cfg *Config) *Transport {
	return &Transport{
		config: cfg,
		peers:  NewPeerManager(cfg),
		stopCh: make(chan struct{}),
	}
}

// SetHandlers configures message and connection callbacks
func (t *Transport) SetHandlers(
	onConnect func(PeerID),
	onDisconnect func(PeerID),
	onMessage func(PeerID, *Message),
) {
	t.peers.SetHandlers(onConnect, onDisconnect, onMessage)
}

// Start begins listening (server) or connecting (client)
func (t *Transport) Start() error {
	if !t.running.CompareAndSwap(false, true) {
		return nil // Already running
	}

	switch t.config.Role {
	case RoleHost:
		return t.startServer()
	case RolePeer:
		return t.startClient()
	default:
		return nil // RoleNone, no-op
	}
}

// startServer binds and accepts connections
func (t *Transport) startServer() error {
	var ln net.Listener
	var err error

	if t.config.TLS != nil {
		ln, err = tls.Listen("tcp", t.config.Address, t.config.TLS)
	} else {
		ln, err = net.Listen("tcp", t.config.Address)
	}

	if err != nil {
		t.running.Store(false)
		return err
	}

	t.listener = ln

	t.wg.Add(1)
	go t.acceptLoop()

	return nil
}

// acceptLoop handles incoming connections
func (t *Transport) acceptLoop() {
	defer t.wg.Done()

	for {
		select {
		case <-t.stopCh:
			return
		default:
		}

		conn, err := t.listener.Accept()
		if err != nil {
			select {
			case <-t.stopCh:
				return
			default:
			}
			// A closed listener never recovers; anything else is reported and
			// paced, so a persistent accept failure cannot spin this goroutine.
			if errors.Is(err, net.ErrClosed) {
				return
			}
			t.report(err)
			select {
			case <-t.stopCh:
				return
			case <-time.After(acceptRetryDelay):
			}
			continue
		}

		// A participant is admitted only under the identity the session assigned it.
		// Accept order is not an identity: the barrier's per-source epoch window and
		// every roster lookup are keyed by canonical participant ID, so admitting a
		// stream under a connection-local number would corrupt both silently.
		if t.config.AcceptSession == nil {
			_ = conn.Close()
			t.report(errors.New("transport: host accepted a connection with no session handshake"))
			continue
		}
		id, err := t.config.AcceptSession(conn)
		if err != nil {
			_ = conn.Close()
			t.report(err)
			continue
		}
		if _, err := t.peers.AddConnectionAs(conn, id); err != nil {
			t.report(err)
			continue
		}
		if t.config.OnAdmit != nil {
			t.config.OnAdmit(id)
		}
	}
}

// startClient connects to server
func (t *Transport) startClient() error {
	conn, id := t.config.preconnected, t.config.preconnectedPeer
	if conn == nil {
		var err error
		if conn, err = dial(t.config.Address, t.config); err != nil {
			t.running.Store(false)
			return err
		}
	}
	// The coordinator's identity comes from the offer this stream already accepted;
	// a client that reached here without one has no session to join.
	if id == 0 {
		_ = conn.Close()
		t.running.Store(false)
		return errors.New("transport: peer has no coordinator identity from the join handshake")
	}
	if _, err := t.peers.AddConnectionAs(conn, id); err != nil {
		_ = conn.Close()
		t.running.Store(false)
		return err
	}
	return nil
}

// Disconnect drops one peer, reporting whether it was connected.
func (t *Transport) Disconnect(id PeerID) bool { return t.peers.Disconnect(id) }

// Addr returns the bound listener address, nil for a client or before Start.
func (t *Transport) Addr() net.Addr {
	if t.listener == nil {
		return nil
	}
	return t.listener.Addr()
}

// report publishes an asynchronous accept or handshake failure.
func (t *Transport) report(err error) {
	if t.config.OnError != nil {
		t.config.OnError(err)
	}
}

// Stop halts the transport
func (t *Transport) Stop() error {
	if !t.running.CompareAndSwap(true, false) {
		return nil
	}

	close(t.stopCh)

	if t.listener != nil {
		t.listener.Close()
	}

	t.peers.Close()
	t.wg.Wait()

	return nil
}

// Send transmits to a specific peer
func (t *Transport) Send(id PeerID, msg *Message) bool {
	return t.peers.Send(id, msg)
}

// BroadcastExcept sends to all peers but one and returns the number that refused.
func (t *Transport) BroadcastExcept(exclude PeerID, msg *Message) int {
	return t.peers.BroadcastExcept(exclude, msg)
}

// PeerCount returns connected peer count
func (t *Transport) PeerCount() int {
	return t.peers.PeerCount()
}

// IsRunning returns transport state
func (t *Transport) IsRunning() bool {
	return t.running.Load()
}
