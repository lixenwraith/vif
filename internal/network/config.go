package network

import (
	"crypto/tls"
	"net"
	"time"

	"github.com/lixenwraith/vif/internal/parameter"
)

// Role defines the network topology role
type Role uint8

const (
	RoleNone Role = iota // Network disabled
	RoleHost             // Binds and accepts participants
	RolePeer             // Dials the coordinator
	// RoleRelay forwards the authority's artifacts to the participants behind it and
	// answers their selective requests from what it retained. It is a session role,
	// not a transport mode: the transport dials as RolePeer, and SessionRole is what
	// the protocol reads.
	RoleRelay
)

// SessionRole is a participant's protocol role from what it is, not how it
// connected: the author hosts, one with participants behind it relays, others are
// peers.
func SessionRole(authoring bool, behind int) Role {
	switch {
	case authoring:
		return RoleHost
	case behind > 0:
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

	// TLS is the wss route's: the certificate a wss:// listener serves, and what a
	// wss:// dial trusts in place of the system's roots. Nil serves no wss. TCP and
	// ws stay plaintext whatever it holds: a session is trusted-peer by decision.
	TLS *tls.Config

	// Connection limits
	MaxPeers int

	// MaxHandshakes bounds the accepted connections whose session handshake may be
	// in flight at once. Handshakes run off the accept path, as each reads a socket
	// the far end controls; this bounds what a flood of silent dialers costs.
	MaxHandshakes int

	// Session identity is agreed by the join handshake.
	ParticipantID PeerID

	// SessionName is what a dialer was told it is joining, sent as the first frame
	// of every join so a front door can place the connection on an address that
	// serves several sessions. Empty dials an address that serves one.
	SessionName string

	// Claims are the tokens a dial names when the coordinator asks which places it
	// can come back to: one a Wraith keeps a place under is offered that place.
	Claims []string

	// Identity is the build half of what this peer is (wire contract, manifest and
	// layout schemas), compared against the offer before a world is built from it.
	// A zero Protocol disables the check, so a harness can dial a hand-built fixture.
	Identity PeerIdentity

	// Timing. ReadTimeout bounds the wait for each frame a stream carries after the
	// offer: the far end streams or probes it, so a stream that quiet has stopped.
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration

	// Buffer sizes
	ReadBufferSize  int
	WriteBufferSize int
	SendQueueSize   int
	RecvQueueSize   int

	// AcceptSession authenticates and assigns a canonical participant ID before
	// an accepted stream reaches the poll endpoint. It runs on a goroutine of the
	// connection's own, bounded by MaxHandshakes, so an implementation may block on
	// the stream for as long as its own deadline allows without holding the accept
	// loop.
	AcceptSession func(net.Conn) (PeerID, error)

	// ListenAddress and AcceptPeer let a participant that is not the coordinator
	// bind a port of its own, so the session can reach it after a handoff (reach.go).
	// Its port runs AcceptSession too, for a join, which the opening frame tells
	// from a peer link: a successor takes players where the session reaches it.
	// Empty leaves a participant a leaf.
	ListenAddress string
	AcceptPeer    func(net.Conn) (PeerID, error)

	// PreboundListener is a port the caller bound, served instead of ListenAddress or
	// a host's Address: the bound address is declared before the transport exists.
	PreboundListener net.Listener

	// OnAdmit runs once a join this transport accepted is a peer, on the accept
	// goroutine, and sends a mid-run join its start gate and capture. It is apart
	// from AcceptSession because the peer must receive crossings before its world is
	// read, or the epochs between the two are lost to it.
	OnAdmit func(PeerID)

	OnError func(error)

	// Dial replaces dialling the address: a guest's bot is handed one end of an
	// in-process stream whose other end its holder serves through ServeConn, since
	// the two share a process and a fate.
	Dial func() (net.Conn, error)

	preconnected     net.Conn
	preconnectedPeer PeerID
}

// DefaultConfig returns production-safe defaults
func DefaultConfig() *Config {
	return &Config{
		Role:            RoleNone,
		Address:         ":7777",
		MaxPeers:        16,
		MaxHandshakes:   8,
		ConnectTimeout:  5 * time.Second,
		ReadTimeout:     parameter.NetworkLinkDeadAfter,
		WriteTimeout:    5 * time.Second,
		ReadBufferSize:  64 * 1024,
		WriteBufferSize: 64 * 1024,
		SendQueueSize:   256,
		RecvQueueSize:   256,
	}
}

// DebugConfig returns the defaults for one role at one address.
func DebugConfig(role Role, addr string) *Config {
	cfg := DefaultConfig()
	cfg.Role = role
	cfg.Address = addr
	return cfg
}
