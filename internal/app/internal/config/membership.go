package config

import (
	"crypto/tls"
	"strings"

	"github.com/lixenwraith/vif/internal/converge"
	link "github.com/lixenwraith/vif/pkg/network"
)

// Membership is the session half of Config: the session this run hosts or joins and
// the rules it opens one with, how the run is reached and relays for others, the bots
// it seats and the keys it comes back with. It is all of Config the membership holds.
type Membership struct {
	// HostAddress is the address this run hosts a session on. An interactive run
	// opens it once its clock runs, as :host does; a scripted, bot-driven or
	// dedicated host holds tick zero in a lobby until lobbyQuorum guests pass the
	// start gate.
	HostAddress string

	// TLS is the certificate a wss:// door serves, -cert and -key or the one -selfsign
	// made for the run; nil opens none. It is the run's, so a door :host opens later
	// serves it too. Trust is what a wss:// dial trusts a host by, -ca; nil is the
	// system's roots.
	TLS, Trust *tls.Config

	// JoinAddress is the session this run joins, at startup or through :join, at
	// whatever tick the host has reached. The host's offer supplies simulation
	// identity before the App is constructed.
	JoinAddress string

	// Watch makes the run a viewer of the session JoinAddress names: it holds no
	// place there, drives no cursor and sends nothing, and runs from the feed that
	// session's host copies to everyone watching it (doc/multi-player.md §5.8).
	// Follow is the slot it looks from first, the next one played while that is empty.
	Watch  bool
	Follow uint8

	// SessionName is what one address calls this session, so a deployment can put
	// several behind one. A host answers to it and refuses a dial that named
	// another; a joiner sends it before the handshake, where a front door can read
	// it. Empty on both is an address with one session on it.
	SessionName string

	// ListenAddress pins the port this participant is dialled back on in a migrate
	// session, and has it listen there whatever [play] takeover says. Without it a
	// run that may take over binds the coordinator's own port, one firewall rule for
	// a session, else an OS-assigned one; a bind that fails leaves it a leaf.
	ListenAddress string

	// RelayAddress is where a joined run admits participants that cannot reach the
	// host, carrying their artifacts on its own link: an address to bind, or, with the
	// vif:// scheme kept, a router to publish the door through (RelayRouter). Empty
	// relays only its bots.
	RelayAddress string

	// Migrate lets a session this run opens move to a guest that allowed it when
	// the run goes, rather than end there: -authority migrate, or :host's word. Off
	// unless asked for, since a migrating session shares such a guest's address. It
	// is a session property: the offer carries it (SessionOffer.FixedAuthority, its
	// negation). See doc/multi-player.md §5.
	Migrate bool

	// SlowPolicy is when this run, while authoring, evicts a participant that cannot
	// keep up, and ShedPolicy what it drops when it cannot keep its own tick; nil
	// keeps the default. See converge.SlowPolicy and converge.ShedPolicy.
	SlowPolicy *converge.SlowPolicy
	ShedPolicy *converge.ShedPolicy

	// Participants is a ceiling on the roster's cursors, a playing host's own
	// included; zero means the whole roster. On a scripted or bot-driven host an
	// explicit value is also what the startup lobby waits for. Unset, and always on
	// a dedicated host, the lobby starts on its first guest and the rest arrive
	// through the mid-run gate; an interactive host has no lobby.
	Participants int

	// Spectators is how many viewers a session this run hosts feeds at once: zero is
	// parameter.FeedAudience and a negative count none.
	Spectators int

	// Bots names the graph of each bot seat this run holds, one entry a bot. Seats
	// join the session the run is in, and a run in none opens one with no listener.
	Bots []string

	// Passphrase is what this run's own doors take, -passphrase or :host's, derived
	// once into the credential every door serves, and what its dials show a door
	// that takes one, :join's. Empty opens its doors and shows none.
	Passphrase string

	// TicketKeys are the router's public keys a dedicated host's doors take the site's
	// tickets under (-admit ticket), in place of a bare key; none admits keys as any
	// door does.
	TicketKeys [][]byte

	// SignInStore keeps the sign-ins this process holds, one a site, past its life, a
	// file for the user alone; nil for a page, whose browser keeps the cookie, and
	// for a run that keeps none.
	SignInStore PlaceStore

	// PlaceStore keeps the places this process comes back to its sessions with past
	// its own life: a page's across a reload, a terminal's or a phone's across a
	// restart or a crash; nil keeps them while it runs.
	PlaceStore PlaceStore
}

// PlaceStore keeps a process's places, the keys it holds them under, past the
// process: a page's in the tab's own storage, a terminal's in a file of the user's
// state directory. Without one they last as long as the process does.
type PlaceStore interface {
	Load() ([]byte, error)
	Store([]byte) error
}

// RelayRouter is the router -relay names with the vif:// scheme, to publish this
// run's door through rather than an address to bind: a door behind a NAT, reached at
// the router's address (doc/multi-player.md §5.6).
func RelayRouter(relay string) (string, bool) {
	if !strings.HasPrefix(relay, "vif://") {
		return "", false
	}
	e, err := link.ParseEndpoint(relay)
	if err != nil || e.Addr == "" {
		return "", false
	}
	return e.Addr, true
}
