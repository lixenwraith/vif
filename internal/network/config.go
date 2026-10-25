package network

import (
	"github.com/lixenwraith/vif/internal/parameter"
	link "github.com/lixenwraith/vif/pkg/network"
)

// Config is a session endpoint's configuration: the link's, and what a dial brings
// to the door beside it.
type Config struct {
	link.Config

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
}

// SessionRole is a participant's protocol role from what it is, not how it
// connected: the author hosts, one with participants behind it relays, others are
// peers.
func SessionRole(authoring bool, behind int) link.Role {
	switch {
	case authoring:
		return link.RoleHost
	case behind > 0:
		return link.RoleRelay
	default:
		return link.RolePeer
	}
}

// DefaultConfig is the session's defaults: the link's, the conventional port, the
// timing parameter sets and the kinds a session exchanges.
func DefaultConfig() *Config {
	cfg := &Config{Config: *link.DefaultConfig()}
	cfg.Address = ":7777"
	cfg.ReadTimeout = parameter.NetworkLinkDeadAfter
	cfg.ProbeInterval = parameter.NetworkProbeInterval
	cfg.ProbeLostAfter = parameter.NetworkProbeLostAfter
	cfg.WriteLinger = parameter.NetworkWriteLinger
	cfg.StreamLevel = parameter.NetworkStreamLevel
	cfg.TickInterval = parameter.GameUpdateInterval
	cfg.Kinds = kinds
	return cfg
}

// DebugConfig returns the defaults for one role at one address.
func DebugConfig(role link.Role, addr string) *Config {
	cfg := DefaultConfig()
	cfg.Role = role
	cfg.Address = addr
	return cfg
}

// NewMesh returns an empty in-process link graph on the session's timing.
func NewMesh() *link.Mesh { return link.NewMesh(&DefaultConfig().Config) }

// NewLoopbackPair links two participants on a mesh of their own and returns their
// endpoints: the two-participant case is a mesh with one link.
func NewLoopbackPair(a, b link.PeerID) (*link.MeshPort, *link.MeshPort) {
	m := NewMesh()
	m.Link(a, b)
	return m.Node(a), m.Node(b)
}

// RequestSession asks site's allocator for a new session and returns the link this
// build joins it by. players and scenario choose from what the site advertises,
// zero for its defaults; a refusal, an unauthorised one included, is its message.
func RequestSession(site string, players int, scenario string) (string, error) {
	return link.RequestSession(site, players, scenario, parameter.NetworkSessionRequestTimeout)
}
