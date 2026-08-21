package status

import (
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// SubRec is the vlog subsystem tag on every flight-recorder record.
//
// Flush encoding: one record per metric group per window. A metric whose value
// never changed is emitted as a scalar of its native type; a metric that varied
// is emitted as a string — comma-separated for ints, floats and strings, a run
// of 0/1 digits for bools. t0 names the first tick and n the sample count, so
// position k in a series belongs to tick t0+k.
const SubRec = "rec"

// Trigger reasons
const (
	TrigDrop   = "event.dropped"
	TrigLock   = "lock.hold"
	TrigRace   = "race"
	TrigCrash  = "crash"
	TrigManual = "manual"
	TrigBreak  = "break"
)

// active is the process-wide recorder, so triggers reach it from packages
// holding no Registry reference: the update mutex, the stderr drain, the crash
// hook. One registry exists per process.
var active atomic.Pointer[Recorder]

// Trigger requests a flush from any goroutine; the tick goroutine performs it
func Trigger(reason string) {
	if rc := active.Load(); rc != nil {
		rc.Trigger(reason)
	}
}

// TriggerFSM requests a flush for an FSM transition; disabled by default,
// since transitions are frequent enough to flood on their own
func TriggerFSM(region string) {
	rc := active.Load()
	if rc == nil || !rc.trigFSM.Load() {
		return
	}
	rc.Trigger("fsm:" + region)
}

// RecorderActive reports whether a recorder is sampling
func RecorderActive() bool { return active.Load() != nil }

// CrashFlush drains the window synchronously from the crash path
func CrashFlush() {
	if rc := active.Load(); rc != nil {
		rc.CrashFlush()
	}
}

// recCol binds one metric's short name to its slot index within its kind plane
type recCol struct {
	name string
	idx  int
	kind uint8
}

// recGroup mirrors a statGroup in the recorder's flat layout
type recGroup struct {
	name    string
	cols    []recCol
	visible int // Entity column for a roster slot; -1 for always-visible groups
}

// Recorder is a fixed-depth ring of full registry snapshots, sampled every
// game tick and written only on a trigger. Storage is slot-major: one tick's
// values are contiguous, so the per-tick write is a linear walk.
type Recorder struct {
	reg   *Registry
	depth int

	// Source pointers, flattened in column order per kind
	srcI []*atomic.Int64
	srcF []*AtomicFloat
	srcB []*atomic.Bool
	srcS []*AtomicString

	// Ring planes; bufB is a bw-word bitset per slot
	bufI []int64
	bufF []float64
	bufB []uint64
	bufS []string
	tick []uint64
	bw   int

	groups []recGroup

	// head counts samples written; the newest occupies (head-1) % depth.
	// Stored after the slot's writes, so a reader never sees a torn slot.
	head atomic.Uint64

	pending  atomic.Bool
	reason   atomic.Pointer[string]
	flushing atomic.Bool
	trigFSM  atomic.Bool
	lastPath atomic.Pointer[string]

	lastFlush uint64 // tick goroutine only
	minGap    uint64

	slots   []int  // flush scratch: window slots, oldest first
	scratch []byte // flush scratch: series formatting

	statFlushes *atomic.Int64
	statRecords *atomic.Int64
	statSkipped *atomic.Int64
	statDepth   *atomic.Int64
}

// newRecorder allocates an unbound recorder; bind lays out the ring
func newRecorder(r *Registry, depth int) *Recorder {
	rc := &Recorder{
		reg:         r,
		depth:       depth,
		minGap:      uint64(depth / 4),
		statFlushes: r.Ints.Get("rec.flushes"),
		statRecords: r.Ints.Get("rec.records"),
		statSkipped: r.Ints.Get("rec.skipped"),
		statDepth:   r.Ints.Get("rec.depth"),
	}
	rc.statDepth.Store(int64(depth))
	return rc
}

// bind lays out the ring from the frozen group index. History is discarded.
func (rc *Recorder) bind(groups []statGroup) {
	rc.srcI, rc.srcF, rc.srcB, rc.srcS = nil, nil, nil, nil
	rc.groups = make([]recGroup, 0, len(groups))

	for gi := range groups {
		g := &groups[gi]
		rg := recGroup{name: g.name, cols: make([]recCol, 0, len(g.members)), visible: -1}
		for i := range g.members {
			m := &g.members[i]
			c := recCol{name: m.name, kind: m.kind}
			switch m.kind {
			case kindInt:
				c.idx = len(rc.srcI)
				rc.srcI = append(rc.srcI, m.i)
			case kindFloat:
				c.idx = len(rc.srcF)
				rc.srcF = append(rc.srcF, m.f)
			case kindBool:
				c.idx = len(rc.srcB)
				rc.srcB = append(rc.srcB, m.b)
			case kindString:
				c.idx = len(rc.srcS)
				rc.srcS = append(rc.srcS, m.s)
			}
			rg.cols = append(rg.cols, c)
		}
		rc.groups = append(rc.groups, rg)
	}
	for gi := range groups {
		if groups[gi].visible == nil {
			continue
		}
		for i, source := range rc.srcI {
			if source == groups[gi].visible {
				rc.groups[gi].visible = i
				break
			}
		}
	}

	rc.bw = (len(rc.srcB) + 63) / 64
	rc.bufI = make([]int64, len(rc.srcI)*rc.depth)
	rc.bufF = make([]float64, len(rc.srcF)*rc.depth)
	rc.bufB = make([]uint64, rc.bw*rc.depth)
	rc.bufS = make([]string, len(rc.srcS)*rc.depth)
	rc.tick = make([]uint64, rc.depth)
	rc.slots = make([]int, 0, rc.depth)
	rc.head.Store(0)
}

// sample records one tick. Allocation-free: AtomicString.Load returns an
// existing header, so storing it retains rather than copies.
func (rc *Recorder) sample(t uint64) {
	if rc.tick == nil {
		return // Freeze has not run yet
	}
	seq := rc.head.Load()
	slot := int(seq % uint64(rc.depth))

	rc.tick[slot] = t

	if n := len(rc.srcI); n > 0 {
		base := slot * n
		for i, p := range rc.srcI {
			rc.bufI[base+i] = p.Load()
		}
	}
	if n := len(rc.srcF); n > 0 {
		base := slot * n
		for i, p := range rc.srcF {
			rc.bufF[base+i] = p.Get()
		}
	}
	if n := len(rc.srcS); n > 0 {
		base := slot * n
		for i, p := range rc.srcS {
			rc.bufS[base+i] = p.Load()
		}
	}
	if rc.bw > 0 {
		base := slot * rc.bw
		for i := range rc.bufB[base : base+rc.bw] {
			rc.bufB[base+i] = 0
		}
		for i, p := range rc.srcB {
			if p.Load() {
				rc.bufB[base+i>>6] |= 1 << uint(i&63)
			}
		}
	}

	rc.head.Store(seq + 1)
}

// Trigger requests a flush; the newest reason wins and repeats collapse
func (rc *Recorder) Trigger(reason string) {
	rc.reason.Store(&reason)
	rc.pending.Store(true)
}

// SetFSMTrigger enables flushing on FSM state transitions
func (rc *Recorder) SetFSMTrigger(on bool) { rc.trigFSM.Store(on) }

// FSMTrigger reports whether transitions trigger a flush
func (rc *Recorder) FSMTrigger() bool { return rc.trigFSM.Load() }

// LastPath returns the last standalone file written, empty when the session
// log absorbed every flush
func (rc *Recorder) LastPath() string {
	if p := rc.lastPath.Load(); p != nil {
		return *p
	}
	return ""
}

// drain performs a requested flush, throttled so a repeating fault cannot
// flood the log. Runs on the tick goroutine, off the world lock.
func (rc *Recorder) drain(t uint64) {
	if !rc.pending.Swap(false) {
		return
	}
	if rc.lastFlush != 0 && t-rc.lastFlush < rc.minGap {
		rc.statSkipped.Add(1)
		return
	}
	rc.lastFlush = t
	rc.Flush(rc.pendingReason())
}

func (rc *Recorder) pendingReason() string {
	if p := rc.reason.Load(); p != nil {
		return *p
	}
	return TrigManual
}

// CrashFlush drains from the panic path. It is a no-op without a session log:
// opening files during a panic is worse than losing the window.
func (rc *Recorder) CrashFlush() {
	if !vlog.Enabled() {
		return
	}
	rc.Flush(TrigCrash)
}

// Flush writes the current window. Safe from any goroutine, but a concurrent
// sample may overwrite the oldest slot mid-read; the tick stamps in the window
// header make that visible rather than silent.
func (rc *Recorder) Flush(reason string) {
	if rc.tick == nil || !rc.flushing.CompareAndSwap(false, true) {
		return
	}
	defer rc.flushing.Store(false)

	// No sink and no directory to open one: the novlog/wasm stub, or an
	// embedder that never configured vlog. A window cannot reach disk, so
	// walking it and counting a flush would report work that did not happen.
	if !vlog.Enabled() && vlog.Dir() == "" {
		return
	}

	// EmitSet discards the whole set when scope or level suppresses it; walking
	// the window and counting a flush that wrote nothing is a false success
	if vlog.Enabled() && !vlog.On(SubRec, vlog.LevelInfo) {
		rc.statSkipped.Add(1)
		return
	}

	head := rc.head.Load()
	if head == 0 {
		return
	}
	n := rc.depth
	if head < uint64(n) {
		n = int(head)
	}
	oldest := head - uint64(n)

	rc.slots = rc.slots[:0]
	for k := range n {
		rc.slots = append(rc.slots, int((oldest+uint64(k))%uint64(rc.depth)))
	}
	t0, t1 := rc.tick[rc.slots[0]], rc.tick[rc.slots[n-1]]
	visibleGroups := 0
	for i := range rc.groups {
		if rc.groupVisible(&rc.groups[i], n) {
			visibleGroups++
		}
	}

	start := time.Now()
	records := 0
	run, tick, frame := vlog.Stamp()

	path, err := vlog.EmitSet(SubRec, run, tick, frame, func(emit func(args ...any)) {
		emit("msg", "window", "reason", reason,
			"t0", t0, "t1", t1, "n", n, "groups", visibleGroups)
		records++
		for gi := range rc.groups {
			g := &rc.groups[gi]
			if !rc.groupVisible(g, n) {
				continue
			}
			args := make([]any, 0, 6+2*len(g.cols))
			args = append(args, "msg", g.name, "t0", t0, "n", n)
			for ci := range g.cols {
				args = append(args, g.cols[ci].name, rc.series(&g.cols[ci], n))
			}
			emit(args...)
			records++
		}
	})

	rc.statFlushes.Add(1)
	rc.statRecords.Store(int64(records))
	if path != "" {
		rc.lastPath.Store(&path)
	}

	// Breadcrumb lands in the session log; a standalone file has nothing to
	// correlate against and reports through :log rec instead
	if err != nil {
		vlog.Error("app", "msg", "recorder flush failed", "reason", reason, "error", err.Error())
		return
	}
	if path == "" {
		vlog.Info("app", "msg", "recorder flush",
			"reason", reason, "t0", t0, "ticks", n,
			"records", records, "us", time.Since(start).Microseconds())
	}
}

// groupVisible keeps a player group when its slot existed anywhere in the
// emitted recorder window; schema storage remains fixed for allocation-free sampling.
func (rc *Recorder) groupVisible(g *recGroup, n int) bool {
	if g.visible < 0 {
		return true
	}
	stride := len(rc.srcI)
	for k := range n {
		if rc.bufI[rc.slots[k]*stride+g.visible] != 0 {
			return true
		}
	}
	return false
}

// series returns a column's window: a native scalar when constant, otherwise
// the compact string form documented on SubRec
func (rc *Recorder) series(c *recCol, n int) any {
	switch c.kind {
	case kindInt:
		stride := len(rc.srcI)
		first := rc.bufI[rc.slots[0]*stride+c.idx]
		if rc.constInt(c.idx, stride, n, first) {
			return first
		}
		rc.scratch = rc.scratch[:0]
		for k := range n {
			if k > 0 {
				rc.scratch = append(rc.scratch, ',')
			}
			rc.scratch = strconv.AppendInt(rc.scratch, rc.bufI[rc.slots[k]*stride+c.idx], 10)
		}
		return string(rc.scratch)

	case kindFloat:
		stride := len(rc.srcF)
		first := rc.bufF[rc.slots[0]*stride+c.idx]
		if rc.constFloat(c.idx, stride, n, first) {
			return first
		}
		rc.scratch = rc.scratch[:0]
		for k := range n {
			if k > 0 {
				rc.scratch = append(rc.scratch, ',')
			}
			rc.scratch = strconv.AppendFloat(rc.scratch, rc.bufF[rc.slots[k]*stride+c.idx], 'g', -1, 64)
		}
		return string(rc.scratch)

	case kindBool:
		first := rc.boolAt(rc.slots[0], c.idx)
		if rc.constBool(c.idx, n, first) {
			return first
		}
		rc.scratch = rc.scratch[:0]
		for k := range n {
			b := byte('0')
			if rc.boolAt(rc.slots[k], c.idx) {
				b = '1'
			}
			rc.scratch = append(rc.scratch, b)
		}
		return string(rc.scratch)

	case kindString:
		stride := len(rc.srcS)
		first := rc.bufS[rc.slots[0]*stride+c.idx]
		if rc.constString(c.idx, stride, n, first) {
			// Escape here too: a constant carrying the separator is otherwise
			// indistinguishable from a series
			return strings.ReplaceAll(first, ",", ";")
		}
		rc.scratch = rc.scratch[:0]
		for k := range n {
			if k > 0 {
				rc.scratch = append(rc.scratch, ',')
			}
			// The separator is reserved; no status string carries a comma today
			rc.scratch = append(rc.scratch,
				strings.ReplaceAll(rc.bufS[rc.slots[k]*stride+c.idx], ",", ";")...)
		}
		return string(rc.scratch)
	}
	return nil
}

func (rc *Recorder) boolAt(slot, idx int) bool {
	return rc.bufB[slot*rc.bw+idx>>6]&(1<<uint(idx&63)) != 0
}

func (rc *Recorder) constInt(idx, stride, n int, v int64) bool {
	for k := 1; k < n; k++ {
		if rc.bufI[rc.slots[k]*stride+idx] != v {
			return false
		}
	}
	return true
}

func (rc *Recorder) constFloat(idx, stride, n int, v float64) bool {
	for k := 1; k < n; k++ {
		if rc.bufF[rc.slots[k]*stride+idx] != v {
			return false
		}
	}
	return true
}

func (rc *Recorder) constBool(idx, n int, v bool) bool {
	for k := 1; k < n; k++ {
		if rc.boolAt(rc.slots[k], idx) != v {
			return false
		}
	}
	return true
}

func (rc *Recorder) constString(idx, stride, n int, v string) bool {
	for k := 1; k < n; k++ {
		if rc.bufS[rc.slots[k]*stride+idx] != v {
			return false
		}
	}
	return true
}
