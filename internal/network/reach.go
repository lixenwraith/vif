// Package network: where a participant is reached, so a successor can be dialled.
//
// The chain is the whole of it: every participant that bound a port, in join
// order, with the address it declared. It travels in the offer, the handoff
// record and MsgPeerList, and succession reads it.
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

// ChainEntry is one eligible participant and where it listens.
type ChainEntry struct {
	ID   PeerID `json:"id"`
	Addr string `json:"addr"`
}

// SuccessionChain is the ordered candidate list every participant holds.
//
// Append-only in join order, never re-sorted. That is what keeps succession safe
// without agreement: every held chain is a prefix of the newest, and a prefix and
// its extension name the same first survivor.
type SuccessionChain []ChainEntry

// Lookup returns the address published for one participant.
func (c SuccessionChain) Lookup(id PeerID) (string, bool) {
	i := slices.IndexFunc(c, func(e ChainEntry) bool { return e.ID == id })
	if i < 0 || c[i].Addr == "" {
		return "", false
	}
	return c[i].Addr, true
}

// Append adds or updates one participant, keeping its position when it already
// has one so the order stays a prefix of what peers hold.
func (c SuccessionChain) Append(id PeerID, addr string) SuccessionChain {
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

// Without drops a departed participant, whose identity returns to the pool.
func (c SuccessionChain) Without(id PeerID) SuccessionChain {
	return slices.DeleteFunc(slices.Clone(c), func(e ChainEntry) bool { return e.ID == id })
}

// PeerListRecord is the MsgPeerList body. It rides the authority's term and is
// refused below the term the receiver holds.
type PeerListRecord struct {
	Term      AuthorityTerm   `json:"term"`
	Authority PeerID          `json:"authority"`
	Chain     SuccessionChain `json:"chain"`
}

func EncodePeerList(r PeerListRecord) ([]byte, error) { return json.Marshal(r) }

func DecodePeerList(b []byte) (PeerListRecord, error) {
	var r PeerListRecord
	err := json.Unmarshal(b, &r)
	return r, err
}

// BindPeer binds the port this participant is dialled on, falling back to an
// OS-assigned one when the requested port is taken.
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

// peerHello is the MsgConnect body. Two participants already hold identities the
// coordinator assigned, so a peer link admits rather than joins: nothing is
// allocated, offered or captured.
type peerHello struct {
	From     PeerID       `json:"from"`
	Identity PeerIdentity `json:"identity"`
}

type peerWelcome struct {
	From     PeerID       `json:"from"`
	Error    string       `json:"error,omitempty"`
	Identity PeerIdentity `json:"identity"`
}

// PeerGate decides whether a peer link may open here. It reads the roster and
// allocates nothing.
type PeerGate struct {
	Local    PeerID
	Identity PeerIdentity
	Admit    func(from PeerID) error
}

// PeerAcceptor returns the accept handshake for links from participants rather
// than joiners; a guest binds with this and never with HostAcceptor. Only the
// build half of the identity is verified, as on the join path: the session half is
// settled by the roster the gate admits against.
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
			if err := g.Identity.VerifyBuild(hello.Identity); err != nil {
				reply.Error = err.Error()
			}
		}
		if reply.Error == "" && g.Admit != nil {
			if err := g.Admit(hello.From); err != nil {
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
		if reply.Error != "" {
			return 0, errors.New(reply.Error)
		}
		return hello.From, nil
	}
}

// DialPeerLink opens one stream to a chain address and completes the handshake.
func DialPeerLink(addr string, cfg *Config, local PeerID) (net.Conn, PeerID, error) {
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
	fail := func(err error) (net.Conn, PeerID, error) {
		_ = conn.Close()
		return nil, 0, err
	}
	body, err := json.Marshal(peerHello{From: local, Identity: base.Identity})
	if err != nil {
		return fail(err)
	}
	if err := NewMessage(MsgConnect, body).Encode(conn); err != nil {
		return fail(err)
	}
	msg, err := Decode(conn)
	if err != nil {
		return fail(err)
	}
	if msg.Type != MsgAck {
		return fail(fmt.Errorf("peer link: got message %#x, want ack", msg.Type))
	}
	var welcome peerWelcome
	if err := json.Unmarshal(msg.Payload, &welcome); err != nil {
		return fail(fmt.Errorf("peer link ack: %w", err))
	}
	if welcome.Error != "" {
		return fail(errors.New(welcome.Error))
	}
	if welcome.From == 0 {
		return fail(errors.New("peer link: the far end named no participant"))
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, welcome.From, nil
}
