package logfile

import (
	"io"
	"os"
	"sync"
	"sync/atomic"
)

// Meta is one index row: 48 bytes, pointer-free, holds no line bytes.
type Meta struct {
	Off   int64
	TS    int64
	Len   uint32
	Run   uint32
	Tick  uint32
	Frame uint32
	Msg   uint32 // interned fields.msg
	Snap  uint32 // stat-snapshot group id, 0 = none
	Sub   uint16 // interned sub
	Src   uint16 // source file index
	Lvl   Level
	Flags uint8
}

// Meta flag bits.
const (
	FlagMalformed uint8 = 1 << iota
	FlagSnapHead
	FlagTrace
	FlagNoTime // TS was inherited from the previous line, for ordering only
)

// Snapshot groups the stat records sharing one (src, run, tick). Members are
// not contiguous in the merged view; Count is authoritative, Head locates the
// group's first row in the published order.
type Snapshot struct {
	Head  uint32
	Count uint32
	Run   uint32
	Tick  uint32
	Frame uint32
	Src   uint16
}

// Source is one indexed file.
type Source struct {
	Path string
	Name string
	Size int64

	scanned atomic.Int64
	bad     atomic.Int64
	done    atomic.Bool
	failure atomic.Pointer[scanErr]
}

// Index is the append-only line index over one or more files. The scanner
// publishes immutable slice headers; readers load them atomically, so the
// render path never locks. A single source publishes incrementally; several
// sources publish once, merged by timestamp, so row indices never shift.
type Index struct {
	srcs []*Source

	subN, msgN *interner

	metas atomic.Pointer[[]Meta]
	snaps atomic.Pointer[[]Snapshot]
	subs  atomic.Pointer[[]string]
	msgs  atomic.Pointer[[]string]
}

type scanErr struct{ err error }

// The accessors below tolerate a nil receiver: the viewer runs with no file
// open until one is chosen, and the render path reads the index every frame.

// Sources returns the indexed files in source-id order.
func (x *Index) Sources() []*Source {
	if x == nil {
		return nil
	}
	return x.srcs
}

// SrcCount returns the number of indexed files.
func (x *Index) SrcCount() int {
	if x == nil {
		return 0
	}
	return len(x.srcs)
}

// SrcName resolves a source id to its base filename.
func (x *Index) SrcName(id uint16) string {
	if x == nil || int(id) >= len(x.srcs) {
		return ""
	}
	return x.srcs[id].Name
}

// SrcMark returns the one-character gutter label for a source id.
func (x *Index) SrcMark(id uint16) rune {
	const marks = "123456789abcdefghijklmnopqrstuvwxyz"
	if int(id) >= len(marks) {
		return '?'
	}
	return rune(marks[id])
}

// Metas returns the currently published index rows.
func (x *Index) Metas() []Meta {
	if x == nil {
		return nil
	}
	if p := x.metas.Load(); p != nil {
		return *p
	}
	return nil
}

// Len returns the number of indexed records.
func (x *Index) Len() int { return len(x.Metas()) }

// Snaps returns the published snapshot groups.
func (x *Index) Snaps() []Snapshot {
	if x == nil {
		return nil
	}
	if p := x.snaps.Load(); p != nil {
		return *p
	}
	return nil
}

// Subs returns the interned subsystem table.
func (x *Index) Subs() []string {
	if x == nil {
		return nil
	}
	return strTable(x.subs.Load())
}

// Msgs returns the interned fields.msg table.
func (x *Index) Msgs() []string {
	if x == nil {
		return nil
	}
	return strTable(x.msgs.Load())
}

// SubName resolves an interned sub id.
func (x *Index) SubName(id uint16) string {
	if x == nil {
		return ""
	}
	return lookup(x.subs.Load(), int(id))
}

// MsgName resolves an interned fields.msg id.
func (x *Index) MsgName(id uint32) string {
	if x == nil {
		return ""
	}
	return lookup(x.msgs.Load(), int(id))
}

func strTable(p *[]string) []string {
	if p == nil {
		return nil
	}
	return *p
}

func lookup(p *[]string, i int) string {
	t := strTable(p)
	if i < 0 || i >= len(t) {
		return ""
	}
	return t[i]
}

// Progress reports bytes scanned, total bytes and completion across all sources.
func (x *Index) Progress() (scanned, total int64, complete bool) {
	if x == nil {
		return 0, 0, true
	}
	complete = true
	for _, s := range x.srcs {
		scanned += s.scanned.Load()
		total += s.Size
		if !s.done.Load() {
			complete = false
		}
	}
	return scanned, total, complete
}

// Malformed returns the count of unparseable lines seen so far.
func (x *Index) Malformed() int64 {
	if x == nil {
		return 0
	}
	var n int64
	for _, s := range x.srcs {
		n += s.bad.Load()
	}
	return n
}

// Err returns the first scan failure, if any.
func (x *Index) Err() error {
	if x == nil {
		return nil
	}
	for _, s := range x.srcs {
		if p := s.failure.Load(); p != nil {
			return p.err
		}
	}
	return nil
}

// SnapshotOf returns the stat group a record belongs to. The group currently
// being scanned is not published, so ok=false until it closes.
func (x *Index) SnapshotOf(m Meta) (Snapshot, bool) {
	if x == nil || m.Snap == 0 {
		return Snapshot{}, false
	}
	s := x.Snaps()
	if int(m.Snap) > len(s) {
		return Snapshot{}, false
	}
	return s[m.Snap-1], true
}

const readWindow = 256 << 10

// Reader fetches raw line bytes through a per-source sliding window.
// Not safe for concurrent use; create one per goroutine.
type Reader struct {
	paths []string
	win   []window
}

type window struct {
	f    *os.File
	buf  []byte
	base int64
	n    int
}

// NewReader opens an independent handle set on the indexed files. Handles are
// opened lazily, so a reader that only touches one source costs one fd.
func (x *Index) NewReader() (*Reader, error) {
	r := &Reader{
		paths: make([]string, len(x.srcs)),
		win:   make([]window, len(x.srcs)),
	}
	for i, s := range x.srcs {
		r.paths[i] = s.Path
		r.win[i].base = -1
	}
	return r, nil
}

// Line returns the raw bytes of m, valid until the next call.
func (r *Reader) Line(m Meta) ([]byte, error) {
	n := int(m.Len)
	if n == 0 || int(m.Src) >= len(r.win) {
		return nil, nil
	}
	w := &r.win[m.Src]
	if w.f == nil {
		f, err := os.Open(r.paths[m.Src])
		if err != nil {
			return nil, err
		}
		w.f, w.buf, w.base = f, make([]byte, readWindow), -1
	}
	if n+4096 > len(w.buf) {
		w.buf = make([]byte, n+4096)
		w.base = -1
	}
	if w.base < 0 || m.Off < w.base || m.Off+int64(n) > w.base+int64(w.n) {
		base := m.Off &^ 4095
		got, err := w.f.ReadAt(w.buf, base)
		if got == 0 && err != nil {
			return nil, err
		}
		w.base, w.n = base, got
	}
	s := int(m.Off - w.base)
	if s < 0 || s+n > w.n {
		return nil, io.ErrUnexpectedEOF
	}
	return w.buf[s : s+n], nil
}

// Close releases every open file handle.
func (r *Reader) Close() error {
	var err error
	for i := range r.win {
		if r.win[i].f != nil {
			if e := r.win[i].f.Close(); e != nil && err == nil {
				err = e
			}
			r.win[i].f = nil
		}
	}
	return err
}

// interner maps byte tokens to dense ids. Writer-side only; the id→name table
// is published as an immutable snapshot. Locked: sources scan concurrently.
type interner struct {
	mu  sync.Mutex
	ids map[string]uint32
	tab []string
}

func newInterner() *interner {
	n := &interner{ids: make(map[string]uint32, 64)}
	n.intern(nil) // id 0 == ""
	return n
}

func (n *interner) intern(b []byte) uint32 {
	n.mu.Lock()
	defer n.mu.Unlock()
	if id, ok := n.ids[string(b)]; ok {
		return id
	}
	s := string(b)
	id := uint32(len(n.tab))
	n.tab = append(n.tab, s)
	n.ids[s] = id
	return id
}

// table returns a consistent header over the interned names.
func (n *interner) table() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.tab[:len(n.tab):len(n.tab)]
}
