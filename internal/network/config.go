package network

import (
	"crypto/tls"
	"net"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// Role defines the network topology role
type Role uint8

const (
	RoleNone Role = iota // Network disabled
	RoleHost             // Binds and accepts participants
	RolePeer             // Dials the coordinator
	// RoleRelay is a participant that forwards the authority's artifacts to the
	// participants behind it and retains what it forwards, so it can answer their
	// selective requests from its own retention rather than leaving them on the
	// whole-body flood.
	//
	// It is a *session* role rather than a transport mode, which is the whole point
	// of it: nothing about the flood changes, nothing about who authors changes,
	// and the transport still dials or binds exactly as it did. What changes is
	// that a relayed participant has somewhere to send a request that can answer
	// it. The transport therefore treats it as RolePeer; SessionRole below is what
	// the protocol reads.
	RoleRelay
)

// SessionRole is the role a participant holds in the protocol, as a function of
// what it is rather than of how it connected: the instance authoring is the host,
// a participant with more than one link is relaying for the ones behind it, and
// everything else is a peer.
func SessionRole(authoring bool, links int) Role {
	switch {
	case authoring:
		return RoleHost
	case links > 1:
		return RoleRelay
	default:
		return RolePeer
	}
}

// Config holds network configuration
type Config struct {
	// Role determines connection behavior
	Role Role

	// Address to bind (server) or connect to (client)
	Address string

	// TLS configuration (nil = plaintext, debug only)
	TLS *tls.Config

	// Connection limits
	MaxPeers int

	// Session identity and fixed playout delay are agreed by the join handshake.
	ParticipantID     PeerID
	BarrierDelayTicks uint64

	// Timing
	ConnectTimeout    time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	HeartbeatInterval time.Duration
	DisconnectTimeout time.Duration

	// Buffer sizes
	ReadBufferSize  int
	WriteBufferSize int
	SendQueueSize   int
	RecvQueueSize   int

	// AcceptSession authenticates and assigns a canonical participant ID before
	// an accepted stream reaches the poll endpoint.
	AcceptSession func(net.Conn) (PeerID, error)

	// OnAdmit is called once an accepted stream has become a peer, on the accept
	// goroutine. It is where a mid-run join sends its start gate and its capture,
	// and the ordering is the reason it exists rather than being folded into
	// AcceptSession: a participant has to be receiving this instance's crossings
	// before the world it is about to install is read, or the epochs produced
	// between the two are lost to it and to nothing else.
	OnAdmit func(PeerID)

	OnError func(error)

	preconnected     net.Conn
	preconnectedPeer PeerID
}

// DefaultConfig returns production-safe defaults
func DefaultConfig() *Config {
	return &Config{
		Role:              RoleNone,
		Address:           ":7777",
		TLS:               nil, // Must be explicitly configured for production
		MaxPeers:          16,
		BarrierDelayTicks: parameter.NetworkBarrierDelayTicks,
		ConnectTimeout:    5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      5 * time.Second,
		HeartbeatInterval: 10 * time.Second,
		DisconnectTimeout: 30 * time.Second,
		ReadBufferSize:    64 * 1024,
		WriteBufferSize:   64 * 1024,
		SendQueueSize:     256,
		RecvQueueSize:     256,
	}
}

// DebugConfig returns config with TLS disabled for local testing
func DebugConfig(role Role, addr string) *Config {
	cfg := DefaultConfig()
	cfg.Role = role
	cfg.Address = addr
	cfg.TLS = nil
	return cfg
}
