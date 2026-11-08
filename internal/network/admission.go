package network

import (
	"cmp"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/lixenwraith/vif/internal/parameter"
)

// Admission is what a door asks of a dial after the route frame and the address
// budget, in place of the claims step: proof of a participant key, and with it a
// session pass, an admission ticket or a passphrase exchange. The frames, the tokens
// and the proofs are defined here; doc/auth-session-design.md §7 is the design.

// MaxAuthFrame bounds either admission frame's payload: the keys a run proves at
// their most (PlacesKept), a pass or a ticket of a few hundred bytes, a SCRAM step
// under a kilobyte.
const MaxAuthFrame = 8192

// AdmissionKind is what an admission is for. A pass and a ticket each carry one.
type AdmissionKind uint8

const (
	AdmitPlay    AdmissionKind = iota + 1 // a cursor in the session
	AdmitWatch                            // a cursorless entry that receives and never acts
	AdmitCreate                           // a fleet session made at the site
	AdmitPublish                          // a door published through the router
)

var admissionKinds = [...]string{AdmitPlay: "play", AdmitWatch: "watch", AdmitCreate: "create", AdmitPublish: "publish"}

// Valid reports a kind this build names.
func (k AdmissionKind) Valid() bool { return int(k) < len(admissionKinds) && admissionKinds[k] != "" }

func (k AdmissionKind) String() string {
	if k.Valid() {
		return admissionKinds[k]
	}
	return fmt.Sprintf("kind(%d)", uint8(k))
}

func (k AdmissionKind) MarshalText() ([]byte, error) {
	if !k.Valid() {
		return nil, fmt.Errorf("admission kind %d has no name", uint8(k))
	}
	return []byte(admissionKinds[k]), nil
}

func (k *AdmissionKind) UnmarshalText(text []byte) error {
	for i, name := range admissionKinds {
		if name != "" && name == string(text) {
			*k = AdmissionKind(i)
			return nil
		}
	}
	return fmt.Errorf("%q is no admission kind", text)
}

// The methods a door names in its question, and a dialer answers with. A door
// names every method it takes; a dialer picks the first it can.
const (
	MethodKey    = "key"    // a new key and its proof: an open door
	MethodPass   = "pass"   // a session pass over a key the door admitted before
	MethodTicket = "ticket" // an admission ticket the site's router signed
	MethodScram  = "scram"  // the passphrase exchange
)

// AuthResponse is the MsgAuthResponse body: the door's question, the nonce every
// proof of this dial signs and the methods it takes; then a SCRAM step; then the
// refusal, when the dial is turned away for what it showed.
type AuthResponse struct {
	Methods []string        `json:"methods,omitempty"`
	Nonce   string          `json:"nonce,omitempty"`
	Scram   json.RawMessage `json:"scram,omitempty"`
	refusalField
}

// AuthRequest is the MsgAuthRequest body: the dialer's answer. Keys are the
// participant keys it proves over the door's nonce, the first its own for a new
// place and the rest the places it may come back to; the door seats it in the place
// one of them holds. Pass or Ticket admits a key; neither does at an open door.
// SignIn is a sign-in token the router alone reads, and strips, on its TLS front door.
type AuthRequest struct {
	Keys   []Proof         `json:"keys,omitempty"`
	Pass   *Signed         `json:"pass,omitempty"`
	Ticket *Signed         `json:"ticket,omitempty"`
	SignIn string          `json:"sign_in,omitempty"`
	Scram  json.RawMessage `json:"scram,omitempty"`
}

// Proof is one key a dial proves: the public half and its signature over the door's
// nonce and the door's channel binding (SignProof).
type Proof struct {
	Key []byte `json:"key"`
	Sig []byte `json:"sig"`
}

// Prove is k's proof for one dial.
func (k Key) Prove(nonce string, binding []byte) Proof {
	return Proof{Key: k.Public(), Sig: k.SignProof(nonce, binding)}
}

// VerifyProofs checks every proof a dial named against the door's nonce and returns
// the keys they prove, in the dial's order and as the roster carries them: none, too
// many, a malformed one or one whose signature fails refuses the dial whole.
func VerifyProofs(proofs []Proof, nonce string, binding []byte) ([]string, error) {
	if len(proofs) == 0 || len(proofs) > parameter.PlacesKept {
		return nil, &Refusal{Code: RefusalCredential, Reason: fmt.Sprintf("a dial proves 1 to %d keys, not %d", parameter.PlacesKept, len(proofs))}
	}
	keys := make([]string, len(proofs))
	for i, p := range proofs {
		if !VerifyProof(p.Key, p.Sig, nonce, binding) {
			return nil, &Refusal{Code: RefusalCredential, Reason: "the dial could not prove a key it named"}
		}
		keys[i] = EncodeKey(p.Key)
	}
	return keys, nil
}

// EncodeKey is a public key as the roster and a cursor carry it, 44 characters of
// base64, the form the JSON of a []byte takes; DecodeKey reads one back, nil for a
// string of another form. A roster compares keys as strings.
func EncodeKey(pub []byte) string {
	if !ValidPublicKey(pub) {
		return ""
	}
	return base64.StdEncoding.EncodeToString(pub)
}

func DecodeKey(s string) []byte {
	pub, err := base64.StdEncoding.Strict().DecodeString(s)
	if err != nil || !ValidPublicKey(pub) {
		return nil
	}
	return pub
}

// ValidKeyString reports a string of a public key's form.
func ValidKeyString(s string) bool { return DecodeKey(s) != nil }

// NewNonce is the challenge a door writes into its question: random bytes no two
// dials share, so a proof read off one link is good on no other.
func NewNonce() string {
	b := make([]byte, parameter.AuthNonceBytes)
	_, _ = rand.Read(b) // crypto/rand.Read does not fail
	return base64.RawURLEncoding.EncodeToString(b)
}

// Key is an Ed25519 key pair: a participant's for its place, whose public half the
// roster carries; an authority's, which signs its session's passes; a router's,
// which signs its tickets. A keeper stores the private half beside the pass it holds.
type Key struct {
	private ed25519.PrivateKey
}

// NewKey makes a key pair.
func NewKey() Key {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err) // crypto/rand.Reader does not fail
	}
	return Key{private: private}
}

// Public is the half a roster entry, a pass and a verifier hold.
func (k Key) Public() []byte {
	if !k.Valid() {
		return nil
	}
	return []byte(k.private.Public().(ed25519.PublicKey))
}

// Valid reports a key that can sign.
func (k Key) Valid() bool { return len(k.private) == ed25519.PrivateKeySize }

// ID is the public half as the roster carries it (EncodeKey), "" for an empty key.
func (k Key) ID() string { return EncodeKey(k.Public()) }

// MarshalJSON keeps the 32-byte seed, what a keeper stores; the public half is
// derived from it when read back.
func (k Key) MarshalJSON() ([]byte, error) {
	if !k.Valid() {
		return nil, errors.New("an empty key marshals nothing")
	}
	return json.Marshal(k.private.Seed())
}

func (k *Key) UnmarshalJSON(data []byte) error {
	var seed []byte
	if err := json.Unmarshal(data, &seed); err != nil {
		return err
	}
	if len(seed) != ed25519.SeedSize {
		return fmt.Errorf("a key is %d bytes, not %d", ed25519.SeedSize, len(seed))
	}
	k.private = ed25519.NewKeyFromSeed(seed)
	return nil
}

// ValidPublicKey reports a byte string of a public key's length.
func ValidPublicKey(key []byte) bool { return len(key) == ed25519.PublicKeySize }

// SignProof is a dialer's proof for one dial: the key's signature over the door's
// nonce and, on a wss door, the SHA-256 of the leaf certificate (binding), nil
// elsewhere. A proof is good at one door for one nonce; the session's name is not
// signed, since a front door rewrites it on the way to the door that chose the nonce.
func (k Key) SignProof(nonce string, binding []byte) []byte {
	return ed25519.Sign(k.private, signable("proof", []byte(nonce), binding))
}

// VerifyProof checks proof as key's signature over the same message a dialer signs.
func VerifyProof(key, proof []byte, nonce string, binding []byte) bool {
	return ValidPublicKey(key) && ed25519.Verify(ed25519.PublicKey(key), signable("proof", []byte(nonce), binding), proof)
}

// ChannelBinding is the binding a wss door and its dialer agree on: the SHA-256 of
// the leaf certificate, the digest Fingerprint prints; nil for no certificate.
func ChannelBinding(leaf []byte) []byte {
	if len(leaf) == 0 {
		return nil
	}
	sum := sha256.Sum256(leaf)
	return sum[:]
}

// signable is the bytes a signature covers: a domain separator, so a proof, a pass
// and a ticket signed with one key never stand in for each other, then each field
// behind its length, so no field runs into the next.
func signable(domain string, fields ...[]byte) []byte {
	out := append([]byte("vif "), domain...)
	out = append(out, 0)
	for _, f := range fields {
		out = binary.AppendUvarint(out, uint64(len(f)))
		out = append(out, f...)
	}
	return out
}

// Signed is a statement under an Ed25519 signature: the body as its signer
// marshalled it, kept byte for byte so the verifier checks what was signed, and the
// signature over it under the token's domain. A pass and a ticket are each one.
type Signed struct {
	Body json.RawMessage `json:"body"`
	Sig  []byte          `json:"sig"`
}

// maxSignedBody bounds a token's body: a short JSON object of two keys and a name.
const maxSignedBody = 1024

func sign(domain string, key Key, body any) (*Signed, error) {
	if !key.Valid() {
		return nil, errors.New("no key to sign with")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	if len(raw) > maxSignedBody {
		return nil, fmt.Errorf("a %s body of %d bytes is past %d", domain, len(raw), maxSignedBody)
	}
	return &Signed{Body: raw, Sig: ed25519.Sign(key.private, signable(domain, raw))}, nil
}

// open verifies s under key for the domain and decodes its body into v. A token
// that is not key's, not of this domain or not a body this build reads is refused.
func (s *Signed) open(domain string, key []byte, v any) error {
	if s == nil || len(s.Body) == 0 || len(s.Body) > maxSignedBody || len(s.Sig) != ed25519.SignatureSize {
		return fmt.Errorf("a %s of another form", domain)
	}
	if !ValidPublicKey(key) || !ed25519.Verify(ed25519.PublicKey(key), signable(domain, s.Body), s.Sig) {
		return fmt.Errorf("the %s is not signed by the key it is checked against", domain)
	}
	return json.Unmarshal(s.Body, v)
}

// PassBody is a session pass: the authority's statement that Key was admitted to
// the session as Kind, under the account the ticket named if any, for every redial
// until Expires. Authority is the first term's authority key, the session's identity
// across every name and successor; Term is the term that signed it.
type PassBody struct {
	Session   string        `json:"session"`
	Authority []byte        `json:"authority"`
	Key       []byte        `json:"key"`
	Kind      AdmissionKind `json:"kind"`
	Account   string        `json:"account,omitempty"`
	Name      string        `json:"name,omitempty"`
	Term      AuthorityTerm `json:"term"`
	Issued    int64         `json:"iat"` // [wall] unix seconds, the clock every token lives on
	Expires   int64         `json:"exp"`
}

// SignPass issues a pass under the term's authority key, good from now for life,
// capped at PassLifetime; a life of zero is the cap.
func SignPass(authority Key, body PassBody, now time.Time, life time.Duration) (*Signed, error) {
	switch {
	case len(body.Session) > MaxSessionName:
		return nil, errors.New("the pass's session name is too long")
	case !ValidPublicKey(body.Authority) || !ValidPublicKey(body.Key) || !body.Kind.Valid():
		return nil, errors.New("a pass names an authority key, a participant key and a kind")
	case body.Term < FirstTerm:
		return nil, errors.New("a pass carries its term")
	}
	if life <= 0 || life > parameter.PassLifetime {
		life = parameter.PassLifetime
	}
	body.Issued = now.Unix()
	body.Expires = now.Add(life).Unix()
	return sign("pass", authority, body)
}

// VerifyPass checks a pass against the key of the term it names and the clock, and
// returns its body; the door then checks the session, the keep-out list and the
// dialer's proof over Body.Key. termKey is what the door holds for a term, nil for
// one it does not: a successor reads them from the handoff records.
func VerifyPass(s *Signed, termKey func(AuthorityTerm) []byte, now time.Time) (PassBody, error) {
	var peek struct {
		Term AuthorityTerm `json:"term"`
	}
	if s == nil || len(s.Body) > maxSignedBody || json.Unmarshal(s.Body, &peek) != nil || peek.Term < FirstTerm {
		return PassBody{}, errors.New("a pass of another form")
	}
	var body PassBody
	if err := s.open("pass", termKey(peek.Term), &body); err != nil {
		return PassBody{}, err
	}
	if err := checkToken(body.Issued, body.Expires, now); err != nil {
		return PassBody{}, fmt.Errorf("the pass %w", err)
	}
	if !ValidPublicKey(body.Key) || !ValidPublicKey(body.Authority) || !body.Kind.Valid() {
		return PassBody{}, errors.New("the pass names no participant")
	}
	return body, nil
}

// Renewable reports a pass past half its life, which a redial takes a fresh one for.
func (b PassBody) Renewable(now time.Time) bool {
	return now.Unix()-b.Issued >= (b.Expires-b.Issued)/2
}

// TicketBody is an admission ticket: the router's statement that Subject may enter
// Session as Kind, once, within its minute. Key is the participant key the dialer
// proved to the router, so the ticket admits that key alone; ID is spent once.
type TicketBody struct {
	Session string        `json:"session"`
	Kind    AdmissionKind `json:"kind"`
	Subject string        `json:"sub"`
	Name    string        `json:"name,omitempty"`
	Key     []byte        `json:"key,omitempty"`
	ID      string        `json:"jti"`
	Issued  int64         `json:"iat"`
	Expires int64         `json:"exp"`
}

// maxSubject bounds a ticket's subject: an address or an account id.
const maxSubject = 128

// SubjectOf is an anonymous ticket's subject: the dialer's address as the budgets
// key it, so the pod names the player as the router does. An account's is its id.
func SubjectOf(addr net.Addr) string { return "addr:" + admissionKey(addr) }

// SignTicket issues a ticket under the router's key, good from now for
// TicketLifetime, with a fresh id.
func SignTicket(router Key, body TicketBody, now time.Time) (*Signed, error) {
	switch {
	case len(body.Session) > MaxSessionName:
		return nil, errors.New("the ticket's session name is too long")
	case body.Subject == "" || len(body.Subject) > maxSubject || !body.Kind.Valid():
		return nil, errors.New("a ticket names its subject and its kind")
	case body.Key != nil && !ValidPublicKey(body.Key):
		return nil, errors.New("a ticket's key is a public key")
	}
	body.ID = NewNonce()
	body.Issued = now.Unix()
	body.Expires = now.Add(parameter.TicketLifetime).Unix()
	return sign("ticket", router, body)
}

// VerifyTicket checks a ticket against the router's public key and the clock, and
// returns its body; the door then checks the session, spends the id and checks the
// dialer's proof over Body.Key.
func VerifyTicket(s *Signed, routerKey []byte, now time.Time) (TicketBody, error) {
	var body TicketBody
	if err := s.open("ticket", routerKey, &body); err != nil {
		return TicketBody{}, err
	}
	if err := checkToken(body.Issued, body.Expires, now); err != nil {
		return TicketBody{}, fmt.Errorf("the ticket %w", err)
	}
	if body.ID == "" || body.Subject == "" || !body.Kind.Valid() || (body.Key != nil && !ValidPublicKey(body.Key)) {
		return TicketBody{}, errors.New("the ticket names nobody")
	}
	return body, nil
}

// checkToken reads a token's window against the clock, AuthClockLeeway either way
// for a dialer's or an issuer's skew: one from the future is as refused as a stale one.
func checkToken(issued, expires int64, now time.Time) error {
	leeway := int64(parameter.AuthClockLeeway / time.Second)
	at := now.Unix()
	switch {
	case expires <= issued:
		return errors.New("has no life")
	case at > expires+leeway:
		return errors.New("has expired")
	case at+leeway < issued:
		return errors.New("is from the future")
	}
	return nil
}

// SpentTickets is what a door keeps of the tickets it admitted, by id, for a
// ticket's life, so one admits once. Bounded: at Max a fresh id is refused too,
// since a door that full is being flooded with tickets a router did sign.
type SpentTickets struct {
	Max int // zero is parameter.SpentTicketsKept

	mu    sync.Mutex
	spent map[string]int64
}

// Spend records a ticket's id and reports whether it was fresh, sweeping ids past
// their expiry as it goes.
func (s *SpentTickets) Spend(body TicketBody, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	at := now.Unix()
	if s.spent == nil {
		s.spent = make(map[string]int64)
	}
	for id, exp := range s.spent {
		if exp < at {
			delete(s.spent, id)
		}
	}
	if _, seen := s.spent[body.ID]; seen || len(s.spent) >= cmp.Or(s.Max, parameter.SpentTicketsKept) {
		return false
	}
	s.spent[body.ID] = body.Expires + int64(parameter.AuthClockLeeway/time.Second)
	return true
}
