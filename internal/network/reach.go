package network

import (
	"crypto/ed25519"
	"encoding/binary"
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

// PeerKeys is what a peer link's two ends sign and verify with: this participant's
// own key, and the roster's key for any participant. A gate bound later answers
// nothing until it is.
type PeerKeys interface {
	LocalKey() Key
	KeyOf(id link.PeerID) string
}

// peerHello is the MsgConnect body. Two participants already hold identities the
// coordinator assigned, so a peer link admits rather than joins: nothing is
// allocated, offered or captured. Nonce is what the acceptor proves itself over.
type peerHello struct {
	From     link.PeerID  `json:"from"`
	Identity PeerIdentity `json:"identity"`
	Nonce    string       `json:"nonce,omitempty"`
}

// peerWelcome is the MsgAck answer: who answered and its build, its proof over the
// dialer's nonce, and the nonce the dialer proves itself over in turn; or a refusal.
type peerWelcome struct {
	From link.PeerID `json:"from"`
	refusalField
	Identity PeerIdentity `json:"identity"`
	Nonce    string       `json:"nonce,omitempty"`
	Proof    []byte       `json:"proof,omitempty"`
}

// peerBind is the dialer's second MsgConnect: its proof over the acceptor's nonce.
// The acceptor answers it with one MsgAck carrying a refusal or none.
type peerBind struct {
	Proof []byte `json:"proof"`
}

// maxPeerFrame bounds a peer link's frames: an identity, a nonce and a signature.
const maxPeerFrame = 2048

// peerProof is a participant's signature binding one link: the nonce the other end
// chose, who signs and whom for, so a proof read off one link opens no other and
// names no other pair. The session is the roster the keys are read from: a dialer
// holds only the build half of its identity when its link is configured.
func peerProof(k Key, nonce string, from, to link.PeerID) []byte {
	return ed25519.Sign(k.private, peerSignable(nonce, from, to))
}

func verifyPeerProof(key string, proof []byte, nonce string, from, to link.PeerID) bool {
	pub := DecodeKey(key)
	return pub != nil && ed25519.Verify(ed25519.PublicKey(pub), peerSignable(nonce, from, to), proof)
}

func peerSignable(nonce string, from, to link.PeerID) []byte {
	return signable("peer", []byte(nonce), binary.AppendUvarint(nil, uint64(from)), binary.AppendUvarint(nil, uint64(to)))
}

// PeerGate decides whether a peer link may open here. It reads the roster and
// allocates nothing. Local names this participant as each link opens: a joiner's
// listener is bound before its seat names it. Keys signs the welcome and verifies
// the dialer's bind.
type PeerGate struct {
	Local    func() link.PeerID
	Identity PeerIdentity
	Admit    func(from link.PeerID) error
	Keys     PeerKeys
}

// ErrPeerUnproven refuses a peer link whose far end could not prove the key the
// roster holds for the participant it named; it crosses the wire as its kind.
var ErrPeerUnproven = &Refusal{Code: RefusalCredential, Reason: "the far end did not prove its participant key"}

// PeerAcceptor returns the accept handshake for links from participants rather
// than joiners, which a guest's port runs beside HostAcceptor's. Only the build half
// of the identity is verified, as on the join path: the session half is settled by
// the roster the gate admits against, and the signatures by the keys it carries.
func PeerAcceptor(g PeerGate, timeout time.Duration) func(net.Conn) (link.PeerID, error) {
	return func(conn net.Conn) (link.PeerID, error) {
		if timeout > 0 {
			_ = conn.SetDeadline(time.Now().Add(timeout))
			defer conn.SetDeadline(time.Time{})
		}
		msg, err := link.DecodeWithin(conn, maxPeerFrame)
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
		local := g.Local()
		reply := peerWelcome{From: local, Identity: g.Identity, Nonce: NewNonce()}
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
		// Nothing to sign with refuses every link, as a gate bound to no run does.
		var localKey Key
		keyOf := func(link.PeerID) string { return "" }
		if g.Keys != nil {
			localKey, keyOf = g.Keys.LocalKey(), g.Keys.KeyOf
		}
		if refused == nil && localKey.Valid() && hello.Nonce != "" {
			reply.Proof = peerProof(localKey, hello.Nonce, local, hello.From)
		}
		if err := writePeer(conn, MsgAck, reply); err != nil {
			return 0, err
		}
		if refused != nil {
			return 0, refused
		}
		msg, err = link.DecodeWithin(conn, maxPeerFrame)
		if err != nil {
			return 0, err
		}
		var bind peerBind
		if msg.Type != MsgConnect || json.Unmarshal(msg.Payload, &bind) != nil {
			return 0, fmt.Errorf("peer link: got message %#x, want the dialer's proof", msg.Type)
		}
		var verdict peerWelcome
		verdict.From = local
		if !verifyPeerProof(keyOf(hello.From), bind.Proof, reply.Nonce, hello.From, local) {
			verdict.Refuse(ErrPeerUnproven)
			_ = writePeer(conn, MsgAck, verdict)
			return 0, ErrPeerUnproven
		}
		if err := writePeer(conn, MsgAck, verdict); err != nil {
			return 0, err
		}
		return hello.From, nil
	}
}

// PeerDialer returns the dial side of the peer-link handshake, which a port runs on
// each stream DialPeer opens (Config.ConnectPeer): it names this participant and its
// build, reads which participant answered and that it proved the roster's key for
// itself, then proves its own. Only the build half of the identity travels, as on
// the accept side. A dialer with no keys opens nothing.
func PeerDialer(identity PeerIdentity, keys PeerKeys, timeout time.Duration) func(net.Conn, link.PeerID) (link.PeerID, error) {
	return func(conn net.Conn, local link.PeerID) (link.PeerID, error) {
		if keys == nil {
			return 0, errors.New("peer link: this participant holds no keys to dial with")
		}
		if timeout > 0 {
			_ = conn.SetDeadline(time.Now().Add(timeout))
			defer conn.SetDeadline(time.Time{})
		}
		hello := peerHello{From: local, Identity: identity, Nonce: NewNonce()}
		if err := writePeer(conn, MsgConnect, hello); err != nil {
			return 0, err
		}
		msg, err := link.DecodeWithin(conn, maxPeerFrame)
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
		if !verifyPeerProof(keys.KeyOf(welcome.From), welcome.Proof, hello.Nonce, welcome.From, local) {
			return 0, ErrPeerUnproven
		}
		k := keys.LocalKey()
		if !k.Valid() {
			return 0, errors.New("peer link: this participant holds no key yet")
		}
		if err := writePeer(conn, MsgConnect, peerBind{Proof: peerProof(k, welcome.Nonce, local, welcome.From)}); err != nil {
			return 0, err
		}
		if msg, err = link.DecodeWithin(conn, maxPeerFrame); err != nil {
			return 0, err
		}
		var verdict peerWelcome
		if msg.Type != MsgAck || json.Unmarshal(msg.Payload, &verdict) != nil {
			return 0, fmt.Errorf("peer link: got message %#x, want the acceptor's verdict", msg.Type)
		}
		if err := verdict.Refused(); err != nil {
			return 0, err
		}
		return welcome.From, nil
	}
}

func writePeer(conn net.Conn, kind link.MessageType, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return link.NewMessage(kind, raw).Encode(conn)
}
