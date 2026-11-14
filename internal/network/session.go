package network

import (
	"cmp"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lixenwraith/auth"
	link "github.com/lixenwraith/vif/pkg/network"

	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// RosterEntry is one identity the coordinator has admitted, and the cursor slot it
// drives. NoPlayerSlot means it drives none, which is what a dedicated coordinator
// holds: in the roster, and not one of the session's participants. Away is one that
// left and has a Wraith waiting in its slot: both stay taken, and nothing waits on it.
// Key is the public half of its participant key (EncodeKey), which a dial proves to
// come back to the place and a peer link or a succession message is signed with.
type RosterEntry struct {
	ID     link.PeerID `json:"id"`
	Slot   uint8       `json:"slot"`
	Holder link.PeerID `json:"holder,omitempty"`
	Away   bool        `json:"away,omitempty"`
	Key    string      `json:"key,omitempty"`
	// Account and Name are the site's, copied from a verified ticket or the pass that
	// carried them on; an anonymous participant has neither and keeps the slot's label.
	Account string `json:"account,omitempty"`
	Name    string `json:"name,omitempty"`
}

// SessionOffer carries the replay identity and the coordinator-owned roster.
type SessionOffer struct {
	Anchor   event.JoinAnchor `json:"anchor"`
	Host     link.PeerID      `json:"host"`
	Assigned link.PeerID      `json:"assigned"`

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

	// TermKeys is the authority key of every term since the first, the session's
	// identity across its successors, which a pass is verified against (TermKey);
	// KeepOut the keys and accounts the authority dropped, refused at every door.
	// Both move whole, as the chain does, so a joiner holds what a successor needs.
	TermKeys []string `json:"term_keys,omitempty"`
	KeepOut  []string `json:"keep_out,omitempty"`

	// Pass is the admission the seat gives the dial to come back with (PassBody),
	// signed by the authority under Term; none when the dial showed one still fresh.
	Pass *Signed `json:"pass,omitempty"`

	// Gated says the session's doors take a passphrase or a pass, never a bare key;
	// Door is the passphrase's verifier (DoorVerifier), which a relay's and a
	// successor's doors serve the exchange from. Gated with no Door admits a pass alone.
	Gated bool          `json:"gated,omitempty"`
	Door  *DoorVerifier `json:"door,omitempty"`

	// Relay is the participant on the far end of the joiner's stream when that is not
	// the host: the holder a bot joined through. Zero on a dial to the coordinator.
	Relay link.PeerID `json:"relay,omitempty"`

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
	Listen string      `json:"listen,omitempty"`
	Holder link.PeerID `json:"holder,omitempty"`

	Record *event.CursorRecord `json:"record,omitempty"`
}

// JoinerReport declares simulation identity, bot holder, and advisory geometry.
// The coordinator validates it before accepting the participant.
type JoinerReport struct {
	Holder link.PeerID
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

	// Record is what a participant coming back to its place brings of the game it
	// left, and nothing for a new place.
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
	RelayDoor                         // the relay's door is open at Door: name it in the chain, unanswered
	RelayNonce                        // a challenge for the dial the relay's door is about to ask
)

// RelayJoin is a relay's request. Nonce pairs it with its answer; Leaf names the
// participant once Assign has. RelayReady carries what the participant reported,
// its identity checked against the offer as a coordinator checks a dial's reply;
// Assign carries the keys the dial proved over Challenge, the nonce RelayNonce was
// answered with, so the coordinator checks the proofs itself and the relay can take
// nobody's place; Door is where the relay admits people, completed from its
// connection as a port is.
type RelayJoin struct {
	Step      RelayStep    `json:"step"`
	Nonce     uint32       `json:"nonce"`
	Leaf      link.PeerID  `json:"leaf,omitempty"`
	Keys      []Proof      `json:"keys,omitempty"`
	Pass      *Signed      `json:"pass,omitempty"`
	Challenge string       `json:"challenge,omitempty"`
	Report    JoinerReport `json:"report,omitzero"`
	Door      string       `json:"door,omitempty"`
}

// RelayAdmit answers one RelayJoin: the offer Assign allocated, the challenge
// RelayNonce asked for, or why not.
type RelayAdmit struct {
	Nonce     uint32       `json:"nonce"`
	Offer     SessionOffer `json:"offer,omitzero"`
	Challenge string       `json:"challenge,omitempty"`
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
	ids := make(map[link.PeerID]bool, len(o.Roster))
	slots := make(map[uint8]bool, len(o.Roster))
	cursorless := link.PeerID(0)
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
func (o SessionOffer) Entry(id link.PeerID) (RosterEntry, bool) {
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
// keys the dialer proved comes back to; Release returns that identity to the pool, or
// that place to its Wraith, when the handshake does not complete, so a rejected or
// abandoned connection does not consume a roster slot. Assign runs at the dialer's
// seat, never before: a dialer prepares on a preview, which holds nothing.
type Coordinator struct {
	Assign  func(a Admission) (SessionOffer, error)
	Release func(link.PeerID)

	// Preview is the session as a dialer proving keys would be offered it, with no
	// identity assigned: the offer Assign would make, or the refusal it would give,
	// with nothing allocated or asked for. Nil previews nothing, which only a dialer
	// that seats at once (DialSession) can join on.
	Preview func(a Admission) (SessionOffer, error)

	// Nonce is the challenge the door asks a dial to prove its keys over; nil makes
	// one here. A relay asks its authority for one, so the proofs it carries on are
	// good at the authority once.
	Nonce func() (string, error)

	// Authority is the session's first authority key as the roster carries it, which
	// a pass must name; Pass verifies one a dial showed against the term it names,
	// this session and the keep-out list, given the keys the dial proved, and says
	// why not. Either nil is a door that takes no pass, and its question says so.
	Authority func() string
	Pass      func(shown *Signed, keys []string) (*PassBody, error)

	// Passphrase is the credential this door takes the exchange against as each dial
	// arrives, since a run sets it after its port is made; Gated says the session's
	// doors take no bare key, for a door with none to check, which then admits a pass
	// alone; Binding is the leaf this door serves over TLS (LeafBinding), which the
	// exchange is bound to there.
	Passphrase func() *PassphraseDoor
	Gated      func() bool
	Binding    []byte

	// Ticket verifies the site's ticket a dial carried, for a fleet pod's doors,
	// which then take no bare key (TicketGate.Admit); nil takes none.
	Ticket func(shown *Signed, keys []string) (*TicketBody, error)

	// PressWindow is the ready period a prepared dialer has to seat in; zero is
	// parameter.NetworkJoinPressWindow.
	PressWindow time.Duration

	// Prepared says a dialer from addr is ready and its player has the ready period
	// to press in, so the session can show whom it is about to seat. Nil shows nobody.
	Prepared func(addr net.Addr)

	// Report carries what a joiner said about itself once its acceptance arrives.
	// It runs after Assign and only for a handshake that completed, so what it
	// describes is a participant the session actually holds.
	Report func(link.PeerID, JoinerReport) error

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

	// Watch is the session as a dial that only watches is shown it, which holds no
	// place, or why it cannot be watched: asked for the preview and again at the
	// seat. Feed then owns the dial's stream and copies the session's feed to it.
	// Either nil is a door nobody watches at.
	Watch func(a Admission) (SessionOffer, error)
	Feed  func(conn net.Conn, a Admission) error
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
func HostAcceptor(c Coordinator, timeout time.Duration) func(net.Conn) (link.PeerID, error) {
	return func(conn net.Conn) (id link.PeerID, err error) {
		// Before Admit, because a link that named another session costs the budget
		// Admit holds for the players of this one.
		if err = readSessionName(conn, MsgSessionRoute, c.Name, timeout); err != nil {
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
		adm, err := askAdmission(conn, c, timeout)
		if err != nil {
			return 0, err
		}
		h := hostHandshake{c: c, conn: conn, timeout: timeout}
		if err = h.awaitSeat(adm); err != nil {
			return 0, err
		}
		o, err := c.Assign(adm)
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
		if err = link.NewMessage(MsgJoinOffer, body).Encode(conn); err != nil {
			return 0, err
		}
		msg, err := link.Decode(conn)
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
			if msg, err = link.Decode(conn); err != nil {
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
		refuse := func(cause error) { RefuseJoin(link.OpenStream(conn, &DefaultConfig().Config), cause, timeout) }
		// Verify before Report so an incompatible joiner cannot affect session state.
		// It is told in its own terms: to it, its build is the one here.
		if err = o.Identity.Verify(reply.Identity); err != nil {
			refuse(reply.Identity.Verify(o.Identity))
			return 0, err
		}
		if c.Report != nil {
			if err = c.Report(o.Assigned, JoinerReport{
				Width: reply.Width, Height: reply.Height, Listen: reply.Listen,
				Remote: conn.RemoteAddr().String(), Holder: reply.Holder, Record: reply.Record,
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
func (h *hostHandshake) awaitSeat(a Admission) error {
	var preview SessionOffer
	if h.c.Preview != nil {
		var err error
		if preview, err = h.c.Preview(a); err != nil {
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
	if err := link.NewMessage(MsgJoinPreview, body).Encode(h.conn); err != nil {
		return err
	}
	window, prepared := h.timeout, false
	for {
		if window > 0 {
			_ = h.conn.SetDeadline(time.Now().Add(window))
		}
		msg, err := link.Decode(h.conn)
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
func refusalFrom(msg *link.Message) error {
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
	chunks, err := link.EncodeSnapshotChunks(0, 0, body)
	if err != nil {
		return err
	}
	for i, chunk := range chunks {
		if timeout > 0 {
			_ = conn.SetDeadline(time.Now().Add(timeout))
		}
		if err := link.NewMessage(MsgScenarioBody, chunk).Encode(conn); err != nil {
			return fmt.Errorf("scenario chunk %d/%d: %w", i+1, len(chunks), err)
		}
	}
	return nil
}

// readSessionName consumes the dialer's routing frame. The name is not echoed
// back: it is a stranger's bytes, the dialer already knows what it sent, and the
// one thing worth saying is that this is not the session it named.
func readSessionName(conn net.Conn, kind link.MessageType, name string, timeout time.Duration) error {
	if timeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(timeout))
		defer conn.SetReadDeadline(time.Time{})
	}
	got, err := readRoute(conn, kind)
	if err != nil {
		return err
	}
	if got != name {
		return &Refusal{Code: RefusalUnknown, Reason: "this address is not serving the session that was dialled"}
	}
	return nil
}

// ReadSessionRoute reads the frame a joining dialer sends before it reads and
// returns the name it carries. A named host checks it; a front door places the
// connection on it.
func ReadSessionRoute(r io.Reader) (string, error) { return readRoute(r, MsgSessionRoute) }

// readRoute reads the opening frame of a dial of kind, a join's or a watch's.
func readRoute(r io.Reader, kind link.MessageType) (string, error) {
	msg, err := link.DecodeWithin(r, MaxSessionName)
	if err != nil {
		return "", err
	}
	if msg.Type != kind {
		return "", fmt.Errorf("join handshake: got message %#x, want a session name", msg.Type)
	}
	return string(msg.Payload), nil
}

// ErrUnwatched refuses a watch dial at a door whose session carries no feed.
var ErrUnwatched = &Refusal{Code: RefusalUnknown, Reason: "this door shows no session to watch"}

// ErrUnstarted refuses a watch dial at a session still in its lobby, which holds
// players alone. It is no refusal that passes in moments: a dial retrying one
// would spend its address's budget on a game with nothing to show yet.
var ErrUnstarted = &Refusal{Code: RefusalUnstarted, Reason: "session has not started"}

// WatchAcceptor returns the handshake a door runs for a dial that only watches: the
// route, the budget and the admission a join passes, then the session as it stands
// and its scenario if asked. Its seat is answered by MsgStart, after which the
// stream is the feed's. Nothing in the session is held for it: a watcher has no place.
func WatchAcceptor(c Coordinator, timeout time.Duration) func(net.Conn) error {
	return func(conn net.Conn) error {
		if err := readSessionName(conn, MsgSessionWatch, c.Name, timeout); err != nil {
			RefuseJoin(conn, err, timeout)
			return err
		}
		if c.Admit != nil {
			if err := c.Admit(conn.RemoteAddr()); err != nil {
				RefuseJoin(conn, err, timeout)
				return fmt.Errorf("%s: %w", conn.RemoteAddr(), err)
			}
		}
		return ServeWatch(conn, c, timeout)
	}
}

// ServeWatch is the watch handshake past its route frame and its budget, for a
// front door that read the one to place the dial and charged the other to the
// address it came from: the admission, the session as it stands, the feed.
func ServeWatch(conn net.Conn, c Coordinator, timeout time.Duration) error {
	refuse := func(err error) error {
		RefuseJoin(conn, err, timeout)
		return err
	}
	if c.Watch == nil || c.Feed == nil {
		return refuse(ErrUnwatched)
	}
	adm, err := askAdmission(conn, c, timeout)
	if err != nil {
		return err
	}
	preview, err := c.Watch(adm)
	if err != nil {
		return refuse(err)
	}
	body, err := json.Marshal(preview)
	if err != nil {
		return err
	}
	defer conn.SetDeadline(time.Time{})
	if timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}
	if err := link.NewMessage(MsgJoinPreview, body).Encode(conn); err != nil {
		return err
	}
	for asked := false; ; {
		msg, err := link.Decode(conn)
		if err != nil {
			return err
		}
		switch {
		case msg.Type == MsgScenarioRequest && !asked:
			asked = true
			if err := serveScenario(conn, c, string(msg.Payload), timeout); err != nil {
				return err
			}
		case msg.Type == MsgJoinReply:
			var reply sessionReply
			_ = json.Unmarshal(msg.Payload, &reply)
			return fmt.Errorf("the dial left before its feed: %s", cmp.Or(reply.Error, "it gave no reason"))
		case msg.Type == MsgJoinSeat:
			// Asked again: the session a dial prepared on may have ended, or filled.
			if _, err := c.Watch(adm); err != nil {
				return refuse(err)
			}
			if err := link.NewMessage(MsgStart, nil).Encode(conn); err != nil {
				return err
			}
			_ = conn.SetDeadline(time.Time{})
			return c.Feed(conn, adm)
		default:
			return fmt.Errorf("watch handshake: got message %#x, want a seat", msg.Type)
		}
	}
}

// Watched is o as a dial that only watches is shown it, in its preview and in every
// starting point of the feed: the game and who plays it, under what names. Nothing
// in it takes, keeps or comes back to a place, and nothing says where anyone listens.
func (o SessionOffer) Watched() SessionOffer {
	o.Assigned, o.Relay, o.Chain, o.TermKeys, o.KeepOut, o.Pass, o.Door = 0, 0, nil, nil, nil, nil, nil
	o.Roster = slices.Clone(o.Roster)
	for i := range o.Roster {
		o.Roster[i].Key, o.Roster[i].Account = "", ""
	}
	return o
}

// WatchHead is a feed's starting point as a session writes it: the session as a
// watching dial is shown it, the host's world at the tick the feed was cut at, and
// the epochs and owner syncs the host had committed past that tick, which went out
// before the cut. A viewer that begins from it needs nothing the feed carried before.
type WatchHead struct {
	Offer          SessionOffer
	World          []byte
	Epochs, States [][]byte
}

// Encode lays the head out as the offer's JSON and the world, then each list's
// count and bodies, every body behind its length.
func (h WatchHead) Encode() ([]byte, error) {
	offer, err := json.Marshal(h.Offer)
	if err != nil {
		return nil, err
	}
	lists := [][][]byte{h.Epochs, h.States}
	n := len(offer) + len(h.World) + 4*binary.MaxVarintLen64
	for _, list := range lists {
		for _, b := range list {
			n += len(b) + binary.MaxVarintLen64
		}
	}
	out := make([]byte, 0, n)
	put := func(b []byte) { out = append(binary.AppendUvarint(out, uint64(len(b))), b...) }
	put(offer)
	put(h.World)
	for _, list := range lists {
		out = binary.AppendUvarint(out, uint64(len(list)))
		for _, b := range list {
			put(b)
		}
	}
	return out, nil
}

// DecodeWatchHead reads what Encode wrote, with the offer cut to what a watching dial
// is shown, whoever wrote it. The world and the lists alias b.
func DecodeWatchHead(b []byte) (WatchHead, error) {
	short := errors.New("watch head is cut short")
	count := func() (uint64, bool) {
		n, k := binary.Uvarint(b)
		if k <= 0 || n > uint64(len(b)-k) {
			return 0, false
		}
		b = b[k:]
		return n, true
	}
	take := func() ([]byte, bool) {
		n, ok := count()
		if !ok {
			return nil, false
		}
		body := b[:n:n]
		b = b[n:]
		return body, true
	}
	var h WatchHead
	offer, ok := take()
	if !ok {
		return h, short
	}
	if err := json.Unmarshal(offer, &h.Offer); err != nil {
		return h, fmt.Errorf("watch head offer: %w", err)
	}
	if h.World, ok = take(); !ok {
		return h, short
	}
	for _, list := range []*[][]byte{&h.Epochs, &h.States} {
		n, ok := count() // each body is a byte at least, so a count past what is left is refused
		if !ok {
			return h, short
		}
		for range n {
			body, ok := take()
			if !ok {
				return h, short
			}
			*list = append(*list, body)
		}
	}
	err := h.Offer.ValidatePreview()
	h.Offer = h.Offer.Watched() // whatever its feed says, a viewer holds nothing of a place
	return h, err
}

// FeedConfig sizes a session's feed for audience viewers, and a viewer's end of one.
func FeedConfig(audience int) link.FeedConfig {
	cfg := link.DefaultFeedConfig()
	cfg.Marks, cfg.Subscribers = parameter.FeedRecordTicks, audience
	return cfg
}

// NewFeedPort is a viewer's end of the feed conn carries, as its run's session port.
func NewFeedPort(conn net.Conn) *link.FeedPort {
	return link.NewFeedPort(conn, kinds, FeedConfig(0))
}

// Admission is what a dial proved at the door: the nonce it was asked and the keys
// it answered with, the first its own for a new place. A relay carries both on to
// its authority, which checks the proofs itself.
type Admission struct {
	Nonce  string
	Proofs []Proof
	// Shown is the pass the dial showed, for a relay to carry on, and Pass its body
	// once the door's Pass hook verified it; a dial that showed none has nil for both.
	// Ticket is the router's ticket the dial carried, verified; nil without one.
	Shown  *Signed
	Pass   *PassBody
	Ticket *TicketBody
}

// Keys is the roster form of the keys proved, in the dial's order.
func (a Admission) Keys() []string {
	out := make([]string, len(a.Proofs))
	for i, p := range a.Proofs {
		out[i] = EncodeKey(p.Key)
	}
	return out
}

// askAdmission asks a dial to prove its keys over a nonce and reads the proofs it
// answers with, before a place is assigned, so a full session still takes back a
// player whose Wraith keeps one. A dial that proves none, or one it cannot, is
// refused with its reason.
func askAdmission(conn net.Conn, c Coordinator, timeout time.Duration) (Admission, error) {
	if timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(timeout))
		defer conn.SetDeadline(time.Time{})
	}
	nonce := NewNonce()
	if c.Nonce != nil {
		var err error
		if nonce, err = c.Nonce(); err != nil {
			RefuseJoin(conn, err, timeout)
			return Admission{}, err
		}
	}
	// What the door takes: a bare key where it is open, the exchange where it holds
	// the passphrase, the site's ticket on a fleet pod, a pass alone where it holds
	// neither for a gated session.
	var door *PassphraseDoor
	if c.Passphrase != nil {
		door = c.Passphrase()
	}
	open := door == nil && c.Ticket == nil && (c.Gated == nil || !c.Gated())
	question := AuthResponse{Nonce: nonce}
	if door != nil {
		question.Methods = append(question.Methods, MethodScram)
	}
	if c.Ticket != nil {
		question.Methods = append(question.Methods, MethodTicket)
	}
	if open {
		question.Methods = []string{MethodKey}
	}
	if c.Authority != nil {
		question.Authority = c.Authority()
	}
	if c.Pass != nil && question.Authority != "" {
		question.Methods = append(question.Methods, MethodPass)
	}
	if err := writeAuth(conn, question); err != nil {
		return Admission{}, err
	}
	msg, err := link.DecodeWithin(conn, MaxAuthFrame)
	if err != nil {
		return Admission{}, err
	}
	if msg.Type != MsgAuthRequest {
		return Admission{}, fmt.Errorf("join handshake: got message %#x, want an admission answer", msg.Type)
	}
	var ans AuthRequest
	if err := json.Unmarshal(msg.Payload, &ans); err != nil {
		return Admission{}, fmt.Errorf("join handshake admission: %w", err)
	}
	keys, err := VerifyProofs(ans.Keys, nonce, nil)
	if err != nil {
		RefuseJoin(conn, err, timeout)
		return Admission{}, err
	}
	adm := Admission{Nonce: nonce, Proofs: ans.Keys}
	if ans.Pass != nil {
		if c.Pass == nil || question.Authority == "" {
			err = &Refusal{Code: RefusalCredential, Reason: "this door takes no pass"}
		} else {
			adm.Pass, err = c.Pass(ans.Pass, keys)
		}
		if err != nil {
			RefuseJoin(conn, err, timeout)
			return Admission{}, err
		}
		adm.Shown = ans.Pass
	}
	if !open && adm.Pass == nil {
		var err error
		switch {
		case c.Ticket != nil && ans.Ticket != nil:
			adm.Ticket, err = c.Ticket(ans.Ticket, keys)
		case door != nil && len(ans.Scram) != 0:
			var binding []byte
			if tlsOf(conn) != nil {
				binding = c.Binding
			}
			err = door.serve(conn, ans.Scram, binding)
		case c.Ticket != nil:
			err = &Refusal{Code: RefusalSignIn, Reason: "this door takes the site's ticket, and the dial came with none"}
		case door != nil:
			err = &Refusal{Code: RefusalSignIn, Reason: "this door takes a passphrase, and the dial showed none"}
		default:
			err = &Refusal{Code: RefusalSignIn, Reason: "this door takes a pass alone; the host's door takes the passphrase"}
		}
		if err != nil {
			RefuseJoin(conn, err, timeout)
			return Admission{}, err
		}
	}
	return adm, nil
}

// answerAdmission proves this dial's keys over the door's nonce, shows the pass it
// holds for the session the door named, and reads the frame the door answers them
// with. A door that takes no bare key asks for a credential this build has none of yet.
func answerAdmission(conn net.Conn, base *Config, question *link.Message) (*link.Message, error) {
	var ask AuthResponse
	if err := json.Unmarshal(question.Payload, &ask); err != nil {
		return nil, fmt.Errorf("join handshake question: %w", err)
	}
	if err := ask.Refused(); err != nil {
		return nil, err
	}
	if ask.Nonce == "" {
		return nil, &Refusal{Code: RefusalSignIn, Reason: "this door asks for a credential this build cannot show"}
	}
	proofs := make([]Proof, 0, len(base.Keys))
	for _, k := range base.Keys {
		if k.Valid() {
			proofs = append(proofs, k.Prove(ask.Nonce, nil))
		}
	}
	answer := AuthRequest{Keys: proofs}
	if slices.Contains(ask.Methods, MethodPass) {
		answer.Pass = ChoosePass(base.Passes, ask.Authority, base.Keys, time.Now()) // [wall] the pass's window
	}
	// A door that takes the site's ticket is answered with the keys alone: the
	// router between adds the ticket, and a dial that reached it by another way is
	// refused there. The router's own dial carries the one it signed.
	if slices.Contains(ask.Methods, MethodTicket) {
		answer.Ticket = base.Ticket
	}
	// A sign-in crosses TLS alone: the router reads it on its front door and never
	// passes it on, and a plain stream would show it to whoever listens.
	if base.SignIn != "" && tlsOf(conn) != nil {
		answer.SignIn = base.SignIn
	}
	var client *auth.ScramClient
	if !slices.Contains(ask.Methods, MethodKey) && !slices.Contains(ask.Methods, MethodTicket) && answer.Pass == nil {
		switch {
		case !slices.Contains(ask.Methods, MethodScram):
			return nil, &Refusal{Code: RefusalSignIn, Reason: "this door takes a pass alone; the host's door takes the passphrase"}
		case base.Passphrase == "":
			return nil, &Refusal{Code: RefusalSignIn, Reason: "this door takes a passphrase, and this dial has none"}
		}
		client = auth.NewScramClient(doorUser, base.Passphrase, auth.WithMinArgonCost(parameter.DoorArgonTime, parameter.DoorArgonMemory))
		first, err := client.StartAuthentication()
		if err != nil {
			return nil, err
		}
		answer.Scram = rawJSON(first)
	}
	body, err := json.Marshal(answer)
	if err != nil {
		return nil, err
	}
	if err := link.NewMessage(MsgAuthRequest, body).Encode(conn); err != nil {
		return nil, err
	}
	msg, err := link.Decode(conn)
	if err != nil || client == nil {
		return msg, err
	}
	return finishPassphrase(conn, client, msg, bindingOf(conn))
}

// RefusalCode is the kind of a join refusal, which a dialer acts on rather than on
// its wording. Zero is a refusal that names no kind this build knows.
type RefusalCode uint8

const (
	RefusalFull       RefusalCode = iota + 1 // no room for another participant
	RefusalStarting                          // the lobby is closing on its roster
	RefusalHandoff                           // the session is electing an authority
	RefusalEnding                            // the session is draining or has expired
	RefusalIdentity                          // another build, or another session
	RefusalBudget                            // this address has joined too often
	RefusalBehind                            // the join could not catch up with the session
	RefusalUnknown                           // no such session at this address
	RefusalYielding                          // full, and a Wraith is giving up its place
	RefusalReturning                         // the claimed place is still linked or already coming back
	RefusalUnpressed                         // the dialer's player did not press within the ready period
	RefusalSignIn                            // the door admits a pass, a ticket or a passphrase, and the dial showed none
	RefusalCredential                        // what the dial showed was refused: a bad proof, pass, ticket or passphrase
	RefusalDropped                           // the authority dropped this participant, and its key or account stays out
	RefusalAudience                          // the session feeds every viewer it takes
	RefusalUnstarted                         // the game has not started, and its lobby holds players alone
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
	RefusalFull:       {RetryLater, "retry when a player leaves"},
	RefusalStarting:   {RetrySoon, "retry in a moment"},
	RefusalHandoff:    {RetrySoon, "retry in a moment"},
	RefusalEnding:     {RetryNever, ""},
	RefusalIdentity:   {RetryNever, ""},
	RefusalBudget:     {RetryLater, "retry in a minute"},
	RefusalBehind:     {RetrySoon, "retry in a moment"},
	RefusalUnknown:    {RetryNever, ""},
	RefusalYielding:   {RetrySoon, "retry in a moment"},
	RefusalReturning:  {RetrySoon, "retry in a moment"},
	RefusalUnpressed:  {RetrySoon, "press again to join"},
	RefusalSignIn:     {RetryLater, "sign in, or ask the host for the passphrase, then retry"},
	RefusalCredential: {RetryNever, ""},
	RefusalDropped:    {RetryNever, ""},
	RefusalAudience:   {RetryLater, "retry when a spectator leaves"},
	RefusalUnstarted:  {RetryLater, "retry once it has"},
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

// RefusalReply is a MsgJoinReply's body turning a join away for cause, which a
// port sends a joiner already on it, inside its gate.
func RefusalReply(cause error) []byte {
	var reply sessionReply
	reply.Refuse(cause)
	body, _ := json.Marshal(reply) // plain data cannot fail to encode
	return body
}

// RefuseJoin writes one rejection, which the dialer reads as its reason; a
// compressed stream is flushed, since the dialer reads nothing it still holds. A
// failure to write it is not worth reporting: the connection is being closed
// either way, and the joiner falls back to reading the stream's end.
func RefuseJoin(conn net.Conn, cause error, timeout time.Duration) {
	if timeout > 0 {
		_ = conn.SetWriteDeadline(time.Now().Add(timeout))
		defer conn.SetWriteDeadline(time.Time{})
	}
	if err := link.NewMessage(MsgJoinReply, RefusalReply(cause)).Encode(conn); err == nil {
		if stream, ok := conn.(*link.Stream); ok {
			_ = stream.Flush()
		}
	}
}

// PendingJoin owns a dialled stream until the startup gate transfers it to a port.
type PendingJoin struct {
	conn        *link.Stream
	base        Config
	offer       SessionOffer
	replied     bool
	started     bool
	transferred bool

	// Keep session traffic received before world installation for the first poll.
	deferred []*link.Message

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
	err := link.NewMessage(MsgScenarioRequest, []byte(digest)).Encode(p.conn)
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

// Key is the participant key a new place this dial is given takes: the first it
// proved, made for the dial when its configuration named none.
func (p *PendingJoin) Key() Key {
	if len(p.base.Keys) == 0 {
		return Key{}
	}
	return p.base.Keys[0]
}

// Deferred returns the session traffic read off the stream during the gate, oldest
// first. Every frame came from Peer, the only one this stream has. Valid after
// Ready; the slice is not reused.
func (p *PendingJoin) Deferred() []*link.Message { return p.deferred }

// Peer names the participant on the other end of this stream: the coordinator, or
// the relay the offer came through.
func (p *PendingJoin) Peer() link.PeerID { return cmp.Or(p.offer.Relay, p.offer.Host) }

// hold buffers one session frame that arrived out of the handshake's turn, and
// reports whether it was one. Anything the handshake itself expects is left to the
// caller, and an unrecognised type is a protocol error rather than something to
// stash.
func (p *PendingJoin) hold(msg *link.Message) bool {
	p.gateBytes += uint64(link.HeaderSize + len(msg.Payload))
	switch msg.Type {
	case link.MsgLinkProbe:
		// Answer probes during admission so snapshot transfer is not measured as loss.
		// An empty pre-world report allows the host to rebase its counters.
		p.answerProbe(msg)
		return true
	case link.MsgLinkEcho:
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
	case MsgEvent, MsgStateSync, link.MsgBundle, MsgDisconnect:
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
func (p *PendingJoin) next() (*link.Message, error) {
	if p.base.ReadTimeout > 0 {
		_ = p.conn.SetReadDeadline(time.Now().Add(p.base.ReadTimeout))
	}
	var header [link.HeaderSize]byte
	msg, err := p.conn.ReadFrame(link.MaxPayloadSize, header[:])
	if ne := net.Error(nil); errors.As(err, &ne) && ne.Timeout() {
		return nil, link.PlainError{Text: "the host stopped answering", Cause: err}
	}
	return msg, err
}

// answerProbe replies to one link probe read off the handshake stream. A failed
// write is not the join's problem: the probe is a measurement and losing one
// costs an estimate, where raising it here would fail a join over telemetry.
func (p *PendingJoin) answerProbe(msg *link.Message) {
	echo := link.AnswerProbe(msg.Payload, p.gateBytes)
	if echo == nil {
		return
	}
	if p.base.WriteTimeout > 0 {
		_ = p.conn.SetWriteDeadline(time.Now().Add(p.base.WriteTimeout))
		defer p.conn.SetWriteDeadline(time.Time{})
	}
	_ = p.send(link.NewMessage(link.MsgLinkEcho, echo))
}

// send writes one frame, flushing it through the stream once the join has opened
// one.
func (p *PendingJoin) send(msg *link.Message) error {
	if err := msg.Encode(p.conn); err != nil {
		return err
	}
	return p.conn.Flush()
}

// maxDeferredJoinFrames bounds what one join may buffer off its stream.
const maxDeferredJoinFrames = 512

// readSnapshot reads a whole capture from a raw handshake stream, holding the
// session traffic that arrives alongside it.
//
// The interleaving is by design rather than by accident. A mid-run host admits a
// participant before it reads the world for it, so the epochs it produces during
// the transfer reach that participant instead of falling into the gap; they arrive
// on this stream, between chunks, and hold keeps them for the barrier to receive
// once the world they apply to exists. A probe is answered the same way. Anything
// else is a protocol error, because the next message on this stream decides what
// the joiner does with the world it just received.
func readSnapshot(p *PendingJoin) (uint64, []byte, error) {
	return readChunked(p, MsgStateSnapshot)
}

// readChunked reassembles one chunked transfer off the handshake stream, holding
// the session traffic that arrives beside it. want names the kind: a capture and a
// scenario are the two messages whose size is a function of their content, and
// they share the framing because they are the same problem.
func readChunked(p *PendingJoin, want link.MessageType) (uint64, []byte, error) {
	defer p.conn.SetReadDeadline(time.Time{})
	var asm link.SnapshotAssembly
	for {
		msg, err := p.next()
		if err != nil {
			return 0, nil, err
		}
		if p.hold(msg) {
			continue
		}
		if msg.Type == MsgJoinReply {
			// The coordinator refused mid-transfer. Its reason is worth more than
			// the surprise at the message kind, and it is the only thing the
			// dialer can act on.
			return 0, nil, refusalFrom(msg)
		}
		if msg.Type != want {
			return 0, nil, fmt.Errorf("join transfer: got message %#x, want %#x", msg.Type, want)
		}
		done, err := asm.Add(msg.Payload)
		if err != nil {
			return 0, nil, err
		}
		if done {
			tick, body := asm.Result()
			return tick, body, nil
		}
	}
}

// PreviewSession dials addr and reads the session it would join, which holds
// nothing for this dial yet: its identity, slot and claim are assigned at Seat. The
// build half of the identity is checked here, before a world is built on the
// preview; the session half once one is.
func PreviewSession(addr string, cfg *Config) (*PendingJoin, SessionOffer, error) {
	return previewed(openJoinStream(addr, cfg, MsgSessionRoute))
}

// WatchSession dials addr to watch and reads the session it would be shown, as
// PreviewSession reads the one a join would be offered. Watch then asks for the feed.
func WatchSession(addr string, cfg *Config) (*PendingJoin, SessionOffer, error) {
	return previewed(openJoinStream(addr, cfg, MsgSessionWatch))
}

// WatchOver is WatchSession on a stream the caller opened, which the dial then owns:
// a front door's own dial to the session it placed the stream on.
func WatchOver(conn net.Conn, cfg *Config) (*PendingJoin, SessionOffer, error) {
	return previewed(openJoinOn(conn, dialConfig("", cfg), MsgSessionWatch))
}

// previewed checks the preview a dial read, closing the dial on one it cannot
// prepare on.
func previewed(p *PendingJoin, err error) (*PendingJoin, SessionOffer, error) {
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
	p, err := openJoinStream(addr, cfg, MsgSessionRoute)
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

// dialConfig is the configuration a dial to addr runs on: cfg's, or the defaults.
func dialConfig(addr string, cfg *Config) *Config {
	base := DefaultConfig()
	if cfg != nil {
		copy := *cfg
		base = &copy
	}
	base.Role, base.Address = link.RolePeer, addr
	// A dial proves a key for the new place it may be given, made here when its
	// configuration named none; a keeper names its kept ones after it.
	if len(base.Keys) == 0 {
		base.Keys = []Key{NewKey()}
	}
	return base
}

// openJoinStream dials addr and opens the handshake kind names on it, a join's
// route or a watch's.
func openJoinStream(addr string, cfg *Config, kind link.MessageType) (*PendingJoin, error) {
	base := dialConfig(addr, cfg)
	conn, err := link.Dial(addr, &base.Config)
	if err != nil && base.Dial == nil && base.SessionName != "" && !link.RouteAnswered(err) {
		conn, err = dialListed(base, err)
	}
	if err != nil {
		return nil, err
	}
	return openJoinOn(conn, base, kind)
}

// openJoinOn names the session and the claims the dial keeps on conn, and reads the
// preview the coordinator answers with, unchecked: a coordinator that previews
// nothing is still one a dial can seat at once with.
func openJoinOn(conn net.Conn, base *Config, kind link.MessageType) (*PendingJoin, error) {
	if base.ConnectTimeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(base.ConnectTimeout))
	}
	// Before the preview is read, because the coordinator reads this before it writes
	// one and a front door has to place the connection before either happens; sent
	// unnamed too, so a port that also takes peer links tells this is a join.
	if err := link.NewMessage(kind, []byte(base.SessionName)).Encode(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	msg, err := link.Decode(conn)
	if err == nil && msg.Type == MsgAuthResponse {
		msg, err = answerAdmission(conn, base, msg)
	}
	_ = conn.SetDeadline(time.Time{})
	if err != nil {
		_ = conn.Close()
		if errors.As(err, new(*Refusal)) {
			return nil, err
		}
		return nil, link.PlainError{Text: "connected, but no vif session answered there", Cause: err}
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
	return &PendingJoin{conn: link.NewStream(conn), base: *base, offer: preview}, nil
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
	if err := link.NewMessage(MsgJoinPrepared, nil).Encode(p.conn); err != nil {
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
	if err := link.NewMessage(MsgJoinSeat, nil).Encode(p.conn); err != nil {
		return SessionOffer{}, link.PlainError{Text: "the host dropped the join before its seat", Cause: err}
	}
	msg, err := link.Decode(p.conn)
	switch {
	case err != nil:
		return SessionOffer{}, link.PlainError{Text: "the host dropped the join before its seat", Cause: err}
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

// Watch asks for the session's feed and returns the stream that carries it, which
// the caller then owns.
func (p *PendingJoin) Watch() (net.Conn, error) {
	if p.replied || p.transferred || p.offer.Assigned != 0 {
		return nil, errors.New("watch handshake already completed")
	}
	if p.base.ConnectTimeout > 0 {
		_ = p.conn.SetDeadline(time.Now().Add(p.base.ConnectTimeout))
		defer p.conn.SetDeadline(time.Time{})
	}
	dropped := func(err error) error {
		return link.PlainError{Text: "the host dropped the watch before its feed", Cause: err}
	}
	if err := link.NewMessage(MsgJoinSeat, nil).Encode(p.conn); err != nil {
		return nil, dropped(err)
	}
	msg, err := link.Decode(p.conn)
	switch {
	case err != nil:
		return nil, dropped(err)
	case msg.Type == MsgJoinReply:
		return nil, refusalFrom(msg)
	case msg.Type != MsgStart:
		return nil, fmt.Errorf("watch handshake: got message %#x, want the feed's start", msg.Type)
	}
	p.transferred = true
	return p.conn, nil
}

// dialListed dials base's session by another link its site lists, once nothing
// answered at its own address: a player may hold the link another kind of client
// joins by. With no site answering, the failure of the address it was given stands.
func dialListed(base *Config, failed error) (net.Conn, error) {
	links, err := link.ListedLinks(base.Address, base.SessionName, base.ConnectTimeout)
	if err != nil {
		return nil, err
	}
	for _, route := range links {
		e, err := link.ParseEndpoint(route)
		if err != nil || e.Addr == base.Address {
			continue
		}
		conn, err := link.Dial(e.Addr, &base.Config)
		if err == nil {
			base.Address = e.Addr
			return conn, nil
		}
		failed = fmt.Errorf("%s: %w", route, err)
	}
	return nil, failed
}

// TransportConfig returns a client config that adopts the negotiated identity and
// the join's stream. Before the seat there is no identity, and the stream stays this
// join's until a port takes it (Hand).
func (p *PendingJoin) TransportConfig() *link.Config {
	cfg := p.base.Config
	cfg.Role = link.RolePeer
	cfg.Dial = nil // the join's own stream; a later peer link dials its address
	if p.offer.Assigned != 0 {
		cfg.ParticipantID = p.offer.Assigned
		cfg.Preconnected, cfg.PreconnectedPeer = p.conn, p.Peer()
	}
	return &cfg
}

// Hand hands a port built before this join was seated its stream and the identity
// the seat assigned: a joiner builds its world, and that port with it, before its
// player's press.
func (p *PendingJoin) Hand(port *link.SocketPort) error {
	if p.offer.Assigned == 0 {
		return errors.New("a join hands its stream over once seated")
	}
	return port.Preconnect(p.conn, p.offer.Assigned, p.Peer())
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
		reply.Record = report.Record
	}
	body, err := json.Marshal(reply)
	if err == nil {
		if p.base.WriteTimeout > 0 {
			_ = p.conn.SetWriteDeadline(time.Now().Add(p.base.WriteTimeout))
			defer p.conn.SetWriteDeadline(time.Time{})
		}
		err = link.NewMessage(MsgJoinReply, body).Encode(p.conn)
	}
	if joinErr != nil {
		_ = p.conn.Close()
		return joinErr
	}
	// Everything after the reply travels on the link's compressed stream, the
	// coordinator's side of which opens as it reads this reply.
	link.OpenStream(p.conn, &p.base.Config)
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
	var msg *link.Message
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
	if err := p.send(link.NewMessage(MsgReady, nil)); err != nil {
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

// AdmissionLimiter bounds expensive joins with a fixed window per key: an address's
// host, which dialers share behind NAT, or an account where a router names one
// (AdmitKey), so a signed-in player's dials count against the account alone.
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
	return l.AdmitKey(admissionKey(addr))
}

// AdmitKey records one admission under key, an account's or an address's; an empty
// key is nothing to attribute a budget to and proceeds, as does a nil limiter.
func (l *AdmissionLimiter) AdmitKey(key string) error {
	if l == nil || key == "" {
		return nil
	}
	return l.AdmitKeyBurst(key, l.burst)
}

// AdmitKeyBurst admits under key with a burst of its own in the limiter's window:
// an account's budget beside the addresses'.
func (l *AdmissionLimiter) AdmitKeyBurst(key string, burst int) error {
	if l == nil || key == "" {
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

	if entry.count >= burst {
		return &Refusal{Code: RefusalBudget, Reason: keyHolder(key) + " has joined too often"}
	}
	entry.count++
	return nil
}

// Refund takes back one admission of addr, for a dial that started nothing.
func (l *AdmissionLimiter) Refund(addr net.Addr) {
	l.RefundKey(admissionKey(addr))
}

func (l *AdmissionLimiter) RefundKey(key string) {
	if l == nil || key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry, ok := l.seen[key]; ok && entry.count > 0 {
		entry.count--
	}
}

// Wait is how long until addr may be admitted again, zero when it may now.
func (l *AdmissionLimiter) Wait(addr net.Addr) time.Duration {
	return l.WaitKey(admissionKey(addr))
}

func (l *AdmissionLimiter) WaitKey(key string) time.Duration {
	if l == nil || key == "" {
		return 0
	}
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

// keyHolder names a key's holder to the dialer refused: the account where the key
// is one, else the address.
func keyHolder(key string) string {
	if strings.HasPrefix(key, "acct:") {
		return "this account"
	}
	return "this address"
}

// AdmissionKey is what a budget counts an address under, for a limiter keyed by
// string beside accounts: the host half of the address, an IPv6 host's /64, empty
// for no address or an in-process one.
func AdmissionKey(addr net.Addr) string { return admissionKey(addr) }

// admissionKey is the host half of a network address, an IPv6 host's /64. The port
// and an IPv6 address's low half are what a dialer changes for free, so counting
// either would count nothing.
func admissionKey(addr net.Addr) string {
	if addr == nil || link.InProcess(addr) {
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
