package network

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"slices"
	"testing"
	"time"
)

// scripted is a dialer whose bytes are fixed in advance, and keeps what the
// handshake writes back. Reading past its bytes ends the stream, as a dialer that
// stops sending ends at its deadline.
type scripted struct {
	in  *bytes.Reader
	out bytes.Buffer
}

var scriptedAddr = &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 7777}

func (c *scripted) Read(p []byte) (int, error)       { return c.in.Read(p) }
func (c *scripted) Write(p []byte) (int, error)      { return c.out.Write(p) }
func (c *scripted) Close() error                     { return nil }
func (c *scripted) LocalAddr() net.Addr              { return scriptedAddr }
func (c *scripted) RemoteAddr() net.Addr             { return scriptedAddr }
func (c *scripted) SetDeadline(time.Time) error      { return nil }
func (c *scripted) SetReadDeadline(time.Time) error  { return nil }
func (c *scripted) SetWriteDeadline(time.Time) error { return nil }

// frames is msgs as a dialer writes them, each a type and its JSON or raw body.
func frames(msgs ...any) []byte {
	var b bytes.Buffer
	for i := 0; i < len(msgs); i += 2 {
		body, ok := msgs[i+1].([]byte)
		if !ok {
			body, _ = json.Marshal(msgs[i+1])
		}
		_ = NewMessage(msgs[i].(MessageType), body).Encode(&b)
	}
	return b.Bytes()
}

// FuzzAJoinHandshakeHoldsOnlyTheIdentityItAdmits: whatever a dialer sends, it is
// answered first with an offer or the reason it has none, and the identity Assign
// gave it is either admitted and reported once, or released once.
func FuzzAJoinHandshakeHoldsOnlyTheIdentityItAdmits(f *testing.F) {
	const name, digest = "0123456789abcdef", "sha256:2604b13fd259"
	ours := sampleIdentity()
	other := ours
	other.Protocol++
	elsewhere := ours
	elsewhere.Seed++
	reply := sessionReply{Identity: ours, Width: 120, Height: 40}
	refusal := sessionReply{}
	refusal.Refuse(errors.New("join mismatch"))
	whole := frames(MsgJoinReply, reply)
	f.Add(whole, false, true, true, true)
	f.Add(frames(MsgSessionRoute, []byte(name), MsgJoinReply, reply), true, true, true, true)
	f.Add(frames(MsgSessionRoute, []byte("another"), MsgJoinReply, reply), true, true, true, true)
	f.Add(frames(MsgSessionRoute, bytes.Repeat([]byte{'n'}, MaxSessionName+1)), true, true, true, true)
	f.Add(frames(MsgScenarioRequest, []byte(digest), MsgJoinReply, reply), false, true, true, true)
	f.Add(frames(MsgScenarioRequest, []byte(digest), MsgScenarioRequest, []byte(digest)), false, true, true, true)
	f.Add(frames(MsgScenarioRequest, []byte("sha256:0"), MsgJoinReply, reply), false, true, true, true)
	f.Add(frames(MsgJoinReply, sessionReply{Identity: other}), false, true, true, true)
	f.Add(frames(MsgJoinReply, sessionReply{Identity: elsewhere}), false, true, true, true)
	f.Add(frames(MsgJoinReply, sessionReply{Identity: ours, Holder: 1, Listen: "192.0.2.2:7777"}), false, true, true, true)
	f.Add(frames(MsgJoinReply, refusal), false, true, true, true)
	f.Add(frames(MsgJoinReply, reply, MsgJoinReply, reply), false, true, true, true)
	f.Add(frames(MsgJoinReply, []byte(`{"code":200,"error":"x"`)), false, true, true, true)
	f.Add(frames(MsgEvent, []byte("not a reply")), false, true, true, true)
	f.Add(whole[:len(whole)-1], false, true, true, true)
	f.Add([]byte{byte(MsgJoinReply), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff}, false, true, true, true)
	f.Add([]byte{}, false, true, true, true)
	f.Add(whole, false, false, true, true)
	f.Add(whole, false, true, false, true)
	f.Add(whole, false, true, true, false)
	f.Fuzz(func(t *testing.T, script []byte, named, budget, room, reports bool) {
		offer := testOffer()
		offer.Identity = ours
		var assigned, released, reported []PeerID
		c := Coordinator{
			Assign: func() (SessionOffer, error) {
				if !room {
					return SessionOffer{}, &Refusal{Code: RefusalFull, Reason: "session is full"}
				}
				assigned = append(assigned, offer.Assigned)
				return offer, nil
			},
			Release: func(id PeerID) { released = append(released, id) },
			Report: func(id PeerID, _ JoinerReport) error {
				if reported = append(reported, id); !reports {
					return errors.New("participant left during admission")
				}
				return nil
			},
			Admit: func(net.Addr) error {
				if !budget {
					return &Refusal{Code: RefusalBudget, Reason: "this address has joined too often"}
				}
				return nil
			},
			Scenario: func(d string) ([]byte, error) {
				if d != digest {
					return nil, errors.New("this session plays another scenario")
				}
				return []byte("a deflated scenario"), nil
			},
		}
		if named {
			c.Name = name
		}
		conn := &scripted{in: bytes.NewReader(script)}
		id, err := HostAcceptor(c, time.Second)(conn)

		first, derr := Decode(bytes.NewReader(conn.out.Bytes()))
		var answer sessionReply
		switch {
		case derr != nil:
			t.Fatalf("the dialer was not answered: %v", derr)
		case first.Type == MsgJoinReply && (json.Unmarshal(first.Payload, &answer) != nil || answer.Refused() == nil):
			t.Fatalf("the dialer was refused with no reason: %q", first.Payload)
		case first.Type != MsgJoinReply && first.Type != MsgJoinOffer:
			t.Fatalf("the dialer was answered with message %#x", first.Type)
		}
		switch {
		case len(assigned) > 1:
			t.Fatalf("one handshake was assigned %v", assigned)
		case err == nil && (len(assigned) != 1 || id != assigned[0] || released != nil || !slices.Equal(reported, assigned)):
			t.Fatalf("admitted %d: assigned %v, released %v, reported %v", id, assigned, released, reported)
		case err != nil && (id != 0 || !slices.Equal(released, assigned)):
			t.Fatalf("refused as %d (%v): assigned %v, released %v", id, err, assigned, released)
		}
	})
}

// FuzzAPeerLinkOpensOnlyForAnAdmittedParticipant: whatever a dialer sends, a link
// opens only for a participant the gate admits, and a dialer that said who it is
// is answered with one ack, refusing exactly the links that did not open.
func FuzzAPeerLinkOpensOnlyForAnAdmittedParticipant(f *testing.F) {
	ours := sampleIdentity()
	other := ours
	other.Simulation = "deadbeef"
	f.Add(frames(MsgConnect, peerHello{From: 3, Identity: ours}))
	f.Add(frames(MsgConnect, peerHello{From: 9, Identity: ours}))
	f.Add(frames(MsgConnect, peerHello{From: 2, Identity: ours}))
	f.Add(frames(MsgConnect, peerHello{Identity: ours}))
	f.Add(frames(MsgConnect, peerHello{From: 3, Identity: other}))
	f.Add(frames(MsgConnect, peerHello{From: 3, Identity: ours}, MsgConnect, peerHello{From: 4, Identity: ours}))
	f.Add(frames(MsgConnect, []byte(`{"from":3,`)))
	f.Add(frames(MsgJoinReply, peerHello{From: 3, Identity: ours}))
	f.Add([]byte{byte(MsgConnect), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, script []byte) {
		admitted := []PeerID{3, 4}
		gate := PeerGate{Local: 2, Identity: ours, Admit: func(from PeerID) error {
			if !slices.Contains(admitted, from) {
				return errors.New("peer link: participant is not in this session")
			}
			return nil
		}}
		conn := &scripted{in: bytes.NewReader(script)}
		id, err := PeerAcceptor(gate, time.Second)(conn)
		if (err == nil) != slices.Contains(admitted, id) || (err != nil && id != 0) {
			t.Fatalf("opened as %d (%v)", id, err)
		}
		if conn.out.Len() == 0 {
			if err == nil {
				t.Fatalf("opened a link for %d without answering it", id)
			}
			return
		}
		out := bytes.NewReader(conn.out.Bytes())
		ack, derr := Decode(out)
		var welcome peerWelcome
		if derr != nil || ack.Type != MsgAck || json.Unmarshal(ack.Payload, &welcome) != nil || out.Len() != 0 {
			t.Fatalf("answered with %q", conn.out.Bytes())
		}
		if welcome.From != gate.Local || (welcome.Refused() == nil) != (err == nil) {
			t.Fatalf("answered as %d refusing %v a link that opened: %t", welcome.From, welcome.Refused(), err == nil)
		}
	})
}
