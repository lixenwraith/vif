package network

import (
	"cmp"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// RosterEntry is one identity the coordinator has admitted, and the cursor slot it
// drives. NoPlayerSlot means it drives none, which is what a dedicated coordinator
// holds: in the roster, and not one of the session's participants. Away is one that
// left and has a Wraith waiting in its slot: both stay taken, and nothing waits on it.
// Claim is the digest of the token its participant comes back with (ClaimDigest).
type RosterEntry struct {
	ID     PeerID `json:"id"`
	Slot   uint8  `json:"slot"`
	Holder PeerID `json:"holder,omitempty"`
	Away   bool   `json:"away,omitempty"`
	Claim  string `json:"claim,omitempty"`
}

// SessionOffer carries the replay identity and the coordinator-owned roster.
type SessionOffer struct {
	Anchor   event.JoinAnchor `json:"anchor"`
	Host     PeerID           `json:"host"`
	Assigned PeerID           `json:"assigned"`

	// Term is the authority generation this offer was written under, and Host the
	// participant authoring it. A joiner adopts both: an admission is an
	// authoritative artifact like any other, and one written under a term that is
	// about to end is exactly what a mid-handoff dial must not be half-admitted
	// into.
	Term AuthorityTerm `json:"term,omitempty"`

	// Roster is everyone the coordinator admitted, the cursorless one included.
	// ParticipantCount is what a count of participants means; len(Roster) is not.
	Roster []RosterEntry `json:"participants"`

	// Chain is the succession candidate list with the address each was confirmed
	// at. Beside the roster rather than inside RosterEntry, which SameRoster
	// compares by value: an address change must not look like a different roster.
	Chain SuccessionChain `json:"chain,omitempty"`

	// FixedAuthority pins authorship to the opening participant. The session-wide
	// policy keeps guests from interpreting one host loss in different ways.
	FixedAuthority bool `json:"fixed_authority,omitempty"`

	// Players is the ceiling on the session's cursors the host that opened it set
	// with -players, zero for none: a host that takes the session over admits no
	// more than it would have.
	Players int `json:"players,omitempty"`

	// Relay is the participant on the far end of the joiner's stream when that is not
	// the host: the holder a bot joined through. Zero on a dial to the coordinator.
	Relay PeerID `json:"relay,omitempty"`

	// SnapshotTick names the tick of the capture that follows the start gate, and
	// SnapshotBytes its encoded length. A joiner reads them before the transfer so a
	// stream that stops halfway is a failed join rather than a world installed from
	// a prefix. Zero means the gate carries no capture, which is what an offer
	// written by a build that predates this looks like.
	SnapshotTick  uint64 `json:"snapshot_tick,omitempty"`
	SnapshotBytes int    `json:"snapshot_bytes,omitempty"`

	// Held says the session stands held at SnapshotTick, so the joiner holds there
	// too. The crossings still scheduled past that tick follow the capture. Start
	// says the hold is a new game waiting for its host's press, which the joiner
	// waits for as that rather than as a pause.
	Held  bool `json:"held,omitempty"`
	Start bool `json:"start,omitempty"`

	// Identity is what the coordinator is. The joiner reads it before it builds a
	// world, so a peer that cannot be in this session finds out before paying for
	// the attempt — and the anchor comparison it already performs is left in place,
	// because that one covers the world it is about to construct.
	Identity PeerIdentity `json:"identity"`
}

// CarriesSnapshot reports whether the start gate is followed by a capture.
func (o SessionOffer) CarriesSnapshot() bool { return o.SnapshotBytes > 0 }

type sessionReply struct {
	refusalField

	// The joiner's own terminal-equivalent geometry, sent with its acceptance. It
	// is advisory and arrives after the joiner has built its world, so it can only
	// inform what the coordinator does next — not what it has already done.
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`

	// Identity is what the joiner turned out to be, sent with the same acceptance
	// and not advisory at all: the coordinator refuses the join on it. A reply that
	// carries none is a peer that predates this or one that chose not to say, and
	// both are refused for the same reason — the authority cannot verify what it
	// was not told.
	Identity PeerIdentity `json:"identity"`

	// Listen is the address this participant bound, empty for a leaf. Declared, not
	// confirmed: the coordinator dials it once before publishing it.
	Listen string `json:"listen,omitempty"`
	Holder PeerID `json:"holder,omitempty"`

	Claim  string              `json:"claim,omitempty"`
	Record *event.CursorRecord `json:"record,omitempty"`
}

// JoinerReport declares simulation identity, bot holder, and advisory geometry.
// The coordinator validates it before accepting the participant.
type JoinerReport struct {
	Holder PeerID
	Width  int
	Height int

	// Identity is what this participant turned out to be, and unlike the geometry
	// it is not advisory: the coordinator refuses the join on it. It is reported
	// with the acceptance rather than with the dial because half of it — the seed,
	// the configuration, the corpus that actually loaded — does not exist until the
	// joiner has built its world.
	Identity PeerIdentity

	// Listen is the address this participant bound, declared and not yet confirmed.
	// Remote is where the join stream came from, filled by the coordinator: a
	// participant knows its port and not the address the world reaches it at.
	Listen string
	Remote string

	// Claim is the digest of a new token this participant will come back with; one
	// coming back keeps the claim it returned under. Record is what one coming back
	// brings of the game it left, and nothing for a new place.
	Claim  string
	Record *event.CursorRecord
}

// Sized reports whether this report names a usable geometry.
func (r JoinerReport) Sized() bool { return r.Width > 0 && r.Height > 0 }

// RelayStep is one decision a relay asks of the coordinator for a participant
// joining through it, in the order a join takes them.
type RelayStep uint8

const (
	RelayAssign  RelayStep = iota + 1 // allocate an identity and a slot the relay holds
	RelayReady                        // the participant installed the relay's world: cross its arrival
	RelayAbandon                      // the join failed: retire the participant, unanswered
)

// RelayJoin is a relay's request. Nonce pairs it with its answer; Leaf names the
// participant once Assign has. The relay checked the participant's identity against
// the offer, as a coordinator checks a dial's reply, and RelayReady carries what it
// reported: its holder, if it is a bot, and the address it declared, if any. Assign
// carries the tokens the participant named, as a dial names them to a coordinator.
type RelayJoin struct {
	Step   RelayStep    `json:"step"`
	Nonce  uint32       `json:"nonce"`
	Leaf   PeerID       `json:"leaf,omitempty"`
	Claims []string     `json:"claims,omitempty"`
	Report JoinerReport `json:"report,omitzero"`
}

// RelayAdmit answers one RelayJoin: the offer Assign allocated, or why not.
type RelayAdmit struct {
	Nonce uint32       `json:"nonce"`
	Offer SessionOffer `json:"offer,omitzero"`
	refusalField
}

// Validate rejects transport-level ambiguity before App.Join checks identity.
func (o SessionOffer) Validate() error {
	if o.Host == 0 || o.Assigned == 0 || o.Host == o.Assigned {
		return errors.New("join offer carries invalid participant assignment")
	}
	if o.Term < FirstTerm {
		return errors.New("join offer carries no authority term")
	}
	ids := make(map[PeerID]bool, len(o.Roster))
	slots := make(map[uint8]bool, len(o.Roster))
	cursorless := PeerID(0)
	for _, p := range o.Roster {
		if p.ID == 0 || ids[p.ID] {
			return errors.New("join offer carries duplicate participant assignment")
		}
		ids[p.ID] = true
		// A cursorless entry holds no slot, so it collides with nobody. Only the
		// coordinator may be one: every other peer is in the session to drive a
		// cursor, and one that is not would take an identity and a roster
		// entry while contributing nothing the roster describes.
		if p.Slot == parameter.NoPlayerSlot {
			if cursorless != 0 {
				return errors.New("join offer carries more than one cursorless peer")
			}
			cursorless = p.ID
			continue
		}
		if slots[p.Slot] {
			return errors.New("join offer carries duplicate participant assignment")
		}
		slots[p.Slot] = true
	}
	for _, p := range o.Roster {
		if p.Holder == 0 {
			continue
		}
		holder, ok := o.Entry(p.Holder)
		if !ok || holder.Holder != 0 || p.ID == p.Holder || p.ID == o.Host {
			return errors.New("join offer carries invalid bot ownership")
		}
	}
	if cursorless != 0 && cursorless != o.Host {
		return errors.New("join offer makes a peer other than the host cursorless")
	}
	if !ids[o.Host] || !ids[o.Assigned] {
		return errors.New("join offer roster omits host or assigned participant")
	}
	if o.Relay != 0 && (!ids[o.Relay] || o.Relay == o.Host || o.Relay == o.Assigned) {
		return errors.New("join offer names a relay outside the roster, or the host, or the joiner")
	}
	return nil
}

// ValidatePreview rejects a preview no dialer can prepare on: one naming no host or
// term, one that already assigns a place, or one relayed by the host itself.
func (o SessionOffer) ValidatePreview() error {
	switch {
	case o.Host == 0 || o.Assigned != 0:
		return errors.New("join preview names no host, or assigns a place before the seat")
	case o.Term < FirstTerm:
		return errors.New("join preview carries no authority term")
	case o.Relay == o.Host:
		return errors.New("join preview names the host as its own relay")
	}
	return nil
}

// Entry returns the coordinator assignment for id.
func (o SessionOffer) Entry(id PeerID) (RosterEntry, bool) {
	i := slices.IndexFunc(o.Roster, func(p RosterEntry) bool { return p.ID == id })
	if i < 0 {
		return RosterEntry{}, false
	}
	return o.Roster[i], true
}

// ParticipantCount is how many cursors this session holds. A coordinator with no
// slot is in the roster and not in this number, so a dedicated host serving one
// guest is a session of one participant.
func (o SessionOffer) ParticipantCount() int {
	n := 0
	for _, e := range o.Roster {
		if e.Slot != parameter.NoPlayerSlot {
			n++
		}
	}
	return n
}

// Coordinator is the host side of the startup handshake. Assign allocates the next
// participant identity and returns the offer carrying it, or the place one of the
// dialer's claim tokens comes back to; Release returns that identity to the pool, or
// that place to its Wraith, when the handshake does not complete, so a rejected or
// abandoned connection does not consume a roster slot. Assign runs at the dialer's
// seat, never before: a dialer prepares on a preview, which holds nothing.
type Coordinator struct {
	Assign  func(claims []string) (SessionOffer, error)
	Release func(PeerID)

	// Preview is the session as a dialer naming claims would be offered it, with no
	// identity assigned: the offer Assign would make, or the refusal it would give,
	// with nothing allocated or asked for. Nil previews nothing, which only a dialer
	// that seats at once (DialSession) can join on.
	Preview func(claims []string) (SessionOffer, error)

	// PressWindow is the ready period a prepared dialer has to seat in; zero is
	// parameter.NetworkJoinPressWindow.
	PressWindow time.Duration

	// Prepared says a dialer from addr is ready and its player has the ready period
	// to press in, so the session can show whom it is about to seat. Nil shows nobody.
	Prepared func(addr net.Addr)

	// Report carries what a joiner said about itself once its acceptance arrives.
	// It runs after Assign and only for a handshake that completed, so what it
	// describes is a participant the session actually holds.
	Report func(PeerID, JoinerReport) error

	// Admit spends the dial budget before expensive world capture and construction.
	// A nil hook admits the dial.
	Admit func(net.Addr) error

	// Name is what this session answers to, so one address can serve several. A
	// dialer sends one before the handshake and one that named another session is
	// refused. Empty answers a dial that named none, which is an address with one
	// session on it — a two-terminal lobby, a harness, a host somebody started by hand.
	Name string

	// Scenario answers a joiner that asked for the session's scenario by digest,
	// returning the deflated canonical form. The format is the caller's; this
	// package carries bytes. Nil serves nothing, which refuses the request rather
	// than pretending the transfer is not part of the protocol.
	Scenario func(digest string) ([]byte, error)
}

// MaxSessionName is the longest name a session may answer to. A name is a short
// link component, and the bound is what a caller validates one against before it
// reaches a host or a link; ReadSessionRoute refuses a longer one unread.
const MaxSessionName = 64

// HostAcceptor returns a pre-world handshake for Transport's accept loop. Each
// accepted connection gets its own identity, so the roster grows with the lobby
// rather than being fixed at two, and gets it only at its seat: a dialer that
// prepares, and whose player does not press within the ready period, leaves having
// held nothing.
func HostAcceptor(c Coordinator, timeout time.Duration) func(net.Conn) (PeerID, error) {
	return func(conn net.Conn) (id PeerID, err error) {
		// Before Admit, because a link that named another session costs the budget
		// Admit holds for the players of this one.
		if err = readSessionName(conn, c.Name, timeout); err != nil {
			RefuseJoin(conn, err, timeout)
			return 0, err
		}
		if c.Admit != nil {
			if err = c.Admit(conn.RemoteAddr()); err != nil {
				// Answered for the same reason a refused Assign is: a dialer that
				// can read why it was turned away can back off, where one that only
				// sees the stream end retries immediately and makes the condition
				// the refusal exists to stop. The host's own report names the address.
				RefuseJoin(conn, err, timeout)
				return 0, fmt.Errorf("%s: %w", conn.RemoteAddr(), err)
			}
		}
		claims, err := askClaims(conn, timeout)
		if err != nil {
			return 0, err
		}
		h := hostHandshake{c: c, conn: conn, timeout: timeout}
		if err = h.awaitSeat(claims); err != nil {
			return 0, err
		}
		o, err := c.Assign(claims)
		if err != nil {
			// Return the admission reason so a dialer can distinguish retryable handoff.
			RefuseJoin(conn, err, timeout)
			return 0, err
		}
		defer func() {
			if err != nil && c.Release != nil {
				c.Release(o.Assigned)
			}
		}()
		if err = o.Validate(); err != nil {
			return 0, err
		}
		if timeout > 0 {
			_ = conn.SetDeadline(time.Now().Add(timeout))
			defer conn.SetDeadline(time.Time{})
		}
		body, err := json.Marshal(o)
		if err != nil {
			return 0, err
		}
		if err = NewMessage(MsgJoinOffer, body).Encode(conn); err != nil {
			return 0, err
		}
		msg, err := Decode(conn)
		if err != nil {
			return 0, err
		}
		// A dialer that seated at once asks for the scenario here, as it would have
		// while preparing: before it builds a world and so before it reports an
		// identity this coordinator would refuse it on.
		if msg.Type == MsgScenarioRequest && !h.asked {
			h.asked = true
			if err = serveScenario(conn, c, string(msg.Payload), timeout); err != nil {
				return 0, err
			}
			if msg, err = Decode(conn); err != nil {
				return 0, err
			}
		}
		if msg.Type != MsgJoinReply {
			return 0, fmt.Errorf("join handshake: got message %#x, want reply", msg.Type)
		}
		var reply sessionReply
		if err = json.Unmarshal(msg.Payload, &reply); err != nil {
			return 0, fmt.Errorf("join handshake reply: %w", err)
		}
		if err = reply.Refused(); err != nil {
			return 0, err
		}
		// The joiner reads everything after its reply off the compressed stream.
		refuse := func(cause error) { RefuseJoin(openFlateStream(conn, 0, 0), cause, timeout) }
		// Verify before Report so an incompatible joiner cannot affect session state.
		// It is told in its own terms: to it, its build is the one here.
		if err = o.Identity.Verify(reply.Identity); err != nil {
			refuse(reply.Identity.Verify(o.Identity))
			return 0, err
		}
		if c.Report != nil {
			if err = c.Report(o.Assigned, JoinerReport{
				Width: reply.Width, Height: reply.Height, Listen: reply.Listen,
				Remote: conn.RemoteAddr().String(), Holder: reply.Holder,
				Claim: reply.Claim, Record: reply.Record,
			}); err != nil {
				refuse(err)
				return 0, err
			}
		}
		return o.Assigned, nil
	}
}

// hostHandshake is one dialer's handshake as the coordinator runs it.
type hostHandshake struct {
	c       Coordinator
	conn    net.Conn
	timeout time.Duration
	asked   bool // the one scenario request this handshake has is spent
}

// awaitSeat previews the session to the dialer and waits, holding nothing for it,
// for its seat: at once, or after it prepared and its player pressed within the
// ready period. Preparing is a world build, bounded as the frames around it are; the
// ready period is a person's, and running out of it drops the candidate.
func (h *hostHandshake) awaitSeat(claims []string) error {
	var preview SessionOffer
	if h.c.Preview != nil {
		var err error
		if preview, err = h.c.Preview(claims); err != nil {
			RefuseJoin(h.conn, err, h.timeout)
			return err
		}
	}
	body, err := json.Marshal(preview)
	if err != nil {
		return err
	}
	defer h.conn.SetDeadline(time.Time{})
	if h.timeout > 0 {
		_ = h.conn.SetDeadline(time.Now().Add(h.timeout))
	}
	if err := NewMessage(MsgJoinPreview, body).Encode(h.conn); err != nil {
		return err
	}
	window, prepared := h.timeout, false
	for {
		if window > 0 {
			_ = h.conn.SetDeadline(time.Now().Add(window))
		}
		msg, err := Decode(h.conn)
		if ne := net.Error(nil); prepared && errors.As(err, &ne) && ne.Timeout() {
			err = &Refusal{Code: RefusalUnpressed, Reason: fmt.Sprintf("no press came within %s", window)}
			RefuseJoin(h.conn, err, h.timeout)
			return err
		}
		if err != nil {
			return err
		}
		switch {
		case msg.Type == MsgJoinSeat:
			return nil
		case msg.Type == MsgScenarioRequest && !h.asked && !prepared:
			h.asked = true
			if err := serveScenario(h.conn, h.c, string(msg.Payload), h.timeout); err != nil {
				return err
			}
		case msg.Type == MsgJoinPrepared && !prepared:
			prepared = true
			window = cmp.Or(h.c.PressWindow, parameter.NetworkJoinPressWindow)
			if h.c.Prepared != nil {
				h.c.Prepared(h.conn.RemoteAddr())
			}
		case msg.Type == MsgJoinReply:
			// The dialer gave up before its seat, and says why.
			var reply sessionReply
			if err := json.Unmarshal(msg.Payload, &reply); err != nil {
				return fmt.Errorf("join handshake reply: %w", err)
			}
			if err := reply.Refused(); err != nil {
				return err
			}
			return errors.New("join handshake: accepted before any seat")
		default:
			return fmt.Errorf("join handshake: got message %#x, want a seat", msg.Type)
		}
	}
}

// refusalFrom reads the reason out of a rejection frame, so a refused dialer says
// what it was told rather than that it was told something.
func refusalFrom(msg *Message) error {
	var reply sessionReply
	if err := json.Unmarshal(msg.Payload, &reply); err != nil || reply.Error == "" {
		return errors.New("refused by the host with no reason given")
	}
	return fmt.Errorf("refused by the host: %w", reply.Refused())
}

// serveScenario answers one scenario request with the chunked body. The deadline is
// renewed per chunk: the handshake's single window is sized for frames a session
// exchanges, and this is the one that is a function of what is being played.
func serveScenario(conn net.Conn, c Coordinator, digest string, timeout time.Duration) error {
	if c.Scenario == nil {
		err := errors.New("this coordinator does not serve its scenario")
		RefuseJoin(conn, err, timeout)
		return err
	}
	body, err := c.Scenario(digest)
	if err != nil {
		RefuseJoin(conn, err, timeout)
		return err
	}
	chunks, err := EncodeSnapshotChunks(0, 0, body)
	if err != nil {
		return err
	}
	for i, chunk := range chunks {
		if timeout > 0 {
			_ = conn.SetDeadline(time.Now().Add(timeout))
		}
		if err := NewMessage(MsgScenarioBody, chunk).Encode(conn); err != nil {
			return fmt.Errorf("scenario chunk %d/%d: %w", i+1, len(chunks), err)
		}
	}
	return nil
}

// readSessionName consumes the dialer's routing frame. The name is not echoed
// back: it is a stranger's bytes, the dialer already knows what it sent, and the
// one thing worth saying is that this is not the session it named.
func readSessionName(conn net.Conn, name string, timeout time.Duration) error {
	if timeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(timeout))
		defer conn.SetReadDeadline(time.Time{})
	}
	got, err := ReadSessionRoute(conn)
	if err != nil {
		return err
	}
	if got != name {
		return &Refusal{Code: RefusalUnknown, Reason: "this address is not serving the session that was dialled"}
	}
	return nil
}

// ReadSessionRoute reads the frame a dialer sends before it reads and returns the
// name it carries. A named host checks it; a front door places the connection on it.
func ReadSessionRoute(r io.Reader) (string, error) {
	var header [HeaderSize]byte
	msg, err := decode(r, MaxSessionName, header[:])
	if err != nil {
		return "", err
	}
	if msg.Type != MsgSessionRoute {
		return "", fmt.Errorf("join handshake: got message %#x, want a session name", msg.Type)
	}
	return string(msg.Payload), nil
}

// claimTokenBytes is a claim token's randomness, and its digest's length: past
// guessing for as long as any Wraith waits.
const claimTokenBytes = sha256.Size

// maxClaimsFrame bounds a dialer's claims answer: ClaimsKept tokens as JSON.
const maxClaimsFrame = parameter.ClaimsKept*(2*claimTokenBytes+3) + 2

// NewClaimToken is a token a participant comes back to its place with. The roster
// carries its digest alone, so a peer reading the roster cannot take another's
// place; only the participant names the token, on its way to the coordinator. The
// port is unauthenticated by decision: this keeps a place from another player, and
// is no credential against the wire.
func NewClaimToken() string {
	b := make([]byte, claimTokenBytes)
	_, _ = rand.Read(b) // crypto/rand.Read does not fail
	return hex.EncodeToString(b)
}

// ClaimDigest is the digest a roster entry carries for its participant's token.
func ClaimDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ValidClaim reports whether s has the form a token and a digest share.
func ValidClaim(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil && len(s) == 2*claimTokenBytes
}

// askClaims asks a dialer which places it can come back to and reads the tokens it
// keeps. Asked before a place is assigned, so a full session still takes back a
// player whose Wraith keeps one.
func askClaims(conn net.Conn, timeout time.Duration) ([]string, error) {
	if timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(timeout))
		defer conn.SetDeadline(time.Time{})
	}
	if err := NewMessage(MsgJoinClaims, nil).Encode(conn); err != nil {
		return nil, err
	}
	var header [HeaderSize]byte
	msg, err := decode(conn, maxClaimsFrame, header[:])
	if err != nil {
		return nil, err
	}
	if msg.Type != MsgJoinClaims {
		return nil, fmt.Errorf("join handshake: got message %#x, want claims", msg.Type)
	}
	var tokens []string
	if err := json.Unmarshal(msg.Payload, &tokens); err != nil {
		return nil, fmt.Errorf("join handshake claims: %w", err)
	}
	if len(tokens) > parameter.ClaimsKept || slices.ContainsFunc(tokens, func(t string) bool { return !ValidClaim(t) }) {
		return nil, errors.New("join handshake: the dialer named claims of another form")
	}
	return tokens, nil
}

// RefusalCode is the kind of a join refusal, which a dialer acts on rather than on
// its wording. Zero is a refusal that names no kind this build knows.
type RefusalCode uint8

const (
	RefusalFull      RefusalCode = iota + 1 // no room for another participant
	RefusalStarting                         // the lobby is closing on its roster
	RefusalHandoff                          // the session is electing an authority
	RefusalEnding                           // the session is draining or has expired
	RefusalIdentity                         // another build, or another session
	RefusalBudget                           // this address has joined too often
	RefusalBehind                           // the join could not catch up with the session
	RefusalUnknown                          // no such session at this address
	RefusalYielding                         // full, and a Wraith is giving up its place
	RefusalReturning                        // the claimed place is still linked or already coming back
	RefusalUnpressed                        // the dialer's player did not press within the ready period
)

// Retry is what a refused dialer can do about it.
type Retry uint8

const (
	RetryLater Retry = iota // a person may retry, and a rejoin does; a seat does not
	RetrySoon               // it passes in moments, so seats and rejoins retry
	RetryNever              // the same dial is refused again
)

// refusals is what each kind means to its dialer, indexed by RefusalCode: whether
// trying again can help, and the advice its player reads after the reason.
var refusals = [...]struct {
	retry  Retry
	advice string
}{
	RefusalFull:      {RetryLater, "retry when a player leaves"},
	RefusalStarting:  {RetrySoon, "retry in a moment"},
	RefusalHandoff:   {RetrySoon, "retry in a moment"},
	RefusalEnding:    {RetryNever, ""},
	RefusalIdentity:  {RetryNever, ""},
	RefusalBudget:    {RetryLater, "retry in a minute"},
	RefusalBehind:    {RetrySoon, "retry in a moment"},
	RefusalUnknown:   {RetryNever, ""},
	RefusalYielding:  {RetrySoon, "retry in a moment"},
	RefusalReturning: {RetrySoon, "retry in a moment"},
	RefusalUnpressed: {RetrySoon, "press again to join"},
}

// Refusal is a join turned away. Code is the kind and Reason what the player
// reads, to which Error adds the advice for its kind; detail belongs in Reason,
// since the advice ends the sentence. One refusal matches another of its kind.
type Refusal struct {
	Code   RefusalCode
	Reason string
}

func (r *Refusal) Error() string {
	if advice := refusals[r.Code].advice; advice != "" {
		return r.Reason + "; " + advice
	}
	return r.Reason
}

func (r *Refusal) Is(target error) bool {
	t, ok := target.(*Refusal)
	return ok && t.Code == r.Code
}

// RetryOf is what a dialer refused with err can do: its kind's entry, or
// RetryLater for a failure that names no kind.
func RetryOf(err error) Retry {
	var r *Refusal
	if errors.As(err, &r) {
		return refusals[r.Code].retry
	}
	return RetryLater
}

// refusalField is a refusal as a frame carries it: the code, and the reason
// without the advice, which the reader's own build adds.
type refusalField struct {
	Code  RefusalCode `json:"code,omitempty"`
	Error string      `json:"error,omitempty"`
}

// Refuse makes err the refusal this frame carries.
func (f *refusalField) Refuse(err error) {
	f.Code, f.Error = 0, err.Error()
	var r *Refusal
	if errors.As(err, &r) {
		f.Code, f.Error = r.Code, r.Reason
	}
}

// Refused is the refusal this frame carries, nil when it carries none, and a
// Refusal when its kind is one this build knows.
func (f refusalField) Refused() error {
	switch {
	case f.Error == "":
		return nil
	case f.Code == 0 || int(f.Code) >= len(refusals):
		return errors.New(f.Error)
	}
	return &Refusal{Code: f.Code, Reason: f.Error}
}

// RefuseJoin writes one rejection, which the dialer reads as its reason; a
// compressed stream is flushed, since the dialer reads nothing it still holds. A
// failure to write it is not worth reporting: the connection is being closed
// either way, and the joiner falls back to reading the stream's end.
func RefuseJoin(conn net.Conn, cause error, timeout time.Duration) {
	var reply sessionReply
	reply.Refuse(cause)
	body, err := json.Marshal(reply)
	if err != nil {
		return
	}
	if timeout > 0 {
		_ = conn.SetWriteDeadline(time.Now().Add(timeout))
		defer conn.SetWriteDeadline(time.Time{})
	}
	if err = NewMessage(MsgJoinReply, body).Encode(conn); err == nil {
		if stream, ok := conn.(*flateStream); ok {
			_ = stream.Flush()
		}
	}
}

// PendingJoin owns a dialled stream until the startup gate transfers it to a port.
type PendingJoin struct {
	conn        *flateStream
	base        Config
	offer       SessionOffer
	replied     bool
	started     bool
	transferred bool

	// Keep session traffic received before world installation for the first poll.
	deferred []*Message

	// gateBytes is what this stream has decoded during the handshake. It answers a
	// probe that arrives mid-gate, so a host measuring this link sees a running
	// counter rather than a silence it would have to score as loss.
	gateBytes uint64

	// asked records that this stream has already spent its one scenario request.
	asked bool

	// prepared is when this dialer said it was ready for its press, which opened the
	// coordinator's ready period; zero before, and for a dialer that seats at once.
	prepared time.Time
}

// RequestScenario asks the coordinator for the scenario the preview named and
// returns the deflated body. One request per handshake, made while preparing or,
// seated at once, before the reply: the coordinator reads what follows the answer as
// the next step, so a second would be a protocol error on both sides.
func (p *PendingJoin) RequestScenario(digest string) ([]byte, error) {
	switch {
	case p.replied:
		return nil, errors.New("join handshake already completed")
	case p.asked:
		return nil, errors.New("join handshake already asked for a scenario")
	case !p.prepared.IsZero():
		return nil, errors.New("join handshake already prepared on its scenario")
	}
	p.asked = true
	if p.base.WriteTimeout > 0 {
		_ = p.conn.SetWriteDeadline(time.Now().Add(p.base.WriteTimeout))
	}
	err := NewMessage(MsgScenarioRequest, []byte(digest)).Encode(p.conn)
	_ = p.conn.SetWriteDeadline(time.Time{})
	if err != nil {
		return nil, err
	}
	_, body, err := readChunked(p, MsgScenarioBody)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// Deferred returns the session traffic read off the stream during the gate, oldest
// first. Every frame came from Peer, the only one this stream has. Valid after
// Ready; the slice is not reused.
func (p *PendingJoin) Deferred() []*Message { return p.deferred }

// Peer names the participant on the other end of this stream: the coordinator, or
// the relay the offer came through.
func (p *PendingJoin) Peer() PeerID { return cmp.Or(p.offer.Relay, p.offer.Host) }

// hold buffers one session frame that arrived out of the handshake's turn, and
// reports whether it was one. Anything the handshake itself expects is left to the
// caller, and an unrecognised type is a protocol error rather than something to
// stash.
func (p *PendingJoin) hold(msg *Message) bool {
	p.gateBytes += uint64(HeaderSize + len(msg.Payload))
	switch msg.Type {
	case MsgLinkProbe:
		// Answer probes during admission so snapshot transfer is not measured as loss.
		// An empty pre-world report allows the host to rebase its counters.
		p.answerProbe(msg)
		return true
	case MsgLinkEcho:
		// This end sends no probes during the gate, so an echo here is a stray from
		// a previous connection. Nothing to fold it into.
		return true
	case MsgStateCorrection, MsgStateManifest, MsgStateRequest, MsgStateShard, MsgStateUnserved:
		// Discard corrections and selective requests for the uninstalled world.
		// Admission supplies the baseline that subsequent traffic may reference.
		return true
	case MsgAuthorityReport, MsgAuthorityHandoff, MsgPeerList:
		// Held rather than swallowed. These say who is allowed to author, which is
		// exactly what a joiner needs and cannot re-derive: it adopts a term from
		// the offer, and a succession that ran between the offer and the install
		// would otherwise leave it following an authority that has stopped.
		if len(p.deferred) >= maxDeferredJoinFrames {
			p.deferred = append(p.deferred[:0], p.deferred[1:]...)
		}
		p.deferred = append(p.deferred, msg)
		return true
	case MsgEvent, MsgStateSync, MsgBundle, MsgStateDigest, MsgDisconnect:
		// Bound peer-controlled admission buffering; retain the newest traffic.
		if len(p.deferred) >= maxDeferredJoinFrames {
			p.deferred = append(p.deferred[:0], p.deferred[1:]...)
		}
		p.deferred = append(p.deferred, msg)
		return true
	}
	return false
}

// next reads the stream's next frame within ReadTimeout. Past the offer the host
// streams or probes it, so a stream silent that long has lost its host.
func (p *PendingJoin) next() (*Message, error) {
	if p.base.ReadTimeout > 0 {
		_ = p.conn.SetReadDeadline(time.Now().Add(p.base.ReadTimeout))
	}
	var header [HeaderSize]byte
	msg, err := p.conn.readFrame(MaxPayloadSize, header[:])
	if ne := net.Error(nil); errors.As(err, &ne) && ne.Timeout() {
		return nil, plainError{"the host stopped answering", err}
	}
	return msg, err
}

// answerProbe replies to one link probe read off the handshake stream. A failed
// write is not the join's problem: the probe is a measurement and losing one
// costs an estimate, where raising it here would fail a join over telemetry.
func (p *PendingJoin) answerProbe(msg *Message) {
	echo := encodeEcho(msg.Payload, p.gateBytes, LinkReport{}, time.Since(probeEpoch))
	if echo == nil {
		return
	}
	if p.base.WriteTimeout > 0 {
		_ = p.conn.SetWriteDeadline(time.Now().Add(p.base.WriteTimeout))
		defer p.conn.SetWriteDeadline(time.Time{})
	}
	_ = p.send(NewMessage(MsgLinkEcho, echo))
}

// send writes one frame, flushing it through the stream once the join has opened
// one.
func (p *PendingJoin) send(msg *Message) error {
	if err := msg.Encode(p.conn); err != nil {
		return err
	}
	return p.conn.Flush()
}

// maxDeferredJoinFrames bounds what one join may buffer off its stream.
const maxDeferredJoinFrames = 512

// PreviewSession dials addr and reads the session it would join, which holds
// nothing for this dial yet: its identity, slot and claim are assigned at Seat. The
// build half of the identity is checked here, before a world is built on the
// preview; the session half once one is.
func PreviewSession(addr string, cfg *Config) (*PendingJoin, SessionOffer, error) {
	p, err := openJoinStream(addr, cfg)
	if err != nil {
		return nil, SessionOffer{}, err
	}
	if err := p.offer.ValidatePreview(); err != nil {
		_ = p.conn.Close()
		return nil, SessionOffer{}, err
	}
	if p.base.Identity.Protocol != 0 {
		if err := p.base.Identity.VerifyBuild(p.offer.Identity); err != nil {
			_ = p.conn.Close()
			return nil, SessionOffer{}, err
		}
	}
	return p, p.offer, nil
}

// DialSession connects and takes its place at once, preparing on no preview: a
// harness, or a coordinator that previews nothing. The scenario, if asked for, comes
// after the offer.
func DialSession(addr string, cfg *Config) (*PendingJoin, SessionOffer, error) {
	p, err := openJoinStream(addr, cfg)
	if err != nil {
		return nil, SessionOffer{}, err
	}
	offer, err := p.Seat()
	if err != nil {
		_ = p.conn.Close()
		return nil, SessionOffer{}, err
	}
	return p, offer, nil
}

// openJoinStream dials addr, names the session and the claims the dial keeps, and
// reads the preview the coordinator answers with, unchecked: a coordinator that
// previews nothing is still one a dial can seat at once with.
func openJoinStream(addr string, cfg *Config) (*PendingJoin, error) {
	base := DefaultConfig()
	if cfg != nil {
		copy := *cfg
		base = &copy
	}
	base.Role, base.Address = RolePeer, addr
	conn, err := dial(addr, base)
	if err != nil && base.Dial == nil && base.SessionName != "" && !routeAnswered(err) {
		conn, err = dialListed(base, err)
	}
	if err != nil {
		return nil, err
	}
	if base.ConnectTimeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(base.ConnectTimeout))
	}
	// Before the preview is read, because the coordinator reads this before it writes
	// one and a front door has to place the connection before either happens; sent
	// unnamed too, so a port that also takes peer links tells this is a join.
	if err = NewMessage(MsgSessionRoute, []byte(base.SessionName)).Encode(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	msg, err := Decode(conn)
	if err == nil && msg.Type == MsgJoinClaims {
		msg, err = answerClaims(conn, base.Claims)
	}
	_ = conn.SetDeadline(time.Time{})
	if err != nil {
		_ = conn.Close()
		return nil, plainError{"connected, but no vif session answered there", err}
	}
	if msg.Type == MsgJoinReply {
		// Refused before anything was held for it. The coordinator says why, and the
		// kind it names is what a caller decides a retry on (RetryOf).
		_ = conn.Close()
		return nil, refusalFrom(msg)
	}
	if msg.Type != MsgJoinPreview {
		_ = conn.Close()
		return nil, fmt.Errorf("join handshake: got message %#x, want a preview", msg.Type)
	}
	var preview SessionOffer
	if err := json.Unmarshal(msg.Payload, &preview); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("join handshake preview: %w", err)
	}
	return &PendingJoin{conn: &flateStream{Conn: conn}, base: *base, offer: preview}, nil
}

// Prepared tells the coordinator this dialer has built its world on the preview and
// waits for its player's press, which opens the ready period: Seat within it.
func (p *PendingJoin) Prepared() error {
	switch {
	case p.replied || p.offer.Assigned != 0:
		return errors.New("join handshake already seated")
	case !p.prepared.IsZero():
		return errors.New("join handshake already prepared")
	}
	if p.base.WriteTimeout > 0 {
		_ = p.conn.SetWriteDeadline(time.Now().Add(p.base.WriteTimeout))
		defer p.conn.SetWriteDeadline(time.Time{})
	}
	if err := NewMessage(MsgJoinPrepared, nil).Encode(p.conn); err != nil {
		return err
	}
	p.prepared = time.Now() // [wall] a person's wait, measured as the coordinator measures it
	return nil
}

// ErrPressLapsed is a seat asked for after the ready period, which the coordinator
// has ended by now: the stream is not tried, and a dial is what seats.
var ErrPressLapsed = &Refusal{Code: RefusalUnpressed, Reason: "the press came after the host's ready period"}

// Seat asks for this dialer's place, which the coordinator assigns now, and returns
// the offer carrying it. A dialer that prepared seats within the ready period or not
// at all on this stream. The offer is the preview's session as it stands at the seat,
// which the caller compares with the world it prepared.
func (p *PendingJoin) Seat() (SessionOffer, error) {
	switch {
	case p.replied || p.offer.Assigned != 0:
		return SessionOffer{}, errors.New("join handshake already seated")
	case !p.prepared.IsZero() && time.Since(p.prepared) >= parameter.NetworkJoinPressWindow:
		return SessionOffer{}, ErrPressLapsed
	}
	if p.base.ConnectTimeout > 0 {
		_ = p.conn.SetDeadline(time.Now().Add(p.base.ConnectTimeout))
		defer p.conn.SetDeadline(time.Time{})
	}
	if err := NewMessage(MsgJoinSeat, nil).Encode(p.conn); err != nil {
		return SessionOffer{}, plainError{"the host dropped the join before its seat", err}
	}
	msg, err := Decode(p.conn)
	switch {
	case err != nil:
		return SessionOffer{}, plainError{"the host dropped the join before its seat", err}
	case msg.Type == MsgJoinReply:
		return SessionOffer{}, refusalFrom(msg)
	case msg.Type != MsgJoinOffer:
		return SessionOffer{}, fmt.Errorf("join handshake: got message %#x, want offer", msg.Type)
	}
	var offer SessionOffer
	if err := json.Unmarshal(msg.Payload, &offer); err != nil {
		return SessionOffer{}, fmt.Errorf("join handshake offer: %w", err)
	}
	if err := offer.Validate(); err != nil {
		return SessionOffer{}, err
	}
	// Checked again, since a dial that seats at once checks it here alone.
	if p.base.Identity.Protocol != 0 {
		if err := p.base.Identity.VerifyBuild(offer.Identity); err != nil {
			return SessionOffer{}, err
		}
	}
	if p.offer.Host != 0 && (offer.Host != p.offer.Host || offer.Relay != p.offer.Relay) {
		return SessionOffer{}, fmt.Errorf("join handshake: seated by participant %d through %d, previewed by %d through %d",
			offer.Host, offer.Relay, p.offer.Host, p.offer.Relay)
	}
	p.offer = offer
	return offer, nil
}

// answerClaims names the tokens this dial comes back with and reads the frame the
// coordinator answers them with.
func answerClaims(conn net.Conn, tokens []string) (*Message, error) {
	body, err := json.Marshal(tokens)
	if err != nil {
		return nil, err
	}
	if err := NewMessage(MsgJoinClaims, body).Encode(conn); err != nil {
		return nil, err
	}
	return Decode(conn)
}

// dialListed dials base's session by another link its site lists, once nothing
// answered at its own address: a player may hold the link another kind of client
// joins by. With no site answering, the failure of the address it was given stands.
func dialListed(base *Config, failed error) (net.Conn, error) {
	links, err := listedLinks(base.Address, base.SessionName, base.ConnectTimeout)
	if err != nil {
		return nil, err
	}
	for _, link := range links {
		e, err := ParseEndpoint(link)
		if err != nil || e.Addr == base.Address {
			continue
		}
		conn, err := dial(e.Addr, base)
		if err == nil {
			base.Address = e.Addr
			return conn, nil
		}
		failed = fmt.Errorf("%s: %w", link, err)
	}
	return nil, failed
}

// TransportConfig returns a client config that adopts the negotiated identity and
// the join's stream. Before the seat there is no identity, and the stream stays this
// join's until the port takes it (SocketPort.TakeJoin).
func (p *PendingJoin) TransportConfig() *Config {
	cfg := p.base
	cfg.Role = RolePeer
	cfg.Dial = nil // the join's own stream; a later peer link dials its address
	if p.offer.Assigned != 0 {
		cfg.ParticipantID = p.offer.Assigned
		cfg.preconnected, cfg.preconnectedPeer = p.conn, p.Peer()
	}
	return &cfg
}

// Complete returns a rejection unchanged or admits the stream for the start gate.
// The report travels with the acceptance and is ignored on a rejection, which
// carries no participant to describe. A rejection before the seat gives up a
// handshake that held nothing; an acceptance needs the seat.
func (p *PendingJoin) Complete(joinErr error, report JoinerReport) error {
	switch {
	case p.replied:
		return errors.New("join handshake already completed")
	case joinErr == nil && p.offer.Assigned == 0:
		return errors.New("join handshake has no seat to accept")
	}
	p.replied = true
	reply := sessionReply{}
	if joinErr != nil {
		reply.Refuse(joinErr)
	} else {
		reply.Width, reply.Height = report.Width, report.Height
		reply.Identity = report.Identity
		reply.Listen, reply.Holder = report.Listen, report.Holder
		reply.Claim, reply.Record = report.Claim, report.Record
	}
	body, err := json.Marshal(reply)
	if err == nil {
		if p.base.WriteTimeout > 0 {
			_ = p.conn.SetWriteDeadline(time.Now().Add(p.base.WriteTimeout))
			defer p.conn.SetWriteDeadline(time.Time{})
		}
		err = NewMessage(MsgJoinReply, body).Encode(p.conn)
	}
	if joinErr != nil {
		_ = p.conn.Close()
		return joinErr
	}
	// Everything after the reply travels on the link's compressed stream, the
	// coordinator's side of which opens as it reads this reply.
	openFlateStream(p.conn, p.base.ReadBufferSize, p.base.WriteBufferSize)
	return err
}

// WaitStart returns the closed roster before any cursor is built, preserving shared
// entity creation order. Human lobby time has no deadline, but the host probes the
// stream throughout, so a lost host ends it as a closed stream does.
func (p *PendingJoin) WaitStart() (SessionOffer, error) {
	if !p.replied {
		return SessionOffer{}, errors.New("join handshake has no reply")
	}
	if p.started {
		return SessionOffer{}, errors.New("join start gate already received")
	}
	defer p.conn.SetReadDeadline(time.Time{})
	var msg *Message
	for {
		var err error
		if msg, err = p.next(); err != nil {
			return SessionOffer{}, err
		}
		if !p.hold(msg) {
			break
		}
	}
	if msg.Type == MsgJoinReply {
		return SessionOffer{}, refusalFrom(msg)
	}
	if msg.Type != MsgStart {
		return SessionOffer{}, fmt.Errorf("join handshake: got message %#x, want start", msg.Type)
	}
	var final SessionOffer
	if err := json.Unmarshal(msg.Payload, &final); err != nil {
		return SessionOffer{}, fmt.Errorf("join start roster: %w", err)
	}
	if err := final.Validate(); err != nil {
		return SessionOffer{}, err
	}
	if final.Assigned != p.offer.Assigned {
		return SessionOffer{}, fmt.Errorf("join start roster assigns participant %d, offered %d",
			final.Assigned, p.offer.Assigned)
	}
	p.offer = final
	p.started = true
	return final, nil
}

// ReceiveSnapshot reads the announced capture after the roster, preserving the
// world-install boundary before buffered session traffic is released.
func (p *PendingJoin) ReceiveSnapshot() (uint64, []byte, error) {
	if !p.started {
		return 0, nil, errors.New("join start gate not received")
	}
	if !p.offer.CarriesSnapshot() {
		return 0, nil, errors.New("join start gate carries no capture")
	}
	tick, body, err := readSnapshot(p)
	if err != nil {
		return 0, nil, fmt.Errorf("join snapshot: %w", err)
	}
	if tick != p.offer.SnapshotTick {
		return 0, nil, fmt.Errorf("join snapshot: arrived for tick %d, gate named %d",
			tick, p.offer.SnapshotTick)
	}
	if len(body) != p.offer.SnapshotBytes {
		return 0, nil, fmt.Errorf("join snapshot: %d bytes arrived, gate named %d",
			len(body), p.offer.SnapshotBytes)
	}
	return tick, body, nil
}

// Ready releases the host and transfers stream ownership to TransportConfig.
func (p *PendingJoin) Ready() error {
	if !p.started {
		return errors.New("join start gate not received")
	}
	if p.transferred {
		return errors.New("join stream already transferred")
	}
	if p.base.WriteTimeout > 0 {
		_ = p.conn.SetWriteDeadline(time.Now().Add(p.base.WriteTimeout))
		defer p.conn.SetWriteDeadline(time.Time{})
	}
	if err := p.send(NewMessage(MsgReady, nil)); err != nil {
		return err
	}
	p.transferred = true
	return nil
}

// Close releases a stream that never reached its socket port.
func (p *PendingJoin) Close() error {
	if p == nil || p.transferred {
		return nil
	}
	return p.conn.Close()
}

// AdmissionLimiter bounds expensive joins with a per-address fixed window.
// Addresses share a budget behind NAT; participant identities can be reused.
type AdmissionLimiter struct {
	mu     sync.Mutex
	window time.Duration
	burst  int
	max    int
	seen   map[string]*admissionCount
}

type admissionCount struct {
	opened time.Time
	count  int
}

func NewAdmissionLimiter() *AdmissionLimiter {
	return NewAdmissionLimiterOf(parameter.NetworkAdmitWindow, parameter.NetworkAdmitBurst)
}

// NewAdmissionLimiterOf counts burst admissions per address in each window.
func NewAdmissionLimiterOf(window time.Duration, burst int) *AdmissionLimiter {
	return &AdmissionLimiter{
		window: window,
		burst:  burst,
		max:    parameter.NetworkAdmitTracked,
		seen:   make(map[string]*admissionCount),
	}
}

// Admit records one dial from addr and reports whether it may proceed.
func (l *AdmissionLimiter) Admit(addr net.Addr) error {
	key := admissionKey(addr)
	if key == "" {
		return nil // nothing to attribute a budget to; the transport is not a socket
	}
	now := time.Now() // [wall] a rate over real time, not a game one

	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.seen[key]
	switch {
	case ok && now.Sub(entry.opened) >= l.window:
		entry.opened, entry.count = now, 0
	case !ok:
		l.sweepLocked(now)
		if len(l.seen) >= l.max {
			// Fails closed: a completed TCP handshake proves the source address, so
			// a table this wide is many real hosts dialling at once rather than one
			// forging them, and a roster of sixteen has no reading of that in which
			// the next dial is the one it was waiting for.
			return &Refusal{Code: RefusalBudget, Reason: fmt.Sprintf("%d other hosts are joining at once", len(l.seen))}
		}
		entry = &admissionCount{opened: now}
		l.seen[key] = entry
	}

	if entry.count >= l.burst {
		return &Refusal{Code: RefusalBudget, Reason: "this address has joined too often"}
	}
	entry.count++
	return nil
}

// Refund takes back one admission of addr, for a dial that started nothing.
func (l *AdmissionLimiter) Refund(addr net.Addr) {
	key := admissionKey(addr)
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry, ok := l.seen[key]; ok && entry.count > 0 {
		entry.count--
	}
}

// Wait is how long until addr may be admitted again, zero when it may now.
func (l *AdmissionLimiter) Wait(addr net.Addr) time.Duration {
	key := admissionKey(addr)
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.seen[key]
	if !ok || entry.count < l.burst {
		return 0
	}
	return max(l.window-time.Since(entry.opened), 0) // [wall] a rate over real time
}

// sweepLocked drops the keys whose window has passed. It runs only when a new key
// arrives, so the table is walked once per unseen host rather than per dial.
// Caller MUST hold mu.
func (l *AdmissionLimiter) sweepLocked(now time.Time) {
	for key, entry := range l.seen {
		if now.Sub(entry.opened) >= l.window {
			delete(l.seen, key)
		}
	}
}

// admissionKey is the host half of a network address, an IPv6 host's /64. The port
// and an IPv6 address's low half are what a dialer changes for free, so counting
// either would count nothing.
func admissionKey(addr net.Addr) string {
	if _, piped := addr.(inProcess); addr == nil || piped {
		return ""
	}
	host := addr.String()
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if ip, err := netip.ParseAddr(host); err == nil && ip.Is6() && !ip.Is4In6() {
		return netip.PrefixFrom(ip.WithZone(""), 64).Masked().String()
	}
	return host
}
