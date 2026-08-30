package network

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/event"
)

// SessionParticipant is one coordinator-assigned participant and roster slot.
type SessionParticipant struct {
	ID   PeerID `json:"id"`
	Slot uint8  `json:"slot"`
}

// SessionOffer carries the replay identity and the coordinator-owned roster.
type SessionOffer struct {
	Anchor            event.JoinAnchor     `json:"anchor"`
	Host              PeerID               `json:"host"`
	Assigned          PeerID               `json:"assigned"`
	Participants      []SessionParticipant `json:"participants"`
	BarrierDelayTicks uint64               `json:"barrier_delay_ticks"`
}

type sessionReply struct {
	Error string `json:"error,omitempty"`
}

// Validate rejects transport-level ambiguity before App.Join checks identity.
func (o SessionOffer) Validate() error {
	if o.Host == 0 || o.Assigned == 0 || o.Host == o.Assigned {
		return errors.New("join offer carries invalid participant assignment")
	}
	if o.BarrierDelayTicks == 0 {
		return errors.New("join offer carries no barrier delay")
	}
	ids := make(map[PeerID]bool, len(o.Participants))
	slots := make(map[uint8]bool, len(o.Participants))
	for _, p := range o.Participants {
		if p.ID == 0 || ids[p.ID] || slots[p.Slot] {
			return errors.New("join offer carries duplicate participant assignment")
		}
		ids[p.ID], slots[p.Slot] = true, true
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
	// Log returns the records a participant arriving after tick zero must replay to
	// reach the position the offer's anchor names. Nil, or a session still at tick
	// zero, sends nothing.
	Log func() ([][]byte, error)
}

// HostAcceptor returns a pre-world handshake for Transport's accept loop. Each
// accepted connection gets its own identity, so the roster grows with the lobby
// rather than being fixed at two.
func HostAcceptor(c Coordinator, timeout time.Duration) func(net.Conn) (PeerID, error) {
	return func(conn net.Conn) (id PeerID, err error) {
		o, err := c.Assign()
		if err != nil {
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
		// Past the offer, nothing left is bounded by the link: the log is as long as
		// the session and the reply waits on however long the joiner needs to replay
		// it. Each write keeps its own deadline so a stalled peer still fails.
		_ = conn.SetDeadline(time.Time{})
		if err = sendSessionLog(conn, o, c.Log, timeout); err != nil {
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
		return o.Assigned, nil
	}
}

// sendSessionLog transfers the host's replayable log to a mid-run joiner. A session
// still at tick zero has nothing to catch up on and sends nothing, which is what
// keeps the ordinary startup handshake unchanged.
func sendSessionLog(conn net.Conn, o SessionOffer, log func() ([][]byte, error), perWrite time.Duration) error {
	if o.Anchor.Anchor.Tick == 0 && o.Anchor.Anchor.Run == 0 {
		return nil
	}
	if log == nil {
		return errors.New("join: this session retains no replayable log")
	}
	chunks, err := log()
	if err != nil {
		return err
	}
	for _, body := range chunks {
		if perWrite > 0 {
			_ = conn.SetWriteDeadline(time.Now().Add(perWrite))
		}
		if err := NewMessage(MsgSessionLog, body).Encode(conn); err != nil {
			return err
		}
	}
	if perWrite > 0 {
		_ = conn.SetWriteDeadline(time.Time{})
	}
	return nil
}

// PendingJoin owns a dialled stream until the startup gate transfers it to a port.
type PendingJoin struct {
	conn        net.Conn
	base        Config
	offer       SessionOffer
	replied     bool
	started     bool
	transferred bool
}

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

// MidRun reports whether this session has already started, and so whether the
// joiner has to reproduce it before it can take part.
func (p *PendingJoin) MidRun() bool {
	an := p.offer.Anchor.Anchor
	return an.Tick != 0 || an.Run != 0
}

// ReceiveSessionLog reads the host's log off the stream. Called before the reply,
// because a joiner that cannot reproduce the session must decline it rather than
// join a world it does not share.
func (p *PendingJoin) ReceiveSessionLog() ([]event.JournalRecord, error) {
	if !p.MidRun() {
		return nil, nil
	}
	var out []event.JournalRecord
	for seq := uint32(0); ; seq++ {
		if p.base.ReadTimeout > 0 {
			_ = p.conn.SetReadDeadline(time.Now().Add(p.base.ReadTimeout))
		}
		msg, err := Decode(p.conn)
		_ = p.conn.SetReadDeadline(time.Time{})
		if err != nil {
			return nil, err
		}
		if msg.Type != MsgSessionLog {
			return nil, fmt.Errorf("join handshake: got message %#x, want session log", msg.Type)
		}
		chunk, records, err := event.DecodeSessionLogChunk(msg.Payload)
		if err != nil {
			return nil, err
		}
		if chunk.Seq != seq {
			return nil, fmt.Errorf("session log: chunk %d arrived as %d", seq, chunk.Seq)
		}
		out = append(out, records...)
		if chunk.Final {
			if want := chunk.Total; want != seq+1 {
				return nil, fmt.Errorf("session log: final chunk %d of a declared %d", seq+1, want)
			}
			return out, nil
		}
	}
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
func (p *PendingJoin) Complete(joinErr error) error {
	if p.replied {
		return errors.New("join handshake already completed")
	}
	p.replied = true
	reply := sessionReply{}
	if joinErr != nil {
		reply.Error = joinErr.Error()
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
	msg, err := Decode(p.conn)
	if err != nil {
		return SessionOffer{}, err
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
