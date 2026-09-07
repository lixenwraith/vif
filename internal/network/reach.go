// Package network: how a participant is reached, as opposed to who it is.
//
// Until this file the protocol carried no address anywhere. An offer carried
// identity, roster, term and bounds; a handoff carried membership. A guest was
// told where the coordinator was by the operator typing `-join`, and nobody was
// ever told where a guest was. So `-authority migrate` moved authorship correctly
// and moved nothing else: the successor authored, alone, because no survivor had a
// link to it and none could be made.
//
// Two tables close that, and they are separate on purpose.
//
// **Addresses** are where a participant listens, and they change: a peer rebinds,
// an ephemeral port is different next time, a participant that failed to bind at
// all has none. They are carried beside the roster — never inside
// SessionParticipant, which SameRoster compares by value and HandoffRecord.Validate
// requires byte-identical, so an address field there would make a rebind look like
// a different roster and fail every handoff.
//
// **Reachability** is whether the session has ever confirmed a participant at an
// address of its own, and it is frozen for the term. That distinction is what makes
// it safe as a succession input. DesignatedSuccessor has to be a pure function of
// state every survivor holds *identically*, or two survivors compute two successors
// and the split-brain rule is gone — and a table that changes while the term runs
// is exactly not that. The frozen set travels in the artifacts that define the
// term, the offer and the handoff record, so every participant of one term holds
// one set; the addresses travel beside it and may be updated by MsgPeerList as
// often as they like, because nothing decides anything from them.
package network

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"time"
)

// PeerAddress is one participant's confirmed listening address.
type PeerAddress struct {
	ID   PeerID `json:"id"`
	Addr string `json:"addr"`
}

// PeerAddresses is the replicated reachability map, keyed by identity.
type PeerAddresses []PeerAddress

// Lookup returns the address published for one participant.
func (m PeerAddresses) Lookup(id PeerID) (string, bool) {
	i := slices.IndexFunc(m, func(a PeerAddress) bool { return a.ID == id })
	if i < 0 || m[i].Addr == "" {
		return "", false
	}
	return m[i].Addr, true
}

// Normalized drops empty entries and duplicates and sorts by identity, so two
// instances that learned the same addresses in different orders hold one value.
func (m PeerAddresses) Normalized() PeerAddresses {
	if len(m) == 0 {
		return nil
	}
	out := make(PeerAddresses, 0, len(m))
	for _, a := range m {
		if a.ID == 0 || a.Addr == "" {
			continue
		}
		if slices.ContainsFunc(out, func(b PeerAddress) bool { return b.ID == a.ID }) {
			continue
		}
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b PeerAddress) int { return int(a.ID) - int(b.ID) })
	return out
}

// With returns the map updated with one participant's address, normalized.
func (m PeerAddresses) With(id PeerID, addr string) PeerAddresses {
	out := slices.Clone(m)
	if i := slices.IndexFunc(out, func(a PeerAddress) bool { return a.ID == id }); i >= 0 {
		out[i].Addr = addr
	} else {
		out = append(out, PeerAddress{ID: id, Addr: addr})
	}
	return out.Normalized()
}

// Without returns the map with one participant removed, which is what a departure
// leaves behind: an identity returns to the pool and its address describes nobody.
func (m PeerAddresses) Without(id PeerID) PeerAddresses {
	out := slices.DeleteFunc(slices.Clone(m), func(a PeerAddress) bool { return a.ID == id })
	return out.Normalized()
}

// NormalizeReachable sorts and dedupes a confirmed set, so two instances that
// built one from the same confirmations hold the same slice.
func NormalizeReachable(ids []PeerID) []PeerID {
	if len(ids) == 0 {
		return nil
	}
	out := slices.Clone(ids)
	slices.Sort(out)
	out = slices.Compact(out)
	return slices.DeleteFunc(out, func(id PeerID) bool { return id == 0 })
}

// PeerListRecord is the MsgPeerList body: the authority's current address map.
//
// It rides the term like every other authoritative artifact and is refused below
// the term the receiver holds, which is what stops a stale broadcast resurrecting
// a departed peer. It carries addresses only — the confirmed set it was derived
// from belongs to the term and travels in the offer and the handoff.
type PeerListRecord struct {
	Term      AuthorityTerm `json:"term"`
	Authority PeerID        `json:"authority"`
	Addresses PeerAddresses `json:"addresses"`
}

// EncodePeerList renders one address-map broadcast.
func EncodePeerList(r PeerListRecord) ([]byte, error) { return json.Marshal(r) }

// DecodePeerList parses one address-map broadcast.
func DecodePeerList(b []byte) (PeerListRecord, error) {
	var r PeerListRecord
	err := json.Unmarshal(b, &r)
	r.Addresses = r.Addresses.Normalized()
	return r, err
}

// BindPeer binds the port this participant will be dialled on, falling back to an
// OS-assigned one when the requested port is already in use.
//
// The default the caller passes is the coordinator's own port, by decision: one
// firewall rule for a session, and a port that is harmless on the machine that
// chose it rather than an arbitrary one this participant's operator never picked.
// A machine already using it — two guests on one host, a guest on the host's own
// machine — takes an ephemeral port instead and declares that.
func BindPeer(addr string, cfg *Config) (net.Listener, error) {
	listen := func(a string) (net.Listener, error) {
		if cfg != nil && cfg.TLS != nil {
			return tls.Listen("tcp", a, cfg.TLS)
		}
		return net.Listen("tcp", a)
	}
	ln, err := listen(addr)
	if err == nil {
		return ln, nil
	}
	host, _, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return nil, err
	}
	return listen(net.JoinHostPort(host, "0"))
}

// === peer links ===

// peerHello is the MsgConnect body, and it is deliberately not a join offer.
//
// Two participants that dial each other are already in one session: they hold
// identities the coordinator assigned, a roster, a term and a world. What they need
// is a stream, not an admission — so this says who is calling and under which term,
// the far end checks that against the roster it already holds, and nothing is
// allocated, offered or captured.
type peerHello struct {
	From     PeerID        `json:"from"`
	Term     AuthorityTerm `json:"term"`
	Identity PeerIdentity  `json:"identity"`

	// Probe marks the authority's bind confirmation: a round trip that proves the
	// declared address answers, and then closes. It must not become a link — the
	// two are already connected — so the acceptor answers it and refuses it.
	Probe bool `json:"probe,omitempty"`
}

// peerWelcome is the MsgAck body: the answer, and who gave it.
type peerWelcome struct {
	From     PeerID       `json:"from"`
	Error    string       `json:"error,omitempty"`
	Identity PeerIdentity `json:"identity"`
}

// errPeerProbe ends a confirmation dial's stream without admitting it. The dialer
// has what it came for by the time this is returned.
var errPeerProbe = errors.New("peer link: bind confirmation, not a link")

// PeerGate is what an instance needs to decide whether a peer link may open here.
// It reads the roster and the term; it allocates nothing.
type PeerGate struct {
	Local    PeerID
	Identity PeerIdentity

	// Admit reports whether this caller may open a link. It is the roster check —
	// the caller is a participant of this session, it is not this instance, and it
	// is not already connected — plus the term gate every authoritative artifact
	// passes.
	Admit func(from PeerID, term AuthorityTerm) error
}

// PeerAcceptor returns the accept handshake for links from participants rather
// than from joiners. A guest binds with this and never with HostAcceptor: it has
// no roster to assign from, no world to capture, and no identity to hand out.
func PeerAcceptor(g PeerGate, timeout time.Duration) func(net.Conn) (PeerID, error) {
	return func(conn net.Conn) (PeerID, error) {
		if timeout > 0 {
			_ = conn.SetDeadline(time.Now().Add(timeout))
			defer conn.SetDeadline(time.Time{})
		}
		msg, err := Decode(conn)
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
		reply := peerWelcome{From: g.Local, Identity: g.Identity}
		if g.Identity.Protocol != 0 {
			if err := g.Identity.Verify(hello.Identity); err != nil {
				reply.Error = err.Error()
			}
		}
		if reply.Error == "" && !hello.Probe && g.Admit != nil {
			if err := g.Admit(hello.From, hello.Term); err != nil {
				reply.Error = err.Error()
			}
		}
		body, err := json.Marshal(reply)
		if err != nil {
			return 0, err
		}
		if err := NewMessage(MsgAck, body).Encode(conn); err != nil {
			return 0, err
		}
		switch {
		case reply.Error != "":
			return 0, errors.New(reply.Error)
		case hello.Probe:
			return 0, errPeerProbe
		}
		return hello.From, nil
	}
}

// DialPeerLink opens one stream to an address from the address map and completes
// the peer handshake, returning the connection and the participant that answered.
//
// probe asks only for the round trip: the far end answers and closes, which is the
// bind confirmation the authority makes before it publishes an address. A probe
// returns no usable connection.
func DialPeerLink(addr string, cfg *Config, local PeerID, term AuthorityTerm, probe bool) (net.Conn, PeerID, error) {
	base := DefaultConfig()
	if cfg != nil {
		c := *cfg
		base = &c
	}
	conn, err := dial(addr, base)
	if err != nil {
		return nil, 0, err
	}
	if base.ConnectTimeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(base.ConnectTimeout))
	}
	closeOnErr := func(err error) (net.Conn, PeerID, error) {
		_ = conn.Close()
		return nil, 0, err
	}
	body, err := json.Marshal(peerHello{
		From: local, Term: term, Identity: base.Identity, Probe: probe,
	})
	if err != nil {
		return closeOnErr(err)
	}
	if err := NewMessage(MsgConnect, body).Encode(conn); err != nil {
		return closeOnErr(err)
	}
	msg, err := Decode(conn)
	if err != nil {
		return closeOnErr(err)
	}
	if msg.Type != MsgAck {
		return closeOnErr(fmt.Errorf("peer link: got message %#x, want ack", msg.Type))
	}
	var welcome peerWelcome
	if err := json.Unmarshal(msg.Payload, &welcome); err != nil {
		return closeOnErr(fmt.Errorf("peer link ack: %w", err))
	}
	if welcome.Error != "" {
		return closeOnErr(errors.New(welcome.Error))
	}
	if welcome.From == 0 {
		return closeOnErr(errors.New("peer link: the far end named no participant"))
	}
	_ = conn.SetDeadline(time.Time{})
	if probe {
		_ = conn.Close()
		return nil, welcome.From, nil
	}
	return conn, welcome.From, nil
}
