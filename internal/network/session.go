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

// HostAcceptor returns a pre-world handshake for Transport's accept loop.
func HostAcceptor(offer func() (SessionOffer, error), timeout time.Duration) func(net.Conn) (PeerID, error) {
	return func(conn net.Conn) (PeerID, error) {
		o, err := offer()
		if err != nil {
			return 0, err
		}
		if err := o.Validate(); err != nil {
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
		if err := NewMessage(MsgJoinOffer, body).Encode(conn); err != nil {
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
		if err := json.Unmarshal(msg.Payload, &reply); err != nil {
			return 0, fmt.Errorf("join handshake reply: %w", err)
		}
		if reply.Error != "" {
			return 0, errors.New(reply.Error)
		}
		return o.Assigned, nil
	}
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

// WaitStart waits for the host to finish its own roster setup.
func (p *PendingJoin) WaitStart() error {
	if !p.replied {
		return errors.New("join handshake has no reply")
	}
	if p.started {
		return errors.New("join start gate already received")
	}
	if p.base.ConnectTimeout > 0 {
		_ = p.conn.SetReadDeadline(time.Now().Add(p.base.ConnectTimeout))
		defer p.conn.SetReadDeadline(time.Time{})
	}
	msg, err := Decode(p.conn)
	if err != nil {
		return err
	}
	if msg.Type != MsgStart {
		return fmt.Errorf("join handshake: got message %#x, want start", msg.Type)
	}
	p.started = true
	return nil
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
