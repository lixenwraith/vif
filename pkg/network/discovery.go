package network

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DatagramSize is a probe's size and a beacon's ceiling: a host's answer never
// outweighs the question that asked for it.
const DatagramSize = 512

// A discovery datagram opens with the magic, the version and its kind.
const (
	discoveryMagic   = "vif?"
	discoveryVersion = 1
	discoveryHeader  = len(discoveryMagic) + 2

	datagramBeacon = 1
	datagramProbe  = 2
)

// What a beacon's fields are held to. Its text is printable ASCII a JSON string
// carries unescaped, so a beacon has one encoding and a list shows a stranger's
// words plainly.
const (
	beaconNonceLen = 16
	beaconNameMax  = 64
	beaconTextMax  = 48
	beaconDoorsMax = 4
)

// Beacon is a session as its host announces it on the local network: what a list
// shows of it, and the doors it is dialled at on the address its datagram came from.
type Beacon struct {
	Nonce    string       `json:"nonce"` // one session, whichever address announces it
	Name     string       `json:"name"`  // what the session answers to, empty for none
	Scenario string       `json:"scenario"`
	Players  uint16       `json:"players"`
	Ceiling  uint16       `json:"ceiling"`
	Waiting  bool         `json:"waiting"` // its game has not started
	Protocol uint32       `json:"protocol"`
	Build    string       `json:"build"`
	Doors    []BeaconDoor `json:"doors"`
}

// BeaconDoor is one door of a session: tcp, ws or wss, and its port.
type BeaconDoor struct {
	Scheme string `json:"scheme"`
	Port   uint16 `json:"port"`
}

func datagramHeader(kind byte) []byte {
	return append([]byte(discoveryMagic), discoveryVersion, kind)
}

// datagramBody is what follows the header of a datagram of kind, false for any other.
func datagramBody(p []byte, kind byte) ([]byte, bool) {
	if len(p) > DatagramSize || !bytes.HasPrefix(p, datagramHeader(kind)) {
		return nil, false
	}
	return p[discoveryHeader:], true
}

// probeDatagram is the one probe: its header padded to DatagramSize.
var probeDatagram = append(datagramHeader(datagramProbe), make([]byte, DatagramSize-discoveryHeader)...)

// EncodeBeacon is b as its datagram, refused where DecodeBeacon would refuse it.
func EncodeBeacon(b Beacon) ([]byte, error) {
	if err := b.check(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	return append(datagramHeader(datagramBeacon), body...), nil
}

// DecodeBeacon reads a beacon datagram strictly: within check's bounds, and in no
// encoding but the one EncodeBeacon writes.
func DecodeBeacon(p []byte) (Beacon, error) {
	body, ok := datagramBody(p, datagramBeacon)
	if !ok {
		return Beacon{}, errors.New("not a beacon")
	}
	var b Beacon
	if err := json.Unmarshal(body, &b); err != nil {
		return Beacon{}, err
	}
	if err := b.check(); err != nil {
		return Beacon{}, err
	}
	if canon, err := json.Marshal(b); err != nil || !bytes.Equal(canon, body) {
		return Beacon{}, errors.New("a beacon in another encoding")
	}
	return b, nil
}

func (b Beacon) check() error {
	if _, err := hex.DecodeString(b.Nonce); err != nil || len(b.Nonce) != beaconNonceLen || b.Nonce != strings.ToLower(b.Nonce) {
		return errors.New("a beacon's nonce is 16 lowercase hex digits")
	}
	// A name goes into the link a join dials, so it holds to what a path takes as it is.
	if len(b.Name) > beaconNameMax || strings.ContainsFunc(b.Name, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-'
	}) {
		return errors.New("a beacon's name is lowercase alphanumeric or '-'")
	}
	if b.Scenario != BeaconText(b.Scenario) || b.Build != BeaconText(b.Build) {
		return errors.New("a beacon's text is short printable ASCII")
	}
	if len(b.Doors) == 0 || len(b.Doors) > beaconDoorsMax {
		return errors.New("a beacon names one to four doors")
	}
	for _, d := range b.Doors {
		if d.Port == 0 || d.Scheme != "tcp" && d.Scheme != "ws" && d.Scheme != "wss" {
			return errors.New("a beacon's door is tcp, ws or wss on a port")
		}
	}
	return nil
}

// BeaconText fits a host's own words to what a beacon carries: cut to length, each
// character a JSON string would escape or a list could not show turned to '?'.
func BeaconText(s string) string {
	out := make([]byte, 0, beaconTextMax)
	for _, r := range s {
		if len(out) == beaconTextMax {
			break
		}
		if r < ' ' || r > '~' || strings.ContainsRune(`"\<>&`, r) {
			r = '?'
		}
		out = append(out, byte(r))
	}
	return string(out)
}

// BeaconDoors are the doors a beacon names for a port answering at addrs, one bound
// to this machine alone or to no IPv4 address left out.
func BeaconDoors(addrs []net.Addr) []BeaconDoor {
	var doors []BeaconDoor
	for _, a := range addrs {
		scheme, at := "tcp", a.String()
		if u, err := url.Parse(at); err == nil && (u.Scheme == "ws" || u.Scheme == "wss") {
			scheme, at = u.Scheme, u.Host
		}
		bound, err := netip.ParseAddrPort(at)
		ip := bound.Addr().Unmap()
		if err != nil || ip.IsLoopback() || !ip.IsUnspecified() && !ip.Is4() || len(doors) == beaconDoorsMax {
			continue
		}
		doors = append(doors, BeaconDoor{Scheme: scheme, Port: bound.Port()})
	}
	return doors
}

// Found is a session a beacon announced, at the address its datagram came from.
type Found struct {
	Beacon
	Source netip.Addr
}

// door is the one a join takes: tcp where the session has one.
func (f Found) door() BeaconDoor {
	i := max(slices.IndexFunc(f.Doors, func(d BeaconDoor) bool { return d.Scheme == "tcp" }), 0)
	return f.Doors[i]
}

// Address is where the session's door is, as a list names it.
func (f Found) Address() string {
	return netip.AddrPortFrom(f.Source, f.door().Port).String()
}

// Target is the link a join dials, the session's name on it where it answers to one.
func (f Found) Target() string {
	if d := f.door(); d.Scheme != "tcp" {
		return d.Scheme + "://" + f.Address() + "/" + f.Name
	}
	if f.Name == "" {
		return linkScheme + f.Address()
	}
	return linkScheme + f.Address() + "/" + f.Name
}

// DiscoveryConfig is how a run finds sessions on the local network and is found
// there. The session layer fills the port, the timing and the budget.
type DiscoveryConfig struct {
	// Port is where beacons and probes are sent and what a run binds to hear them,
	// an OS-assigned port standing in while another run on the machine holds it.
	Port int

	// Interval is a beacon's and a probe's period. Expiry is how long a session
	// stays listed after its last beacon, and a run keeps looking after it was last
	// asked. Rows is the most sessions listed.
	Interval, Expiry time.Duration
	Rows             int

	// ProbeWindow, ProbesPerSource and Probes bound the probes a host answers in
	// each window, from one source and in all.
	ProbeWindow             time.Duration
	ProbesPerSource, Probes int

	// Armed reports whether the run announces a session it hosts, and Beacon is that
	// session now, nil while it hosts none. Beacon is read once a period and never
	// for a datagram, so it may take its run's locks.
	Armed  func() bool
	Beacon func() *Beacon

	// Subnets replaces asking the system for this machine's networks, where the
	// system keeps them from the process. Listening hears the socket open and close.
	Subnets   func() []netip.Prefix
	Listening func(bool)

	OnError func(error)
}

// Discovery is one run's part in finding sessions on the local network: it
// announces the session the run hosts and lists the ones others announce, on one
// UDP socket open only while it does either. A nil Discovery does neither.
type Discovery struct {
	cfg     DiscoveryConfig
	nonce   string
	limited netip.Addr // the limited broadcast, which a test leaves out

	mu      sync.Mutex
	running bool
	closed  bool
	looking time.Time // the list is wanted until then
	found   map[foundKey]foundRow

	wake chan struct{}
	stop chan struct{}
	wg   sync.WaitGroup
	conn atomic.Pointer[net.UDPConn] // the open socket, for Wake and Close to interrupt
}

type foundKey struct {
	nonce  string
	source netip.Addr
}

type foundRow struct {
	Found
	seen time.Time
}

// NewDiscovery makes a run's discovery, which opens nothing until Wake or Look.
func NewDiscovery(cfg DiscoveryConfig) *Discovery {
	var nonce [beaconNonceLen / 2]byte
	_, _ = rand.Read(nonce[:])
	return &Discovery{
		cfg:     cfg,
		nonce:   hex.EncodeToString(nonce[:]),
		limited: netip.AddrFrom4([4]byte{255, 255, 255, 255}),
		found:   make(map[foundKey]foundRow),
		wake:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
	}
}

// Wake has the run read again what it announces: its player turned that on or off,
// or it began to host.
func (d *Discovery) Wake() {
	if d == nil {
		return
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	// Under the lock, so a goroutine parking sees the wake or this starts the next.
	select {
	case d.wake <- struct{}{}:
	default:
	}
	if !d.running {
		d.running = true
		d.wg.Add(1)
		go d.run()
	}
	d.mu.Unlock()
	d.interrupt()
}

// interrupt ends the read the goroutine waits in, after what it is to see was set.
func (d *Discovery) interrupt() {
	if c := d.conn.Load(); c != nil {
		_ = c.SetReadDeadline(time.Now())
	}
}

// Look lists the sessions found, in address order and each once, at the lowest of
// the addresses it answered from, and keeps the run looking for Expiry more: a list
// still open asks again before that runs out.
func (d *Discovery) Look() []Found {
	if d == nil {
		return nil
	}
	now := time.Now() // [wall] the network's clock, not the game's
	d.mu.Lock()
	lapsed := !now.Before(d.looking)
	d.looking = now.Add(d.cfg.Expiry)
	out := make([]Found, 0, len(d.found))
	for _, r := range d.found {
		if now.Sub(r.seen) <= d.cfg.Expiry {
			out = append(out, r.Found)
		}
	}
	d.mu.Unlock()
	if lapsed {
		d.Wake()
	}
	slices.SortFunc(out, func(a, b Found) int {
		if c := a.Source.Compare(b.Source); c != 0 {
			return c
		}
		return strings.Compare(a.Nonce, b.Nonce)
	})
	listed := out[:0]
	for _, f := range out {
		if !slices.ContainsFunc(listed, func(l Found) bool { return l.Nonce == f.Nonce }) {
			listed = append(listed, f)
		}
	}
	return listed
}

// Close ends the run's discovery and waits for its socket to close.
func (d *Discovery) Close() {
	if d == nil {
		return
	}
	d.mu.Lock()
	if !d.closed {
		d.closed = true
		close(d.stop)
	}
	d.mu.Unlock()
	d.interrupt()
	d.wg.Wait()
}

// run is the discovery's one goroutine: each period it reads what the run
// announces, sends its beacon and its probe, and between periods answers and
// lists what the socket hears. It ends once the run neither announces nor looks.
func (d *Discovery) run() {
	defer d.wg.Done()
	var (
		conn   *net.UDPConn
		answer []byte         // the beacon a probe is answered with, nil while none is announced
		local  []netip.Prefix // this machine's networks, as the last period read them
		budget probeBudget
		failed bool
		next   time.Time
		due    = true
		buf    [DatagramSize + 1]byte // one byte over, so a datagram too long reads as one
	)
	defer func() { d.shut(conn) }()
	for {
		select {
		case <-d.stop:
			return
		case <-d.wake:
			due = true
		default:
		}
		now := time.Now() // [wall]
		armed, looking := d.cfg.Armed != nil && d.cfg.Armed(), d.lookingAt(now)
		if !armed && !looking {
			// Shut before it parks, so the goroutine a wake starts next finds the port free.
			d.shut(conn)
			conn = nil
			if d.park() {
				return
			}
			continue
		}
		if due || !now.Before(next) {
			answer = nil
			if armed {
				if b := d.cfg.Beacon(); b != nil {
					announced := *b
					announced.Nonce = d.nonce
					var err error
					if answer, err = EncodeBeacon(announced); err != nil {
						d.report(err, &failed)
					}
				}
			}
			switch need := answer != nil || looking; {
			case need && conn == nil:
				conn = d.open(&failed)
			case !need:
				d.shut(conn)
				conn = nil
			}
			local = d.subnets()
			if conn != nil {
				for _, to := range d.broadcasts(local) {
					if answer != nil {
						_, _ = conn.WriteToUDPAddrPort(answer, to)
					}
					if looking {
						_, _ = conn.WriteToUDPAddrPort(probeDatagram, to)
					}
				}
			}
			d.expire(now)
			due, next = false, now.Add(d.cfg.Interval)
		}
		if conn == nil {
			timer := time.NewTimer(time.Until(next))
			select {
			case <-d.stop:
				timer.Stop()
				return
			case <-d.wake:
				due = true
			case <-timer.C:
			}
			timer.Stop()
			continue
		}
		// The deadline first, then the wake and the stop: one set after it ends the read.
		_ = conn.SetReadDeadline(next)
		if len(d.wake) > 0 || d.stopped() {
			continue
		}
		n, src, err := conn.ReadFromUDPAddrPort(buf[:])
		var timeout net.Error
		switch from := src.Addr().Unmap(); {
		case errors.As(err, &timeout) && timeout.Timeout():
		case err != nil:
			d.report(err, &failed)
			d.shut(conn)
			conn = nil
		case n > DatagramSize || !onLink(from, local):
		case bytes.Equal(buf[:n], probeDatagram):
			if answer != nil && budget.take(from, time.Now(), &d.cfg) {
				_, _ = conn.WriteToUDPAddrPort(answer, src)
			}
		case looking:
			if b, err := DecodeBeacon(buf[:n]); err == nil && b.Nonce != d.nonce {
				d.note(Found{Beacon: b, Source: from})
			}
		}
	}
}

func (d *Discovery) stopped() bool {
	select {
	case <-d.stop:
		return true
	default:
		return false
	}
}

func (d *Discovery) lookingAt(now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return now.Before(d.looking)
}

// park ends the goroutine, forgetting what it listed, unless a wake came meanwhile.
func (d *Discovery) park() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.wake) > 0 {
		return false
	}
	d.running = false
	clear(d.found)
	return true
}

// open binds the discovery port, or any port while another run on this machine
// holds that one: such a run still probes and is answered, and hears no broadcast.
func (d *Discovery) open(failed *bool) *net.UDPConn {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: d.cfg.Port})
	if err != nil {
		conn, err = net.ListenUDP("udp4", nil)
	}
	if err != nil {
		d.report(err, failed)
		return nil
	}
	*failed = false
	d.conn.Store(conn)
	if d.cfg.Listening != nil {
		d.cfg.Listening(true)
	}
	return conn
}

func (d *Discovery) shut(conn *net.UDPConn) {
	if conn == nil {
		return
	}
	d.conn.Store(nil)
	_ = conn.Close()
	if d.cfg.Listening != nil {
		d.cfg.Listening(false)
	}
}

// report says a failure once, not again each period it lasts.
func (d *Discovery) report(err error, failed *bool) {
	if !*failed && d.cfg.OnError != nil {
		d.cfg.OnError(err)
	}
	*failed = true
}

// note lists f or renews it; a full list takes no stranger.
func (d *Discovery) note(f Found) {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := foundKey{f.Nonce, f.Source}
	if _, listed := d.found[key]; listed || len(d.found) < d.cfg.Rows {
		d.found[key] = foundRow{Found: f, seen: time.Now()} // [wall]
	}
}

func (d *Discovery) expire(now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for k, r := range d.found {
		if now.Sub(r.seen) > d.cfg.Expiry {
			delete(d.found, k)
		}
	}
}

// subnets are this machine's IPv4 networks: its host's word where the system keeps
// them from the process, else those of each interface that is up, has a carrier
// and broadcasts, which the loopback and a point-to-point tunnel do not.
func (d *Discovery) subnets() []netip.Prefix {
	if d.cfg.Subnets != nil {
		return d.cfg.Subnets()
	}
	const live = net.FlagUp | net.FlagRunning | net.FlagBroadcast
	ifis, _ := net.Interfaces()
	var out []netip.Prefix
	for _, ifi := range ifis {
		if ifi.Flags&live != live {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			if p, err := netip.ParsePrefix(a.String()); err == nil && p.Addr().Is4() {
				out = append(out, p)
			}
		}
	}
	return out
}

// broadcasts are where a datagram goes to reach every network in local: each one's
// directed broadcast, then the limited one, which reaches the default network.
func (d *Discovery) broadcasts(local []netip.Prefix) []netip.AddrPort {
	var out []netip.AddrPort
	add := func(a netip.Addr) {
		if to := netip.AddrPortFrom(a, uint16(d.cfg.Port)); a.IsValid() && !slices.Contains(out, to) {
			out = append(out, to)
		}
	}
	for _, p := range local {
		if !p.Addr().Is4() {
			continue
		}
		a, mask := p.Addr().As4(), net.CIDRMask(p.Bits(), 32)
		for i := range a {
			a[i] |= ^mask[i]
		}
		add(netip.AddrFrom4(a))
	}
	add(d.limited)
	return out
}

// onLink reports a source on this machine's own networks: a private or link-local
// address, this machine's, or one inside a network in local.
func onLink(from netip.Addr, local []netip.Prefix) bool {
	return from.IsPrivate() || from.IsLinkLocalUnicast() || from.IsLoopback() ||
		slices.ContainsFunc(local, func(p netip.Prefix) bool { return p.Contains(from) })
}

// probeBudget is the probes a host answers in the window standing: a fixed window,
// so what it keeps is bounded by what it answers.
type probeBudget struct {
	opened time.Time
	spent  int
	by     map[netip.Addr]int
}

func (b *probeBudget) take(from netip.Addr, now time.Time, cfg *DiscoveryConfig) bool {
	if b.by == nil || now.Sub(b.opened) >= cfg.ProbeWindow {
		b.opened, b.spent, b.by = now, 0, make(map[netip.Addr]int)
	}
	if b.spent >= cfg.Probes || b.by[from] >= cfg.ProbesPerSource {
		return false
	}
	b.spent++
	b.by[from]++
	return true
}
