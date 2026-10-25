package network

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"time"

	link "github.com/lixenwraith/vif/pkg/network"
)

// ChainEntry is one participant, where it listens for peer links and joins, and
// its door: where it admits people who cannot reach the host, as the world reaches
// it. One with a door and no address is a leaf that relays, never a candidate.
type ChainEntry struct {
	ID   link.PeerID `json:"id"`
	Addr string      `json:"addr"`
	Door string      `json:"door,omitempty"`
}

// SuccessionChain is the ordered candidate list every participant holds.
//
// Append-only in join order, never re-sorted. That is what keeps succession safe
// without agreement: every held chain is a prefix of the newest, and a prefix and
// its extension list the same candidates in the same order.
type SuccessionChain []ChainEntry

// Lookup returns the address published for one participant.
func (c SuccessionChain) Lookup(id link.PeerID) (string, bool) {
	i := slices.IndexFunc(c, func(e ChainEntry) bool { return e.ID == id })
	if i < 0 || c[i].Addr == "" {
		return "", false
	}
	return c[i].Addr, true
}

// Append adds or updates one participant, keeping its position when it already
// has one so the order stays a prefix of what peers hold.
func (c SuccessionChain) Append(id link.PeerID, addr string) SuccessionChain {
	if id == 0 || addr == "" {
		return c
	}
	out := slices.Clone(c)
	if i := slices.IndexFunc(out, func(e ChainEntry) bool { return e.ID == id }); i >= 0 {
		out[i].Addr = addr
		return out
	}
	return append(out, ChainEntry{ID: id, Addr: addr})
}

// Door returns the door published for one participant.
func (c SuccessionChain) Door(id link.PeerID) (string, bool) {
	i := slices.IndexFunc(c, func(e ChainEntry) bool { return e.ID == id })
	if i < 0 || c[i].Door == "" {
		return "", false
	}
	return c[i].Door, true
}

// Doors lists every door the chain names, in chain order.
func (c SuccessionChain) Doors() []string {
	var out []string
	for _, e := range c {
		if e.Door != "" {
			out = append(out, e.Door)
		}
	}
	return out
}

// WithDoor records one participant's door, on its entry or on a new one with no
// address, which lists it without making it a candidate.
func (c SuccessionChain) WithDoor(id link.PeerID, door string) SuccessionChain {
	if id == 0 || door == "" {
		return c
	}
	out := slices.Clone(c)
	if i := slices.IndexFunc(out, func(e ChainEntry) bool { return e.ID == id }); i >= 0 {
		out[i].Door = door
		return out
	}
	return append(out, ChainEntry{ID: id, Door: door})
}

// Without drops a departed participant, whose identity returns to the pool.
func (c SuccessionChain) Without(id link.PeerID) SuccessionChain {
	return slices.DeleteFunc(slices.Clone(c), func(e ChainEntry) bool { return e.ID == id })
}

// PeerListRecord is the MsgPeerList body. It rides the authority's term and is
// refused below the term the receiver holds.
type PeerListRecord struct {
	Term      AuthorityTerm   `json:"term"`
	Authority link.PeerID     `json:"authority"`
	Chain     SuccessionChain `json:"chain"`
}

func EncodePeerList(r PeerListRecord) ([]byte, error) { return json.Marshal(r) }

func DecodePeerList(b []byte) (PeerListRecord, error) {
	var r PeerListRecord
	err := json.Unmarshal(b, &r)
	return r, err
}

// peerHello is the MsgConnect body. Two participants already hold identities the
// coordinator assigned, so a peer link admits rather than joins: nothing is
// allocated, offered or captured.
type peerHello struct {
	From     link.PeerID  `json:"from"`
	Identity PeerIdentity `json:"identity"`
}

type peerWelcome struct {
	From link.PeerID `json:"from"`
	refusalField
	Identity PeerIdentity `json:"identity"`
}

// PeerGate decides whether a peer link may open here. It reads the roster and
// allocates nothing. Local names this participant as each link opens: a joiner's
// listener is bound before its seat names it.
type PeerGate struct {
	Local    func() link.PeerID
	Identity PeerIdentity
	Admit    func(from link.PeerID) error
}

// PeerAcceptor returns the accept handshake for links from participants rather
// than joiners, which a guest's port runs beside HostAcceptor's. Only the build half
// of the identity is verified, as on the join path: the session half is settled by
// the roster the gate admits against.
func PeerAcceptor(g PeerGate, timeout time.Duration) func(net.Conn) (link.PeerID, error) {
	return func(conn net.Conn) (link.PeerID, error) {
		if timeout > 0 {
			_ = conn.SetDeadline(time.Now().Add(timeout))
			defer conn.SetDeadline(time.Time{})
		}
		msg, err := link.Decode(conn)
		if err != nil {
			return 0, err
		}
		if msg.Type != MsgConnect {
			return 0, fmt.Errorf("peer link: got message %#x, want connect", msg.Type)
		}
		var hello peerHello
		if err := json.Unmarshal(msg.Payload, &hello); err != nil {
			return 0, fmt.Errorf("peer link hello: %w", err)
		}
		reply := peerWelcome{From: g.Local(), Identity: g.Identity}
		var refused error
		if g.Identity.Protocol != 0 {
			if refused = g.Identity.VerifyBuild(hello.Identity); refused != nil {
				// Told in the dialler's terms: to it, its build is the one here.
				reply.Refuse(hello.Identity.VerifyBuild(g.Identity))
			}
		}
		if refused == nil && g.Admit != nil {
			if refused = g.Admit(hello.From); refused != nil {
				reply.Refuse(refused)
			}
		}
		body, err := json.Marshal(reply)
		if err != nil {
			return 0, err
		}
		if err := link.NewMessage(MsgAck, body).Encode(conn); err != nil {
			return 0, err
		}
		if refused != nil {
			return 0, refused
		}
		return hello.From, nil
	}
}

// PeerDialer returns the dial side of the peer-link handshake, which a port runs on
// each stream DialPeer opens (Config.ConnectPeer): it names this participant and its
// build, and reads which participant answered. Only the build half of the identity
// travels, as on the accept side.
func PeerDialer(identity PeerIdentity, timeout time.Duration) func(net.Conn, link.PeerID) (link.PeerID, error) {
	return func(conn net.Conn, local link.PeerID) (link.PeerID, error) {
		if timeout > 0 {
			_ = conn.SetDeadline(time.Now().Add(timeout))
			defer conn.SetDeadline(time.Time{})
		}
		body, err := json.Marshal(peerHello{From: local, Identity: identity})
		if err != nil {
			return 0, err
		}
		if err := link.NewMessage(MsgConnect, body).Encode(conn); err != nil {
			return 0, err
		}
		msg, err := link.Decode(conn)
		if err != nil {
			return 0, err
		}
		if msg.Type != MsgAck {
			return 0, fmt.Errorf("peer link: got message %#x, want ack", msg.Type)
		}
		var welcome peerWelcome
		if err := json.Unmarshal(msg.Payload, &welcome); err != nil {
			return 0, fmt.Errorf("peer link ack: %w", err)
		}
		if err := welcome.Refused(); err != nil {
			return 0, err
		}
		if welcome.From == 0 {
			return 0, errors.New("peer link: the far end named no participant")
		}
		return welcome.From, nil
	}
}
