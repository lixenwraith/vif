package network

import (
	"bufio"
	"bytes"
	"cmp"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// A feed is one stream encoded once and copied to every subscriber, as records of
// [kind:1][length:uvarint][body]. It runs one way: a subscriber sends nothing, so
// what a subscriber costs is its copy of bytes the others are sent too.
const (
	FeedSync byte = 1 // a starting point: the publisher's state as of the cut before it
	FeedData byte = 2 // frames and tick marks, deflated against the data since the cut
	FeedCut  byte = 3 // the tick the stream stands at; the deflate history restarts
	FeedEnd  byte = 4 // the feed is over; the body says whether it comes back, and why
	FeedKeep byte = 5 // nothing to carry: the feed is alive
)

// A data record's body is the tick its first mark closes and its plain length, as
// uvarints, then its deflate: a frame as [1][kind][length:uvarint][payload], and a
// mark as [0], which closes the tick after the one the mark before it closed.
const (
	feedMark  byte = 0
	feedFrame byte = 1

	feedChunkBytes = 64 << 10 // plain bytes that send a data record before its marks do
	feedDataMax    = 1 << 20  // the largest record but a starting point, plain or deflated
	feedGather     = 32 << 10 // bytes a subscriber's writer gathers into one write
)

var (
	ErrFeedFull  = errors.New("the feed has every subscriber it takes")
	ErrFeedEnded = errors.New("the feed has ended")
)

// FeedEnded is how a feed its publisher ended reads: the reason it gave, and Again
// where the publisher is to serve it anew where a subscriber found it.
type FeedEnded struct {
	Reason string
	Again  bool
}

func (e FeedEnded) Error() string { return e.Reason }

// FeedConfig sizes a feed; DefaultFeedConfig fills it.
type FeedConfig struct {
	Level        int           // deflate level of the data records
	Marks        int           // tick marks a data record gathers before it is sent
	Subscribers  int           // how many may subscribe at once
	Queue        int           // records a subscriber may fall behind before it is dropped
	Tail         int           // bytes of records a starting point is kept under
	Head         int           // the largest starting point a reader takes
	Backlog      int           // whole ticks a port holds before its run is too far behind
	Keepalive    time.Duration // the longest a live feed stays silent
	Silence      time.Duration // how long a reader waits on a silent feed before it is lost
	WriteTimeout time.Duration // how long one write to a subscriber may take
}

func DefaultFeedConfig() FeedConfig {
	return FeedConfig{
		Level: flate.DefaultCompression, Marks: 10, Subscribers: 64, Queue: 256,
		Tail: 4 << 20, Head: 32 << 20, Backlog: 1200,
		Keepalive: 2 * time.Second, Silence: 10 * time.Second, WriteTimeout: 5 * time.Second,
	}
}

// FeedTotals are a feed's bytes, each counted once however many subscribers it has:
// its frames and marks before deflate, the records they became, and its starting points.
type FeedTotals struct{ Plain, Stream, Heads uint64 }

// feedRecord frames one record.
func feedRecord(kind byte, body ...[]byte) []byte {
	n := 0
	for _, b := range body {
		n += len(b)
	}
	rec := binary.AppendUvarint(append(make([]byte, 0, 1+binary.MaxVarintLen64+n), kind), uint64(n))
	for _, b := range body {
		rec = append(rec, b...)
	}
	return rec
}

// FeedBody is a record's body.
func FeedBody(rec []byte) []byte {
	if len(rec) < 2 {
		return nil
	}
	_, n := binary.Uvarint(rec[1:])
	return rec[1+max(n, 0):]
}

// FeedWriter encodes one feed for its hub. Frames gather under the tick mark that
// follows them into data records, each deflated against those since the last cut.
type FeedWriter struct {
	hub   *FeedHub
	every int

	mu      sync.Mutex
	z       *flate.Writer
	seg     bytes.Buffer // the record in progress, deflated
	scratch []byte
	plain   int    // bytes deflated into it
	marks   int    // tick marks in it
	tick    uint64 // the last tick marked
	cuts    uint64
}

func NewFeedWriter(hub *FeedHub) *FeedWriter {
	w := &FeedWriter{hub: hub, every: max(hub.cfg.Marks, 1)}
	z, err := flate.NewWriter(&w.seg, hub.cfg.Level)
	if err != nil {
		z, _ = flate.NewWriter(&w.seg, flate.DefaultCompression)
	}
	w.z = z
	return w
}

// Frame adds one frame to the tick the next mark closes.
func (w *FeedWriter) Frame(kind MessageType, payload []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.scratch = binary.AppendUvarint(append(w.scratch[:0], feedFrame, byte(kind)), uint64(len(payload)))
	w.write(w.scratch)
	w.write(payload)
	if w.plain >= feedChunkBytes {
		w.flush()
	}
}

// Mark closes tick: every frame added since the mark before it is due by it. A tick
// that does not follow the last one cuts the stream there, as a new game's first does.
func (w *FeedWriter) Mark(tick uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if tick != w.tick+1 {
		w.flush()
		w.cut(tick - 1)
	}
	w.write([]byte{feedMark})
	w.tick, w.marks = tick, w.marks+1
	if w.marks >= w.every {
		w.flush()
	}
}

// Flush sends what has gathered, for a publisher whose ticks have stopped.
func (w *FeedWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flush()
}

// Cut opens a starting point at tick, where the stream stands: what gathered before
// it is sent and the deflate history restarts. It returns the cut's number, which
// Sync publishes the starting point under.
func (w *FeedWriter) Cut(tick uint64) uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flush()
	w.cut(tick)
	return w.cuts
}

// Sync publishes the starting point taken at cut, unless a later cut replaced it.
func (w *FeedWriter) Sync(cut uint64, body []byte) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if cut != w.cuts {
		return false
	}
	w.hub.Publish(feedRecord(FeedSync, body))
	return true
}

func (w *FeedWriter) write(p []byte) {
	n, _ := w.z.Write(p) // into a buffer: it cannot fail
	w.plain += n
	w.hub.plain.Add(uint64(n))
}

// flush sends the record in progress. Caller holds mu.
func (w *FeedWriter) flush() {
	if w.plain == 0 {
		return
	}
	_ = w.z.Flush()
	head := binary.AppendUvarint(w.scratch[:0], w.tick-uint64(w.marks)+1)
	head = binary.AppendUvarint(head, uint64(w.plain))
	w.hub.Publish(feedRecord(FeedData, head, w.seg.Bytes()))
	w.scratch = head
	w.seg.Reset()
	w.plain, w.marks = 0, 0
}

// cut publishes the cut record and restarts the deflate. Caller holds mu.
func (w *FeedWriter) cut(tick uint64) {
	w.hub.Publish(feedRecord(FeedCut, binary.AppendUvarint(nil, tick)))
	w.z.Reset(&w.seg)
	w.tick = tick
	w.cuts++
}

// FeedHub copies one feed to its subscribers, starting points included. It keeps the
// newest of those and the records since its cut, which is where a newcomer begins; a
// subscriber that falls a queue behind is dropped rather than waited for.
type FeedHub struct {
	cfg FeedConfig

	mu    sync.Mutex
	subs  map[*feedSub]bool // true once sent its starting point
	head  []byte            // the newest starting point, nil until one and once its tail outgrew
	tail  [][]byte          // the records since head's cut, the cut first
	pend  [][]byte          // the records since a cut whose starting point has not come
	size  int               // bytes in tail
	ahead int               // bytes in pend
	open  bool              // pend is being kept
	fresh bool              // no cut or starting point has passed: a copy's first comes before its cut
	ended bool
	last  time.Time // the last record's wall time

	want chan struct{}
	stop chan struct{}

	plain, stream, heads atomic.Uint64
}

func NewFeedHub(cfg FeedConfig) *FeedHub {
	h := &FeedHub{
		cfg: cfg, subs: make(map[*feedSub]bool), fresh: true,
		last: time.Now(), // [wall] the link, not the game
		want: make(chan struct{}, 1), stop: make(chan struct{}),
	}
	if cfg.Keepalive > 0 {
		go h.keepalive()
	}
	return h
}

// Publish copies one whole record to the subscribers it is for, and keeps it where a
// newcomer needs it. The hub holds rec from here on.
func (h *FeedHub) Publish(rec []byte) {
	if len(rec) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ended {
		return
	}
	h.last = time.Now() // [wall]
	switch rec[0] {
	case FeedSync:
		h.heads.Add(uint64(len(rec)))
		h.start(rec)
		for s, started := range h.subs {
			switch {
			case started:
				h.send(s, rec)
			case h.head != nil:
				h.begin(s)
			}
		}
		return
	case FeedEnd:
		h.ended = true
		close(h.stop)
		for s := range h.subs {
			if h.send(s, rec) {
				close(s.ch)
			}
		}
		return
	case FeedCut:
		h.pend, h.ahead, h.open, h.fresh = nil, 0, true, false
	}
	h.stream.Add(uint64(len(rec)))
	if rec[0] != FeedKeep {
		h.keep(rec)
	}
	for s, started := range h.subs {
		if started {
			h.send(s, rec)
		}
	}
}

// start makes rec the starting point if the records since its cut are held: a cut
// whose records outgrew the tail has none a newcomer could begin from.
func (h *FeedHub) start(rec []byte) {
	switch {
	case h.open:
		h.head, h.tail, h.size = rec, h.pend, h.ahead
	case h.fresh:
		h.head, h.tail, h.size = rec, nil, 0
	default:
		h.head, h.tail, h.size = nil, nil, 0
	}
	h.pend, h.ahead, h.open, h.fresh = nil, 0, false, false
}

// keep holds rec for the newcomers that begin before it, under the tail's bounds.
func (h *FeedHub) keep(rec []byte) {
	over := func(n, records int) bool { return n > h.cfg.Tail || records >= h.cfg.Queue/2 }
	if h.head != nil {
		h.tail, h.size = append(h.tail, rec), h.size+len(rec)
		if over(h.size, len(h.tail)) {
			h.head, h.tail, h.size = nil, nil, 0
		}
	}
	if h.open {
		h.pend, h.ahead = append(h.pend, rec), h.ahead+len(rec)
		if over(h.ahead, len(h.pend)) {
			h.pend, h.ahead, h.open = nil, 0, false
		}
	}
}

// begin sends a subscriber the starting point and what followed it.
func (h *FeedHub) begin(s *feedSub) {
	h.subs[s] = true
	if !h.send(s, h.head) {
		return
	}
	for _, rec := range h.tail {
		if !h.send(s, rec) {
			return
		}
	}
}

// send queues rec for one subscriber, dropping the subscriber whose queue is full.
func (h *FeedHub) send(s *feedSub, rec []byte) bool {
	select {
	case s.ch <- rec:
		return true
	default:
		h.dropLocked(s)
		return false
	}
}

// Subscribe copies the feed to conn from its newest starting point, or from the
// next one when it has none. The hub owns conn from here on.
func (h *FeedHub) Subscribe(conn net.Conn) error {
	s := &feedSub{conn: conn, ch: make(chan []byte, h.cfg.Queue), gone: make(chan struct{})}
	h.mu.Lock()
	switch {
	case h.ended:
		h.mu.Unlock()
		return ErrFeedEnded
	case len(h.subs) >= h.cfg.Subscribers:
		h.mu.Unlock()
		return ErrFeedFull
	}
	h.subs[s] = false
	if h.head != nil {
		h.begin(s)
	} else {
		select {
		case h.want <- struct{}{}:
		default:
		}
	}
	h.mu.Unlock()
	go s.write(h)
	go s.watch(h)
	return nil
}

// Wanted signals a subscriber waiting on a starting point the hub does not have.
func (h *FeedHub) Wanted() <-chan struct{} { return h.want }

// Waiting reports whether a subscriber has no starting point yet.
func (h *FeedHub) Waiting() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, started := range h.subs {
		if !started {
			return true
		}
	}
	return false
}

func (h *FeedHub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// Idle reports a feed with no subscriber, and drops its starting point: its writer
// may stop then, and a feed that stopped cannot be begun from where it was.
func (h *FeedHub) Idle() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subs) != 0 {
		return false
	}
	h.head, h.tail, h.size = nil, nil, 0
	h.pend, h.ahead, h.open = nil, 0, false
	return true
}

// End tells every subscriber why the feed is over, and whether its publisher is to
// serve it anew where they found it, and closes it.
func (h *FeedHub) End(reason string, again bool) {
	flag := []byte{0}
	if again {
		flag[0] = 1
	}
	h.Publish(feedRecord(FeedEnd, flag, []byte(reason)))
}

func (h *FeedHub) Totals() FeedTotals {
	return FeedTotals{Plain: h.plain.Load(), Stream: h.stream.Load(), Heads: h.heads.Load()}
}

// Repeat publishes the feed conn carries as this hub's own, each record as it came
// and none inflated, until that feed ends, which its subscribers are told as it was
// told, or is lost, which ends this hub's as one to find again. each is shown every
// record before the subscribers are sent it. The caller closes conn.
func (h *FeedHub) Repeat(conn net.Conn, each func(rec []byte)) error {
	r := NewFeedReader(conn, h.cfg)
	for {
		if h.cfg.Silence > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(h.cfg.Silence)) // [wall] the link, not the game
		}
		rec, err := r.Next()
		if err != nil {
			h.End("The feed was lost", true)
			return err
		}
		if each != nil {
			each(rec)
		}
		h.Publish(rec)
		if rec[0] == FeedEnd {
			return feedEnded(rec)
		}
	}
}

// feedEnded reads an end record: whether the feed comes back, and why it ended.
func feedEnded(rec []byte) FeedEnded {
	body := FeedBody(rec)
	if len(body) == 0 {
		return FeedEnded{}
	}
	return FeedEnded{Reason: string(body[1:]), Again: body[0] != 0}
}

func (h *FeedHub) drop(s *feedSub) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dropLocked(s)
}

// dropLocked lets a subscriber go. Its writer closes the link, off this lock: a
// close can wait on the far end, and a publisher must not.
func (h *FeedHub) dropLocked(s *feedSub) {
	delete(h.subs, s)
	s.once.Do(func() {
		close(s.gone)
		_ = s.conn.SetDeadline(time.Now())
	})
}

// keepalive sends a record when nothing else has, so a subscriber can tell a quiet
// feed from a dead link.
func (h *FeedHub) keepalive() {
	t := time.NewTicker(h.cfg.Keepalive / 2) // [wall]
	defer t.Stop()
	for {
		select {
		case <-h.stop:
			return
		case <-t.C:
		}
		h.mu.Lock()
		quiet := time.Since(h.last) >= h.cfg.Keepalive // [wall]
		h.mu.Unlock()
		if quiet {
			h.Publish(feedRecord(FeedKeep))
		}
	}
}

// feedSub is one subscriber: its link and the records it is owed.
type feedSub struct {
	conn net.Conn
	ch   chan []byte
	gone chan struct{}
	once sync.Once
}

// write sends a subscriber its records, small ones gathered into one write.
func (s *feedSub) write(h *FeedHub) {
	defer func() {
		h.drop(s)
		_ = s.conn.Close()
	}()
	var buf []byte
	for {
		var rec []byte
		var ok bool
		select {
		case rec, ok = <-s.ch:
		case <-s.gone:
			return
		}
		if !ok {
			return
		}
		out := rec
		if len(rec) < feedGather {
			buf = append(buf[:0], rec...)
		gather:
			for len(buf) < feedGather {
				select {
				case more, ok := <-s.ch:
					if !ok {
						break gather
					}
					buf = append(buf, more...)
				default:
					break gather
				}
			}
			out = buf
		}
		_ = s.conn.SetWriteDeadline(time.Now().Add(h.cfg.WriteTimeout))
		if err := writeFull(s.conn, out); err != nil {
			return
		}
		if cap(buf) > 4*feedGather {
			buf = nil // a starting point passed through it
		}
	}
}

// watch drops a subscriber that closes its end, or that sends: a feed runs one way.
func (s *feedSub) watch(h *FeedHub) {
	_ = s.conn.SetReadDeadline(time.Time{})
	var b [1]byte
	_, _ = s.conn.Read(b[:])
	h.drop(s)
}

// FeedReader reads a feed's records. A hub copying the feed on publishes each as it
// came; only a subscriber's own port has data records inflated.
type FeedReader struct {
	br   *bufio.Reader
	head int

	src   feedSource
	z     io.Reader
	plain []byte
	tick  uint64 // the last tick marked
}

func NewFeedReader(r io.Reader, cfg FeedConfig) *FeedReader {
	f := &FeedReader{br: bufio.NewReaderSize(r, 16<<10), head: cfg.Head}
	f.z = flate.NewReader(&f.src)
	return f
}

// Next reads one whole record, its kind byte first.
func (r *FeedReader) Next() ([]byte, error) {
	kind, err := r.br.ReadByte()
	if err != nil {
		return nil, err
	}
	n, err := binary.ReadUvarint(r.br)
	if err != nil {
		return nil, err
	}
	limit := feedDataMax + 2*binary.MaxVarintLen64
	switch kind {
	case FeedSync:
		limit = r.head
	case FeedData, FeedCut, FeedEnd, FeedKeep:
	default:
		return nil, fmt.Errorf("feed record of kind %d", kind)
	}
	if n > uint64(limit) {
		return nil, fmt.Errorf("feed record of kind %d and %d bytes exceeds %d", kind, n, limit)
	}
	rec := binary.AppendUvarint(append(make([]byte, 0, 1+binary.MaxVarintLen64+int(n)), kind), n)
	body := len(rec)
	rec = rec[:body+int(n)]
	if _, err := io.ReadFull(r.br, rec[body:]); err != nil {
		return nil, err
	}
	return rec, nil
}

// cut restarts the inflater and takes the tick the stream stands at, which only a
// newcomer's first cut and a new game's move.
func (r *FeedReader) cut(rec []byte) error {
	tick, n := binary.Uvarint(FeedBody(rec))
	if n <= 0 {
		return errors.New("feed cut names no tick")
	}
	r.tick = tick
	r.src = feedSource{}
	return r.z.(flate.Resetter).Reset(&r.src, nil)
}

// data inflates one data record and calls frame and mark in the writer's order; a
// frame's payload is the reader's until the call returns.
func (r *FeedReader) data(rec []byte, frame func(MessageType, []byte), mark func(uint64)) error {
	body := FeedBody(rec)
	first, n := binary.Uvarint(body)
	plain, m := binary.Uvarint(body[max(n, 0):])
	if n <= 0 || m <= 0 || plain == 0 || plain > feedDataMax {
		return errors.New("feed data record malformed")
	}
	if first != r.tick+1 {
		return fmt.Errorf("feed skipped from tick %d to %d", r.tick, first)
	}
	r.src.add(body[n+m:])
	if uint64(cap(r.plain)) < plain {
		r.plain = make([]byte, plain)
	}
	items := r.plain[:plain]
	if _, err := io.ReadFull(r.z, items); err != nil {
		return fmt.Errorf("feed data record: %w", err)
	}
	for len(items) > 0 {
		switch items[0] {
		case feedMark:
			r.tick++
			mark(r.tick)
			items = items[1:]
		case feedFrame:
			if len(items) < 3 {
				return errors.New("feed frame truncated")
			}
			size, k := binary.Uvarint(items[2:])
			if k <= 0 || size > uint64(len(items)-2-k) {
				return errors.New("feed frame truncated")
			}
			end := 2 + k + int(size)
			frame(MessageType(items[1]), items[2+k:end])
			items = items[end:]
		default:
			return fmt.Errorf("feed item of kind %d", items[0])
		}
	}
	return nil
}

// feedSource is what a feed's inflater reads: each data record's deflate after the
// last's. It is a byte reader, so flate adds no buffer that would hold a record back.
type feedSource struct {
	buf []byte
	off int
}

func (s *feedSource) add(p []byte) {
	s.buf = append(s.buf[:copy(s.buf, s.buf[s.off:])], p...)
	s.off = 0
}

func (s *feedSource) ReadByte() (byte, error) {
	if s.off == len(s.buf) {
		return 0, io.EOF
	}
	s.off++
	return s.buf[s.off-1], nil
}

func (s *feedSource) Read(p []byte) (int, error) {
	if s.off == len(s.buf) {
		return 0, io.EOF
	}
	n := copy(p, s.buf[s.off:])
	s.off += n
	return n, nil
}

// FeedPort is a feed's receiving end as a session port, for a run that begins at a
// starting point and follows the feed tick for tick. It sends nothing, and releases a
// tick's frames to Drain once Ready was asked for that tick and its mark has arrived.
type FeedPort struct {
	conn    net.Conn
	kinds   Kinds
	backlog int

	mu      sync.Mutex
	peer    PeerID     // whom the frames are from, to the run
	head    []byte     // the newest starting point not yet taken
	begun   bool       // a starting point has come
	taken   bool       // the run took one: ticks are released
	ticks   []feedTick // whole ticks not yet released, the oldest first
	open    Bundle     // the frames of the tick whose mark has not come
	out     []Message  // released, awaiting Drain
	granted uint64     // the tick Ready last released
	err     error

	heads  chan struct{}
	done   chan struct{}
	closed atomic.Bool
}

type feedTick struct {
	tick   uint64
	frames []Message
}

// NewFeedPort reads the feed conn carries, which it owns; kinds says which frames
// reach the run bundled.
func NewFeedPort(conn net.Conn, kinds Kinds, cfg FeedConfig) *FeedPort {
	p := &FeedPort{
		conn: conn, kinds: kinds, backlog: cfg.Backlog,
		heads: make(chan struct{}, 1), done: make(chan struct{}),
	}
	go p.read(NewFeedReader(conn, cfg), cfg.Silence)
	return p
}

func (p *FeedPort) read(r *FeedReader, silence time.Duration) {
	defer close(p.done)
	for {
		if silence > 0 {
			_ = p.conn.SetReadDeadline(time.Now().Add(silence))
		}
		rec, err := r.Next()
		if err == nil {
			err = p.take(r, rec)
		}
		if err != nil {
			// The first failure stands: a run that lost step reads as that, not as
			// the read it interrupted.
			p.mu.Lock()
			p.err = cmp.Or(p.err, err)
			p.mu.Unlock()
			return
		}
	}
}

func (p *FeedPort) take(r *FeedReader, rec []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	switch rec[0] {
	case FeedSync:
		p.begun, p.head = true, FeedBody(rec)
		select {
		case p.heads <- struct{}{}:
		default:
		}
	case FeedCut:
		return r.cut(rec)
	case FeedData:
		if !p.begun {
			return errors.New("feed data before a starting point")
		}
		if err := r.data(rec, p.frame, p.mark); err != nil {
			return err
		}
		if len(p.ticks) > p.backlog {
			return fmt.Errorf("the run fell %d ticks behind its feed", len(p.ticks))
		}
	case FeedEnd:
		return feedEnded(rec)
	}
	return nil
}

func (p *FeedPort) frame(kind MessageType, payload []byte) {
	if !p.kinds.Traits(kind).Bundles || 3+len(payload) > MaxPayloadSize {
		p.open.out = append(p.open.out, Message{Type: kind, Payload: bytes.Clone(payload)})
		return
	}
	p.open.Add(kind, payload)
}

func (p *FeedPort) mark(tick uint64) {
	p.ticks = append(p.ticks, feedTick{tick, p.open.Take()})
}

// Heads signals a starting point waiting to be taken.
func (p *FeedPort) Heads() <-chan struct{} { return p.heads }

// TakeHead is the newest starting point not yet taken, nil with none. No tick is
// released before the first was taken, which a run begins from; a later one, which
// may stand ahead of the run, is what it checks itself against.
func (p *FeedPort) TakeHead() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	head := p.head
	p.head, p.taken = nil, p.taken || head != nil
	return head
}

// Attribute names the peer the feed's frames reach the run from.
func (p *FeedPort) Attribute(peer uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.peer = PeerID(peer)
}

// Inject hands the run a frame ahead of the feed's own: what a starting point
// carried beside its state, which the run takes before its first tick.
func (p *FeedPort) Inject(_ uint32, kind uint8, payload []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.out = append(p.out, Message{Type: MessageType(kind), Payload: payload})
}

// Ready reports whether tick may run: its mark has arrived, and what the feed
// carried for it is released to Drain. A run at another tick than the feed's next
// has lost step, which ends the port.
func (p *FeedPort) Ready(tick uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	waits := p.taken && p.err == nil && len(p.ticks) != 0
	switch {
	case waits && p.ticks[0].tick == tick: // first: a new game's may bear the number last released
		p.out = append(p.out, p.ticks[0].frames...)
		p.ticks, p.granted = p.ticks[1:], tick
		return true
	case tick == p.granted:
		return true
	case waits:
		p.err = fmt.Errorf("the run is at tick %d and its feed at %d", tick, p.ticks[0].tick)
		_ = p.conn.SetReadDeadline(time.Now()) // wakes the reader, which ends the port
	}
	return false
}

// Backlog is how many whole ticks wait past the last one released.
func (p *FeedPort) Backlog() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.ticks)
}

// Drain hands the run the frames released to it.
func (p *FeedPort) Drain(dst []Inbound) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := min(len(dst), len(p.out))
	for i := range n {
		msg := p.out[i]
		dst[i] = Inbound{Kind: InboundMessage, Peer: p.peer, Msg: &msg}
	}
	if p.out = p.out[n:]; len(p.out) == 0 {
		p.out = nil
	}
	return n
}

// A feed carries nothing back, so every send is refused.
func (p *FeedPort) Send(uint32, uint8, []byte) bool       { return false }
func (p *FeedPort) Broadcast(uint8, []byte)               {}
func (p *FeedPort) BroadcastExcept(uint32, uint8, []byte) {}

// PeerCount is the feed's origin until the port is closed: a feed that was lost is
// the run's to act on, not a peer that left.
func (p *FeedPort) PeerCount() int {
	if p.closed.Load() {
		return 0
	}
	return 1
}

func (p *FeedPort) IsRunning() bool { return !p.closed.Load() }

// Done is closed when the feed ended or was lost; Err says which.
func (p *FeedPort) Done() <-chan struct{} { return p.done }

func (p *FeedPort) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *FeedPort) Close() error {
	p.closed.Store(true)
	return p.conn.Close()
}
