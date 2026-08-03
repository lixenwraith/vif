package network

import (
	"crypto/tls"
	"net"
	"sync"
	"sync/atomic"
)

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
	case RoleServer, RoleHost:
		return t.startServer()
	case RoleClient, RolePeer:
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
				continue
			}
		}

		t.peers.AddConnection(conn)
	}
}

// startClient connects to server
func (t *Transport) startClient() error {
	conn, err := dial(t.config.Address, t.config)
	if err != nil {
		t.running.Store(false)
		return err
	}

	_, err = t.peers.AddConnection(conn)
	if err != nil {
		conn.Close()
		t.running.Store(false)
		return err
	}

	return nil
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

// Broadcast sends to all peers
func (t *Transport) Broadcast(msg *Message) {
	t.peers.Broadcast(msg)
}

// PeerCount returns connected peer count
func (t *Transport) PeerCount() int {
	return t.peers.PeerCount()
}

// IsRunning returns transport state
func (t *Transport) IsRunning() bool {
	return t.running.Load()
}