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
)

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
