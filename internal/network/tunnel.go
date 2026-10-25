package network

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"net"
	"sync"
	"time"

	"github.com/lixenwraith/vif/internal/parameter"
	link "github.com/lixenwraith/vif/pkg/network"
)

// A door published through a router: the run holds a control connection to the
// router's front door, is named and handed the link it is reached at, and opens a
// connection back for each dial the router places on that name, matched by a ticket
// and spliced to the dialer. TunnelHub is the router's half, DoorTunnel the run's.
// See doc/multi-player.md §5.6.

// MaxTunnelFrame bounds a tunnel frame's payload: a name, a link and two tokens.
const MaxTunnelFrame = 512

// PublishRequest is what a run sends to publish its door: the name the door answers
// to, and the name and token of a door it resumes after a lost connection.
type PublishRequest struct {
	Session string `json:"session,omitempty"`
	ID      string `json:"id,omitempty"`
	Resume  string `json:"resume,omitempty"`
}

// Published is the router's answer: the door's name, the link it is reached at and
// the token that resumes it, or why not.
type Published struct {
	ID     string `json:"id,omitempty"`
	Link   string `json:"link,omitempty"`
	Resume string `json:"resume,omitempty"`
	refusalField
}

// TunnelTicket pairs the connection a run opens back with the dial it is for.
type TunnelTicket struct {
	Ticket string `json:"ticket"`
}

// ErrDoorUnanswered is a dial placed on a door whose run opened no connection back
// in time, which a dialer may retry: the run may be slow, or gone unnoticed yet.
var ErrDoorUnanswered = errors.New("the door did not open in time")

// ErrDoorTaken refuses a run resuming a name another run holds live.
var ErrDoorTaken = &Refusal{Code: RefusalIdentity, Reason: "this door is published by another run"}

// ErrNoDoor is a name no door is published under.
var ErrNoDoor = errors.New("no door is published under this name")

// ErrNoDoorToResume refuses a run resuming a name this router never issued, or one
// it issued longer ago than NetworkTunnelResumeWindow: a pod's name, or anyone's.
var ErrNoDoorToResume = &Refusal{Code: RefusalIdentity, Reason: "no door of this name was published here to resume"}

// ErrDoorsPerAddress refuses a publication from an address holding as many doors as
// one may.
var ErrDoorsPerAddress = &Refusal{Code: RefusalFull, Reason: "this address publishes as many doors as it may"}

// PublishedDoor is one door a router carries: its name, the name its run answers
// to, its token, the address it was published from and the control connection the
// run holds.
type PublishedDoor struct {
	id, session, resume, from string
	control                   net.Conn
	writeMu                   sync.Mutex
}

// lostDoor is a door whose control connection went: its token, for the run that held
// it to resume the name by until a deadline.
type lostDoor struct {
	resume string
	until  time.Time
}

// ID is the door's name, from the pods' identifier space.
func (d *PublishedDoor) ID() string { return d.id }

// send writes one frame on the control connection, one writer at a time.
func (d *PublishedDoor) send(kind link.MessageType, payload []byte) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_ = d.control.SetWriteDeadline(time.Now().Add(parameter.NetworkTunnelOpenTimeout))
	err := link.NewMessage(kind, payload).Encode(d.control)
	_ = d.control.SetWriteDeadline(time.Time{})
	return err
}

// TunnelHub is a router's registry of the doors published through it.
type TunnelHub struct {
	link         func(id string) string
	max          int
	perAddress   int // doors one address may hold at once; 0 for no bound
	resumeWindow time.Duration

	mu      sync.Mutex
	doors   map[string]*PublishedDoor
	waiting map[string]chan net.Conn
	// lost keeps the name and token of each door whose control connection went, for
	// the run that held it to resume within the window; a name never issued here, a
	// pod's or anyone's, is nobody's to take.
	lost map[string]lostDoor
}

// NewTunnelHub carries at most max doors, perAddress of them from one address, each
// reached at link(id).
func NewTunnelHub(link func(id string) string, max, perAddress int) *TunnelHub {
	return &TunnelHub{link: link, max: max, perAddress: perAddress, resumeWindow: parameter.NetworkTunnelResumeWindow,
		doors: make(map[string]*PublishedDoor), waiting: make(map[string]chan net.Conn), lost: make(map[string]lostDoor)}
}

// Max is how many doors the hub carries at most.
func (h *TunnelHub) Max() int { return h.max }

// Publish takes a run's publication on control, which the hub holds as the door's
// control connection from here, and answers it on the same stream: the door's name,
// link and token, or the refusal returned, the stream closed. A run resuming its
// name with its token keeps it, the connection it lost closed, within the resume
// window of the loss; from is the address it publishes from, which the per-address
// bound counts.
func (h *TunnelHub) Publish(control net.Conn, from string, req PublishRequest) (*PublishedDoor, error) {
	d, err := h.admit(control, from, req)
	var ans Published
	if err != nil {
		ans.Refuse(err)
	} else {
		ans = Published{ID: d.id, Link: h.link(d.id), Resume: d.resume}
	}
	body, _ := json.Marshal(ans) // plain data cannot fail to encode
	_ = control.SetWriteDeadline(time.Now().Add(parameter.NetworkTunnelOpenTimeout))
	written := link.NewMessage(MsgSessionPublished, body).Encode(control)
	_ = control.SetWriteDeadline(time.Time{})
	if err == nil && written != nil {
		h.remove(d)
		err = written
	}
	if err != nil {
		_ = control.Close()
		return nil, err
	}
	return d, nil
}

// admit names the door, or says why not: a name is resumed only by the run that was
// issued it, while it holds it live or within the window of losing it.
func (h *TunnelHub) admit(control net.Conn, from string, req PublishRequest) (*PublishedDoor, error) {
	if len(req.Session) > MaxSessionName {
		return nil, &Refusal{Code: RefusalIdentity, Reason: "the door's session name is too long"}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	maps.DeleteFunc(h.lost, func(_ string, l lostDoor) bool { return now.After(l.until) })
	id, resume := req.ID, req.Resume
	if id != "" || resume != "" {
		if !validTunnelID(id) {
			return nil, &Refusal{Code: RefusalIdentity, Reason: "a door's name is 16 hex characters"}
		}
		switch old, live := h.doors[id]; {
		case live && old.resume == resume:
			_ = old.control.Close()
			delete(h.doors, id)
		case live:
			return nil, ErrDoorTaken
		default:
			if l, ok := h.lost[id]; !ok || l.resume != resume {
				return nil, ErrNoDoorToResume
			}
			delete(h.lost, id)
		}
	}
	if len(h.doors) >= h.max {
		return nil, &Refusal{Code: RefusalFull, Reason: "this router carries as many doors as it accepts"}
	}
	if h.perAddress > 0 && from != "" && h.heldByLocked(from) >= h.perAddress {
		return nil, ErrDoorsPerAddress
	}
	var err error
	for id == "" || h.doors[id] != nil {
		if id, err = randomHex(8); err != nil {
			return nil, err
		}
	}
	if resume == "" {
		if resume, err = randomHex(16); err != nil {
			return nil, err
		}
	}
	d := &PublishedDoor{id: id, session: req.Session, resume: resume, from: from, control: control}
	h.doors[id] = d
	return d, nil
}

// heldByLocked counts the doors published from one address. Caller holds mu.
func (h *TunnelHub) heldByLocked(from string) int {
	n := 0
	for _, d := range h.doors {
		if d.from == from {
			n++
		}
	}
	return n
}

// Serve reads the door's control connection until it ends: keeps are answered, and
// a run silent past NetworkTunnelKeepLost is gone. The door is removed with it.
func (h *TunnelHub) Serve(d *PublishedDoor) {
	for {
		_ = d.control.SetReadDeadline(time.Now().Add(parameter.NetworkTunnelKeepLost))
		msg, err := link.DecodeWithin(d.control, MaxTunnelFrame)
		if err != nil {
			break
		}
		if msg.Type == MsgTunnelKeep && d.send(MsgTunnelKeep, nil) != nil {
			break
		}
	}
	h.remove(d)
	_ = d.control.Close()
}

// remove forgets d, unless its name was resumed by another connection since, keeping
// its name and token for the run to resume within the window.
func (h *TunnelHub) remove(d *PublishedDoor) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.doors[d.id] == d {
		delete(h.doors, d.id)
		h.lost[d.id] = lostDoor{resume: d.resume, until: time.Now().Add(h.resumeWindow)}
	}
}

// Has reports a door published under id.
func (h *TunnelHub) Has(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.doors[id] != nil
}

// Count is how many doors the hub carries.
func (h *TunnelHub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.doors)
}

// Open asks the door's run for a connection back and returns it with the name the
// run's door answers to, or ErrDoorUnanswered when none opened within timeout.
func (h *TunnelHub) Open(id string, timeout time.Duration) (net.Conn, string, error) {
	h.mu.Lock()
	d := h.doors[id]
	h.mu.Unlock()
	if d == nil {
		return nil, "", ErrNoDoor
	}
	ticket, err := randomHex(16)
	if err != nil {
		return nil, "", err
	}
	ch := make(chan net.Conn, 1)
	h.mu.Lock()
	h.waiting[ticket] = ch
	h.mu.Unlock()
	body, _ := json.Marshal(TunnelTicket{Ticket: ticket})
	if d.send(MsgTunnelOpen, body) != nil {
		h.forget(ticket, ch)
		_ = d.control.Close() // Serve removes it
		return nil, "", ErrDoorUnanswered
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case conn := <-ch:
		return conn, d.session, nil
	case <-timer.C:
		h.forget(ticket, ch)
		return nil, "", ErrDoorUnanswered
	}
}

// forget drops a ticket nobody waits on any more, closing a connection that arrived
// as the wait ran out; Accept hands one over under the same lock, so none is lost.
func (h *TunnelHub) forget(ticket string, ch chan net.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.waiting, ticket)
	select {
	case conn := <-ch:
		_ = conn.Close()
	default:
	}
}

// Accept hands a connection a run opened back to the dial its ticket names, and
// reports whether one waited for it; the caller closes one nobody did.
func (h *TunnelHub) Accept(conn net.Conn, ticket string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := h.waiting[ticket]
	if ch == nil {
		return false
	}
	delete(h.waiting, ticket)
	ch <- conn
	return true
}

// RefusePublish answers a publication the router turns away before the hub sees it.
func RefusePublish(conn net.Conn, cause error) {
	var ans Published
	ans.Refuse(cause)
	body, _ := json.Marshal(ans)
	_ = conn.SetWriteDeadline(time.Now().Add(parameter.NetworkTunnelOpenTimeout))
	_ = link.NewMessage(MsgSessionPublished, body).Encode(conn)
}

// DoorTunnel is a run's door published through a router. Run holds the control
// connection, publishing again with a growing wait when it drops and resuming its
// name, and serves each connection the router asks for.
type DoorTunnel struct {
	Router  string // the router's front door, host:port
	Session string // the name this run's door answers to
	// Dial opens a connection to the router; nil dials TCP.
	Dial func(addr string) (net.Conn, error)

	mu               sync.Mutex
	id, resume, door string
	writeMu          sync.Mutex
}

// Door is the link the router last named for this door, empty before one.
func (t *DoorTunnel) Door() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.door
}

// Run publishes and serves until ctx ends. published is told the door's link on
// each publication, serve runs a door's handshake on each connection opened back,
// and lost is told why a control connection ended; each may be nil. A refusal no
// retry helps ends the run.
func (t *DoorTunnel) Run(ctx context.Context, published func(door string), serve func(net.Conn), lost func(error)) {
	wait := time.Second
	for ctx.Err() == nil {
		err := t.hold(ctx, published, serve)
		if ctx.Err() != nil {
			return
		}
		if lost != nil {
			lost(err)
		}
		if RetryOf(err) == RetryNever {
			return
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return
		}
		wait = min(wait*2, 30*time.Second)
	}
}

// hold publishes on one control connection and serves it until it ends.
func (t *DoorTunnel) hold(ctx context.Context, published func(string), serve func(net.Conn)) error {
	conn, err := t.dial()
	if err != nil {
		return err
	}
	defer conn.Close()
	defer context.AfterFunc(ctx, func() { _ = conn.Close() })()
	t.mu.Lock()
	req := PublishRequest{Session: t.Session, ID: t.id, Resume: t.resume}
	t.mu.Unlock()
	body, _ := json.Marshal(req)
	_ = conn.SetDeadline(time.Now().Add(parameter.NetworkTunnelOpenTimeout))
	if err := link.NewMessage(MsgSessionPublish, body).Encode(conn); err != nil {
		return err
	}
	msg, err := link.DecodeWithin(conn, MaxTunnelFrame)
	if err != nil {
		return err
	}
	var ans Published
	if msg.Type != MsgSessionPublished || json.Unmarshal(msg.Payload, &ans) != nil {
		return errors.New("the router answered with no publication")
	}
	if err := ans.Refused(); err != nil {
		return err
	}
	if !validTunnelID(ans.ID) || ans.Link == "" {
		return errors.New("the router named no door")
	}
	_ = conn.SetDeadline(time.Time{})
	t.mu.Lock()
	t.id, t.resume, t.door = ans.ID, ans.Resume, ans.Link
	t.mu.Unlock()
	if published != nil {
		published(ans.Link)
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		keep := time.NewTicker(parameter.NetworkTunnelKeepInterval)
		defer keep.Stop()
		for {
			select {
			case <-keep.C:
				if t.send(conn, MsgTunnelKeep, nil) != nil {
					_ = conn.Close()
					return
				}
			case <-done:
				return
			}
		}
	}()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(parameter.NetworkTunnelKeepLost))
		msg, err := link.DecodeWithin(conn, MaxTunnelFrame)
		if err != nil {
			return err
		}
		var ticket TunnelTicket
		if msg.Type != MsgTunnelOpen || json.Unmarshal(msg.Payload, &ticket) != nil || ticket.Ticket == "" {
			continue
		}
		go t.answer(ticket.Ticket, serve)
	}
}

// answer opens the connection back a ticket asks for and hands it to serve.
func (t *DoorTunnel) answer(ticket string, serve func(net.Conn)) {
	conn, err := t.dial()
	if err != nil {
		return
	}
	body, _ := json.Marshal(TunnelTicket{Ticket: ticket})
	_ = conn.SetWriteDeadline(time.Now().Add(parameter.NetworkTunnelOpenTimeout))
	if err := link.NewMessage(MsgTunnelAccept, body).Encode(conn); err != nil || serve == nil {
		_ = conn.Close()
		return
	}
	_ = conn.SetWriteDeadline(time.Time{})
	serve(conn)
}

func (t *DoorTunnel) send(conn net.Conn, kind link.MessageType, payload []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(parameter.NetworkTunnelOpenTimeout))
	err := link.NewMessage(kind, payload).Encode(conn)
	_ = conn.SetWriteDeadline(time.Time{})
	return err
}

func (t *DoorTunnel) dial() (net.Conn, error) {
	if t.Dial != nil {
		return t.Dial(t.Router)
	}
	cfg := link.DefaultConfig()
	cfg.ConnectTimeout = parameter.NetworkJoinConnectTimeout
	return link.Dial(t.Router, cfg)
}

// validTunnelID is a door's name as the router issues one: 16 hex characters.
func validTunnelID(id string) bool {
	_, err := hex.DecodeString(id)
	return len(id) == 16 && err == nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
