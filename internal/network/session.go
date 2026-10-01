package network

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// RosterEntry is one coordinator-assigned participant and roster slot.
// RosterEntry is one identity the coordinator has admitted, and the cursor slot it
// drives. NoPlayerSlot means it drives none, which is what a dedicated coordinator
// holds: in the roster, and not one of the session's participants.
type RosterEntry struct {
	ID     PeerID `json:"id"`
	Slot   uint8  `json:"slot"`
	Holder PeerID `json:"holder,omitempty"`
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

	// SnapshotTick names the tick of the capture that follows the start gate, and
	// SnapshotBytes its encoded length. A joiner reads them before the transfer so a
	// stream that stops halfway is a failed join rather than a world installed from
	// a prefix. Zero means the gate carries no capture, which is what an offer
	// written by a build that predates this looks like.
	SnapshotTick  uint64 `json:"snapshot_tick,omitempty"`
	SnapshotBytes int    `json:"snapshot_bytes,omitempty"`

	// Identity is what the coordinator is. The joiner reads it before it builds a
	// world, so a peer that cannot be in this session finds out before paying for
	// the attempt — and the anchor comparison it already performs is left in place,
	// because that one covers the world it is about to construct.
	Identity PeerIdentity `json:"identity"`
}

// CarriesSnapshot reports whether the start gate is followed by a capture.
func (o SessionOffer) CarriesSnapshot() bool { return o.SnapshotBytes > 0 }

type sessionReply struct {
	Error string `json:"error,omitempty"`

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
}

// Sized reports whether this report names a usable geometry.
func (r JoinerReport) Sized() bool { return r.Width > 0 && r.Height > 0 }

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
		// A cursorless participant holds no slot, so it collides with nobody. Only
		// the coordinator may be one: every other participant is in the session to
		// drive a cursor, and one that is not would take a vote and a roster entry
		// while contributing nothing the roster describes.
		if p.Slot == parameter.NoPlayerSlot {
			if cursorless != 0 {
				return errors.New("join offer carries more than one cursorless participant")
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
		return errors.New("join offer makes a participant other than the host cursorless")
	}
	if !ids[o.Host] || !ids[o.Assigned] {
		return errors.New("join offer roster omits host or assigned participant")
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
// participant identity and returns the offer carrying it; Release returns that
// identity to the pool when the handshake does not complete, so a rejected or
// abandoned connection does not consume a roster slot.
type Coordinator struct {
	Assign  func() (SessionOffer, error)
	Release func(PeerID)

	// Report carries what a joiner said about itself once its acceptance arrives.
	// It runs after Assign and only for a handshake that completed, so what it
	// describes is a participant the session actually holds.
	Report func(PeerID, JoinerReport) error

	// Admit spends the dial budget before expensive world capture and construction.
	// A nil hook admits the dial; loopback seats use the same handshake.
	Admit func(net.Addr) error

	// Name is what this session answers to, so one address can serve several. A
	// dialer sends it before the handshake and one that named another session is
	// refused. Empty expects no such frame, which is an address with one session
	// on it — a two-terminal lobby, a harness, a host somebody started by hand.
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
// rather than being fixed at two.
func HostAcceptor(c Coordinator, timeout time.Duration) func(net.Conn) (PeerID, error) {
	return func(conn net.Conn) (id PeerID, err error) {
		if c.Name != "" {
			// Before Admit, because a link that named another session costs the
			// budget Admit holds for the players of this one.
			if err = readSessionName(conn, c.Name, timeout); err != nil {
				RefuseJoin(conn, err, timeout)
				return 0, err
			}
		}
		if c.Admit != nil {
			if err = c.Admit(conn.RemoteAddr()); err != nil {
				// Answered for the same reason a refused Assign is: a dialer that
				// can read why it was turned away can back off, where one that only
				// sees the stream end retries immediately and makes the condition
				// the refusal exists to stop.
				RefuseJoin(conn, err, timeout)
				return 0, err
			}
		}
		o, err := c.Assign()
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
		// A joiner whose roots do not hold this session's scenario asks for it here,
		// before it builds a world and therefore before it reports an identity this
		// coordinator would refuse it on. One request fits: what follows the answer
		// has to be the reply.
		if msg.Type == MsgScenarioRequest {
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
		if reply.Error != "" {
			err = errors.New(reply.Error)
			return 0, err
		}
		// Verify before Report so an incompatible joiner cannot affect session state.
		if err = o.Identity.Verify(reply.Identity); err != nil {
			RefuseJoin(conn, err, timeout)
			return 0, err
		}
		if c.Report != nil {
			if err = c.Report(o.Assigned, JoinerReport{
				Width: reply.Width, Height: reply.Height, Listen: reply.Listen,
				Remote: conn.RemoteAddr().String(), Holder: reply.Holder,
			}); err != nil {
				RefuseJoin(conn, err, timeout)
				return 0, err
			}
		}
		return o.Assigned, nil
	}
}

// refusalFrom reads the reason out of a rejection frame, so a refused dialer says
// what it was told rather than that it was told something.
func refusalFrom(msg *Message) error {
	var reply sessionReply
	if err := json.Unmarshal(msg.Payload, &reply); err != nil || reply.Error == "" {
		return errors.New("join refused with no reason given")
	}
	return errors.New(reply.Error)
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
	chunks, err := EncodeSnapshotChunks(0, body)
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
		return errors.New("this address is not serving the session that was dialled")
	}
	return nil
}

// ReadSessionRoute reads the frame a dialer sends before it reads and returns the
// name it carries. A named host checks it; a front door places the connection on it.
func ReadSessionRoute(r io.Reader) (string, error) {
	msg, err := decode(r, MaxSessionName)
	if err != nil {
		return "", err
	}
	if msg.Type != MsgSessionRoute {
		return "", fmt.Errorf("join handshake: got message %#x, want a session name", msg.Type)
	}
	return string(msg.Payload), nil
}

// RefuseJoin writes one pre-offer rejection, which the dialer reads as its reason.
// A failure to write it is not worth reporting: the connection is being closed
// either way, and the joiner falls back to reading the stream's end.
func RefuseJoin(conn net.Conn, cause error, timeout time.Duration) {
	body, err := json.Marshal(sessionReply{Error: cause.Error()})
	if err != nil {
		return
	}
	if timeout > 0 {
		_ = conn.SetWriteDeadline(time.Now().Add(timeout))
		defer conn.SetWriteDeadline(time.Time{})
	}
	_ = NewMessage(MsgJoinReply, body).Encode(conn)
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
}

// RequestScenario asks the coordinator for the scenario the offer named and returns
// the deflated body. One request per handshake: the coordinator reads what follows
// the answer as the reply, so a second would be a protocol error on both sides.
func (p *PendingJoin) RequestScenario(digest string) ([]byte, error) {
	switch {
	case p.replied:
		return nil, errors.New("join handshake already completed")
	case p.asked:
		return nil, errors.New("join handshake already asked for a scenario")
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
	_, body, err := readChunked(p, MsgScenarioBody, p.base.ReadTimeout)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// Deferred returns the session traffic read off the stream during the gate, oldest
// first. Every frame came from the coordinator, which is the only peer this stream
// has. Valid after Ready; the slice is not reused.
func (p *PendingJoin) Deferred() []*Message { return p.deferred }

// HostID names the coordinator on the other end of this stream.
func (p *PendingJoin) HostID() PeerID { return p.offer.Host }

// hold buffers one session frame that arrived out of the handshake's turn, and
// reports whether it was one. Anything the handshake itself expects is left to the
// caller, and an unrecognised type is a protocol error rather than something to
// stash.
func (p *PendingJoin) hold(msg *Message) bool {
	p.gateBytes += uint64(HeaderSize + len(msg.Payload))
	switch msg.Type {
	case MsgHeartbeat:
		return true
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
	case MsgEvent, MsgStateSync, MsgStateDigest, MsgDisconnect:
		// Bound peer-controlled admission buffering; retain the newest traffic.
		if len(p.deferred) >= maxDeferredJoinFrames {
			p.deferred = append(p.deferred[:0], p.deferred[1:]...)
		}
		p.deferred = append(p.deferred, msg)
		return true
	}
	return false
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

// DialSession connects and receives the host's anchor before an App is built.
func DialSession(addr string, cfg *Config) (*PendingJoin, SessionOffer, error) {
	base := DefaultConfig()
	if cfg != nil {
		copy := *cfg
		base = &copy
	}
	base.Role, base.Address = RolePeer, addr
	conn, err := dial(addr, base)
	if err != nil {
		return nil, SessionOffer{}, err
	}
	if base.ConnectTimeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(base.ConnectTimeout))
	}
	// Before the offer is read, because the coordinator reads this before it writes
	// one and a front door has to place the connection before either happens.
	if base.SessionName != "" {
		if err = NewMessage(MsgSessionRoute, []byte(base.SessionName)).Encode(conn); err != nil {
			_ = conn.Close()
			return nil, SessionOffer{}, err
		}
	}
	msg, err := Decode(conn)
	_ = conn.SetDeadline(time.Time{})
	if err != nil {
		_ = conn.Close()
		return nil, SessionOffer{}, err
	}
	if msg.Type == MsgJoinReply {
		// Refused before an identity was allocated. The coordinator says why, and
		// the one reason worth acting on rather than reporting is a succession:
		// IsHandoffRefusal is what a caller retries on.
		_ = conn.Close()
		return nil, SessionOffer{}, refusalFrom(msg)
	}
	if msg.Type != MsgJoinOffer {
		_ = conn.Close()
		return nil, SessionOffer{}, fmt.Errorf("join handshake: got message %#x, want offer", msg.Type)
	}
	var offer SessionOffer
	if err := json.Unmarshal(msg.Payload, &offer); err != nil {
		_ = conn.Close()
		return nil, SessionOffer{}, fmt.Errorf("join handshake offer: %w", err)
	}
	if err := offer.Validate(); err != nil {
		_ = conn.Close()
		return nil, SessionOffer{}, err
	}
	// Check build identity before constructing the offered world; session identity
	// is verified after construction.
	if base.Identity.Protocol != 0 {
		if err := base.Identity.VerifyBuild(offer.Identity); err != nil {
			_ = conn.Close()
			return nil, SessionOffer{}, err
		}
	}
	return &PendingJoin{conn: &flateStream{Conn: conn}, base: *base, offer: offer}, offer, nil
}

// TransportConfig returns a client config that adopts the negotiated identity.
func (p *PendingJoin) TransportConfig() *Config {
	cfg := p.base
	cfg.Role = RolePeer
	cfg.ParticipantID = p.offer.Assigned
	cfg.preconnected = p.conn
	cfg.preconnectedPeer = p.offer.Host
	return &cfg
}

// Complete returns a rejection unchanged or admits the stream for the start gate.
// The report travels with the acceptance and is ignored on a rejection, which
// carries no participant to describe.
func (p *PendingJoin) Complete(joinErr error, report JoinerReport) error {
	if p.replied {
		return errors.New("join handshake already completed")
	}
	p.replied = true
	reply := sessionReply{}
	if joinErr != nil {
		reply.Error = joinErr.Error()
	} else {
		reply.Width, reply.Height = report.Width, report.Height
		reply.Identity = report.Identity
		reply.Listen, reply.Holder = report.Listen, report.Holder
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
// entity creation order. Human lobby time has no deadline; transport closure ends it.
func (p *PendingJoin) WaitStart() (SessionOffer, error) {
	if !p.replied {
		return SessionOffer{}, errors.New("join handshake has no reply")
	}
	if p.started {
		return SessionOffer{}, errors.New("join start gate already received")
	}
	var msg *Message
	for {
		var err error
		if msg, err = Decode(p.conn); err != nil {
			return SessionOffer{}, err
		}
		if !p.hold(msg) {
			break
		}
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
	tick, body, err := readSnapshot(p, p.base.ReadTimeout)
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
			return fmt.Errorf("admission: %d dialling hosts already tracked", len(l.seen))
		}
		entry = &admissionCount{opened: now}
		l.seen[key] = entry
	}

	if entry.count >= l.burst {
		return fmt.Errorf("admission: %s has joined %d times within %s",
			key, entry.count, l.window)
	}
	entry.count++
	return nil
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

// admissionKey is the host half of a network address. The port is what a dialer
// changes for free, so counting it would count nothing.
func admissionKey(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	s := addr.String()
	if host, _, err := net.SplitHostPort(s); err == nil {
		return host
	}
	return s
}
