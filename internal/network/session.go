package network

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// SessionParticipant is one coordinator-assigned participant and roster slot.
type SessionParticipant struct {
	ID   PeerID `json:"id"`
	Slot uint8  `json:"slot"`
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

	Participants      []SessionParticipant `json:"participants"`
	BarrierDelayTicks uint64               `json:"barrier_delay_ticks"`

	// SnapshotTick names the tick of the capture that follows the start gate, and
	// SnapshotBytes its encoded length. A joiner reads them before the transfer so a
	// stream that stops halfway is a failed join rather than a world installed from
	// a prefix. Zero means the gate carries no capture, which is what an offer
	// written by a build that predates this looks like.
	SnapshotTick  uint64 `json:"snapshot_tick,omitempty"`
	SnapshotBytes int    `json:"snapshot_bytes,omitempty"`
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
}

// JoinerReport is what a joining participant tells the coordinator about itself.
//
// A dedicated host has no terminal to derive a map from and would otherwise serve
// the default one, so the first guest's geometry is the only real number the
// session ever sees. It is advisory: a participant that reports nothing is one the
// coordinator sizes without, and a coordinator that was given a size ignores it.
type JoinerReport struct {
	Width  int
	Height int
}

// Sized reports whether this report names a usable geometry.
func (r JoinerReport) Sized() bool { return r.Width > 0 && r.Height > 0 }

// Validate rejects transport-level ambiguity before App.Join checks identity.
func (o SessionOffer) Validate() error {
	if o.Host == 0 || o.Assigned == 0 || o.Host == o.Assigned {
		return errors.New("join offer carries invalid participant assignment")
	}
	if o.BarrierDelayTicks == 0 {
		return errors.New("join offer carries no barrier delay")
	}
	if o.Term < FirstTerm {
		return errors.New("join offer carries no authority term")
	}
	ids := make(map[PeerID]bool, len(o.Participants))
	slots := make(map[uint8]bool, len(o.Participants))
	cursorless := PeerID(0)
	for _, p := range o.Participants {
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
	if cursorless != 0 && cursorless != o.Host {
		return errors.New("join offer makes a participant other than the host cursorless")
	}
	if !ids[o.Host] || !ids[o.Assigned] {
		return errors.New("join offer roster omits host or assigned participant")
	}
	return nil
}

// Participant returns the coordinator assignment for id.
func (o SessionOffer) Participant(id PeerID) (SessionParticipant, bool) {
	i := slices.IndexFunc(o.Participants, func(p SessionParticipant) bool { return p.ID == id })
	if i < 0 {
		return SessionParticipant{}, false
	}
	return o.Participants[i], true
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
	Report func(PeerID, JoinerReport)

	// Admit is the one decision made about a dialer before it costs the session
	// anything. Assign allocates an identity and a roster slot, and on a host that
	// is already running the admission that follows reads, encodes and sends a
	// whole world — so a peer that joins and leaves in a loop spends one connect
	// per capture. Refusing here is what bounds that; nil admits everything, which
	// is what a harness and a two-terminal lobby want.
	Admit func(net.Addr) error
}

// HostAcceptor returns a pre-world handshake for Transport's accept loop. Each
// accepted connection gets its own identity, so the roster grows with the lobby
// rather than being fixed at two.
func HostAcceptor(c Coordinator, timeout time.Duration) func(net.Conn) (PeerID, error) {
	return func(conn net.Conn) (id PeerID, err error) {
		if c.Admit != nil {
			if err = c.Admit(conn.RemoteAddr()); err != nil {
				// Answered for the same reason a refused Assign is: a dialer that
				// can read why it was turned away can back off, where one that only
				// sees the stream end retries immediately and makes the condition
				// the refusal exists to stop.
				refuseJoin(conn, err, timeout)
				return 0, err
			}
		}
		o, err := c.Assign()
		if err != nil {
			// Answered rather than dropped. A refusal the dialer can read is the
			// difference between "retry against the new authority" and a stream
			// that ended for no stated reason, and a succession is exactly the
			// case where the two need telling apart.
			refuseJoin(conn, err, timeout)
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
		if c.Report != nil {
			c.Report(o.Assigned, JoinerReport{Width: reply.Width, Height: reply.Height})
		}
		return o.Assigned, nil
	}
}

// refuseJoin writes one pre-offer rejection. A failure to write it is not worth
// reporting: the connection is being closed either way, and the joiner falls back
// to reading the stream's end.
func refuseJoin(conn net.Conn, cause error, timeout time.Duration) {
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
	conn        net.Conn
	base        Config
	offer       SessionOffer
	replied     bool
	started     bool
	transferred bool

	// deferred holds the session traffic that arrived on this stream before the
	// port owned it. A mid-run host admits a participant *before* it reads the
	// world that participant will install, precisely so the epochs produced in
	// between reach it; they arrive interleaved with the gate and the capture, and
	// dropping them here would lose exactly the crossings the ordering exists to
	// preserve.
	deferred []*Message

	// gateBytes is what this stream has decoded during the handshake. It answers a
	// probe that arrives mid-gate, so a host measuring this link sees a running
	// counter rather than a silence it would have to score as loss.
	gateBytes uint64
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
		// Answered rather than swallowed. A host admits a participant before it
		// reads the world for it (D-22), so this stream is a peer — and therefore
		// probed — while the gate is still running. Ignoring the probe would score
		// the whole transfer as loss on the link it is measuring, which is exactly
		// backwards: the transfer is the busiest that link will ever be.
		//
		// The report is empty because there is nothing true to put in it yet: this
		// instance holds no world, so it has no tick, no lag and no cursor. The
		// byte counter is this gate's own, and the meter on the other end re-bases
		// when the port takes the stream over and starts counting again from zero.
		p.answerProbe(msg)
		return true
	case MsgLinkEcho:
		// This end sends no probes during the gate, so an echo here is a stray from
		// a previous connection. Nothing to fold it into.
		return true
	case MsgStateCorrection, MsgStateManifest, MsgStateRequest, MsgStateShard, MsgStateUnserved:
		// Swallowed rather than held. The host broadcasts its cadence to every peer
		// it has, and this stream became one the moment the participant was admitted
		// — before the world was read for it (D-22) — so correction chunks arrive
		// interleaved with the gate. There is nothing to keep: this participant is
		// about to install a whole world, and every correction before that describes
		// one it does not have yet.
		//
		// The selective exchange is swallowed for the same reason and one more: an
		// index, a request or a repair is a question about a world this stream's
		// owner does not hold, and it cannot answer one until it does. Leaving them
		// out of this list is what made a second mid-run join fail outright — the
		// gate read a manifest where it wanted the start record and refused the
		// join — as soon as a session had a participant the host was already
		// publishing an index to.
		return true
	case MsgAuthorityReport, MsgAuthorityVote, MsgAuthorityHandoff:
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
		// Bounded, because this buffer is filled by the peer on the other end of the
		// stream and drained only when the world arrives. The ceiling is far above
		// the epochs a transfer can span — one per tick, and a transfer that took
		// this many ticks has already failed the join's lag check — so reaching it
		// means a sender that is not sending a capture. Dropping the oldest keeps
		// the newest epochs, which are the ones the catch-up reads its target from.
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
	echo := encodeEcho(msg.Payload, p.gateBytes, LinkReport{})
	if echo == nil {
		return
	}
	if p.base.WriteTimeout > 0 {
		_ = p.conn.SetWriteDeadline(time.Now().Add(p.base.WriteTimeout))
		defer p.conn.SetWriteDeadline(time.Time{})
	}
	_ = NewMessage(MsgLinkEcho, echo).Encode(p.conn)
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
		var reply sessionReply
		if err := json.Unmarshal(msg.Payload, &reply); err != nil || reply.Error == "" {
			return nil, SessionOffer{}, errors.New("join refused with no reason given")
		}
		return nil, SessionOffer{}, errors.New(reply.Error)
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
	return &PendingJoin{conn: conn, base: *base, offer: offer}, offer, nil
}

// TransportConfig returns a client config that adopts the negotiated identity.
func (p *PendingJoin) TransportConfig() *Config {
	cfg := p.base
	cfg.Role = RolePeer
	cfg.ParticipantID = p.offer.Assigned
	cfg.BarrierDelayTicks = p.offer.BarrierDelayTicks
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
	return err
}

// WaitStart waits for the host to close the lobby and returns the roster it closed
// on. A joiner's own offer describes only the participants present when it arrived,
// so the roster every instance builds from — and therefore shared creation order,
// which D-11 requires to be identical — is this one, not the offer.
//
// The start gate carries no deadline: it is the host waiting for the rest of the
// lobby, which is a human-paced wait with no bound worth guessing at. A lost host
// surfaces as a stream error instead.
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

// ReceiveSnapshot reads the capture the start gate announced.
//
// It follows MsgStart on the same stream rather than preceding it, because the
// roster the gate closes on is what decides which cursors the capture must already
// contain: the host configures its own roster, captures the world that produced,
// and sends the two in that order.
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
	if err := NewMessage(MsgReady, nil).Encode(p.conn); err != nil {
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
