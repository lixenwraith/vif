package network

import (
	"crypto/tls"
	"time"
)

// Role defines the network topology role
type Role uint8

const (
	RoleNone   Role = iota // Network disabled
	RoleClient             // Connects to server
	RoleServer             // Accepts connections
	RoleHost               // P2P: hosting peer
	RolePeer               // P2P: joining peer
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
}

// DefaultConfig returns production-safe defaults
func DefaultConfig() *Config {
	return &Config{
		Role:              RoleNone,
		Address:           ":7777",
		TLS:               nil, // Must be explicitly configured for production
		MaxPeers:          16,
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