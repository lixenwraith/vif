package network

import (
	"crypto/tls"
	"net"
	"time"
)

// Role defines the network topology role
type Role uint8

const (
	RoleNone Role = iota // Network disabled
	RoleHost             // Binds and accepts participants
	RolePeer             // Dials the coordinator
	// RoleRelay forwards the authority's artifacts to the participants behind it and
	// answers their selective requests from what it retained. It is a session role,
	// not a transport mode: the transport dials as RolePeer, and the session layer's
	// SessionRole is what the protocol reads.
	RoleRelay
)

// Config is a link endpoint's configuration: how it listens or dials, its limits
// and timing, the handshakes it runs and the kinds it carries. The session layer
// fills the timing and the kinds.
type Config struct {
	// Role determines connection behavior
	Role Role

	// Address to bind (server) or connect to (client)
	Address string

	// TLS is the wss route's: the certificate a wss:// listener serves, and what a
	// wss:// dial trusts in place of the system's roots and a self-signed door taken
	// as it is. Nil serves no wss. TCP and ws stay plaintext whatever it holds: a
	// session is trusted-peer by decision.
	TLS *tls.Config

	// Connection limits
	MaxPeers int

	// MaxHandshakes bounds the accepted connections whose session handshake may be
	// in flight at once. Handshakes run off the accept path, as each reads a socket
	// the far end controls; this bounds what a flood of silent dialers costs.
	MaxHandshakes int

	// ParticipantID is this end's identity, agreed by the session's handshake.
	ParticipantID PeerID

	// Timing. ReadTimeout bounds the wait for each frame a stream carries after the
	// offer: the far end streams or probes it, so a stream that quiet has stopped.
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration

	// ProbeInterval is how often a port probes each link, off the game clock, and
	// ProbeLostAfter how long one may go unanswered before each further probe is
	// charged as lost; zero probes nothing, or charges none. WriteLinger gathers a
	// burst before one flush; StreamLevel is a link stream's deflate level, zero
	// storing its frames; TickInterval is the game tick the mesh's clock counts in.
	ProbeInterval  time.Duration
	ProbeLostAfter time.Duration
	WriteLinger    time.Duration
	StreamLevel    int
	TickInterval   time.Duration

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
	// bind a port of its own, so the session can reach it after a handoff. Its port
	// runs AcceptSession too, for a join, which the opening frame tells from a peer
	// link: a successor takes players where the session reaches it. Empty leaves a
	// participant a leaf.
	ListenAddress string
	AcceptPeer    func(net.Conn) (PeerID, error)

	// ConnectPeer is the dial side of the peer-link handshake, run on the stream
	// DialPeer opened as local; it answers the far end's identity. Nil dials none.
	ConnectPeer func(conn net.Conn, local PeerID) (PeerID, error)

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

	// Preconnected is a stream already open to the far end, admitted as
	// PreconnectedPeer in place of a dial: a join's, once seated.
	Preconnected     net.Conn
	PreconnectedPeer PeerID

	// Kinds is the session's table over the kind byte: how each kind's frames travel
	// and are counted, and the kinds the link acts on itself.
	Kinds Kinds
}

// DefaultConfig is the link's defaults: no role or address, the limits, buffers and
// queues, five seconds to connect or write, and none of the timing or the kinds a
// session fills in.
func DefaultConfig() *Config {
	return &Config{
		Role:            RoleNone,
		MaxPeers:        16,
		MaxHandshakes:   8,
		ConnectTimeout:  5 * time.Second,
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
