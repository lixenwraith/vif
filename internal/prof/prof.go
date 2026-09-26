// Package prof measures where a run spends its time and resources: wall time per
// system, event handler, renderer and engine phase, process CPU, memory, GC and
// I/O, and pprof captures. Off, a timed call costs one atomic load; on, each
// window lands in the status registry's activity-gated prof and proc groups.
package prof

import (
	"context"
	"fmt"
	"runtime/metrics"
	"runtime/pprof"
	"runtime/trace"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/status"
)

// Kind classifies a timer; kindLabel prefixes it in rankings and pprof labels
type Kind uint8

const (
	KindCore   Kind = iota // engine phases
	KindSystem             // a system's Update
	KindEvents             // a system's event handlers
	KindRender             // one renderer
)

var kindLabel = [...]string{"core", "sys", "evt", "draw"}

// Phase is an engine span the profiler owns
type Phase uint8

const (
	PhaseTick      Phase = iota // tick body under the world lock
	PhaseTickWait               // acquiring the lock for it
	PhaseFSM                    // FSM update
	PhaseDispatch               // every dispatch pass, in a tick or between ticks
	PhaseSystems                // every system Update
	PhaseStatus                 // snapshot and recorder sampling
	PhaseFrame                  // one rendered frame, flush included
	PhaseFrameWait              // acquiring the lock for it
	PhaseRender                 // renderers under the lock
	PhaseFlush                  // terminal write outside the lock
	phaseCount
)

// phases is the per-phase table: the name behind prof.<name>_us, the phase whose
// calls it averages over, and whether it is a leaf ranked beside the modules
var phases = [phaseCount]struct {
	name string
	per  Phase
	leaf bool
}{
	PhaseTick:      {"tick", PhaseTick, false},
	PhaseTickWait:  {"tick_wait", PhaseTick, false},
	PhaseFSM:       {"fsm", PhaseTick, true},
	PhaseDispatch:  {"dispatch", PhaseTick, false},
	PhaseSystems:   {"systems", PhaseTick, false},
	PhaseStatus:    {"status", PhaseTick, true},
	PhaseFrame:     {"frame", PhaseFrame, false},
	PhaseFrameWait: {"frame_wait", PhaseFrame, false},
	PhaseRender:    {"render", PhaseFrame, false},
	PhaseFlush:     {"flush", PhaseFrame, true},
}

// Timer accumulates one module's calls within the open window
type Timer struct {
	label  string            // "<kind> <name>"
	leaf   bool              // ranked, and labels CPU samples while a capture runs
	labels context.Context   // pprof labels applied while a CPU capture runs
	region *trace.Region     // open while an execution trace runs; one goroutine at a time
	heap   [1]metrics.Sample // the process's allocation counter, read around a leaf call

	ns, calls, max, bytes atomic.Int64
}

func newTimer(kind Kind, name string, leaf bool) *Timer {
	t := &Timer{
		label:  kindLabel[kind] + " " + name,
		leaf:   leaf,
		labels: pprof.WithLabels(context.Background(), pprof.Labels("kind", kindLabel[kind], "module", name)),
	}
	t.heap[0].Name = "/gc/heap/allocs:bytes"
	return t
}

// allocated reads the process-wide allocation counter. It advances a span of
// small objects at a time and counts every goroutine, so per-call deltas only
// estimate a module's allocation; about 400 ns, paid only while profiling.
func (t *Timer) allocated() uint64 {
	metrics.Read(t.heap[:])
	return t.heap[0].Value.Uint64()
}

// Span is one timed call in flight; the zero Span is a call nobody measures
type Span struct {
	t     *Timer
	start int64
	heap  uint64 // allocation counter at Begin; zero when allocation is not read
}

// End closes the call and folds it into its timer
func (s Span) End() {
	if s.start == 0 {
		return
	}
	d := now() - s.start
	t := s.t
	if s.heap != 0 {
		t.bytes.Add(int64(t.allocated() - s.heap))
	}
	t.ns.Add(d)
	t.calls.Add(1)
	for m := t.max.Load(); d > m && !t.max.CompareAndSwap(m, d); m = t.max.Load() {
	}
	if t.region != nil {
		t.region.End()
		t.region = nil
	}
	if t.leaf && labelling.Load() {
		pprof.SetGoroutineLabels(context.Background())
	}
}

var epoch = time.Now()

// now is the profiler's monotonic clock in nanoseconds, never zero
func now() int64 { return max(int64(time.Since(epoch)), 1) }

// ModuleStat is one leaf timer's cost over a closed window
type ModuleStat struct {
	Label       string
	Share       float64 // percent of the window's wall time
	Avg, Max    time.Duration
	PerSec      float64
	AllocPerSec float64 // bytes, estimated
}

func (m ModuleStat) String() string {
	return m.Label + " " + formatDur(m.Avg) + " " + strconv.FormatFloat(m.Share, 'f', 1, 64) + "%"
}

// Detail is the report's line for the module
func (m ModuleStat) Detail() string {
	return fmt.Sprintf("%.1f%%  avg %s  max %s  %.0f/s  ~%.1f KiB/s alloc",
		m.Share, formatDur(m.Avg), formatDur(m.Max), m.PerSec, m.AllocPerSec/1024)
}

// Profiler times one world's modules and publishes into its status registry
type Profiler struct {
	on    atomic.Bool
	start atomic.Int64 // open window's start, now() scale

	mu     sync.Mutex
	timers []*Timer
	phase  [phaseCount]*Timer
	report atomic.Pointer[[]ModuleStat]

	phaseUS  [phaseCount]*atomic.Int64
	tickMax  *atomic.Int64
	frameMax *atomic.Int64
	top      [parameter.ProfTopN]*status.AtomicString
	proc     process
}

// New registers the prof and proc groups; call before the registry freezes
func New(reg *status.Registry) *Profiler {
	p := &Profiler{
		tickMax:  reg.Ints.Get("prof.tick_max_us"),
		frameMax: reg.Ints.Get("prof.frame_max_us"),
	}
	for i := range phases {
		p.phase[i] = newTimer(KindCore, phases[i].name, phases[i].leaf)
		p.phaseUS[i] = reg.Ints.Get("prof." + phases[i].name + "_us")
	}
	for i := range p.top {
		p.top[i] = reg.Strings.Get(fmt.Sprintf("prof.top.%02d", i+1))
	}
	p.proc.register(reg)
	return p
}

// Timer registers one module; nil on a nil profiler, which times nothing
func (p *Profiler) Timer(kind Kind, name string) *Timer {
	if p == nil {
		return nil
	}
	t := newTimer(kind, name, true)
	p.mu.Lock()
	p.timers = append(p.timers, t)
	p.mu.Unlock()
	return t
}

// On reports whether timed calls are measured: profiling, or a capture that
// labels them with the module that ran
func (p *Profiler) On() bool {
	return p != nil && (p.on.Load() || labelling.Load() || trace.IsEnabled())
}

// Profiling reports whether windows are being published
func (p *Profiler) Profiling() bool { return p != nil && p.on.Load() }

// Begin opens a timed call on t
func (p *Profiler) Begin(t *Timer) Span {
	if t == nil || !p.On() {
		return Span{}
	}
	if t.leaf && labelling.Load() {
		pprof.SetGoroutineLabels(t.labels)
	}
	if trace.IsEnabled() {
		t.region = trace.StartRegion(context.Background(), t.label)
	}
	var heap uint64
	if t.leaf && p.on.Load() {
		heap = t.allocated()
	}
	return Span{t: t, start: now(), heap: heap}
}

// BeginPhase opens a timed engine phase
func (p *Profiler) BeginPhase(ph Phase) Span {
	if p == nil {
		return Span{}
	}
	return p.Begin(p.phase[ph])
}

// SetOn starts or stops publishing; a stop clears every cell so the cards hide
func (p *Profiler) SetOn(on bool) {
	if p == nil || p.on.Swap(on) == on {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.drainAll()
	if !on {
		p.clear()
		return
	}
	p.start.Store(now())
	p.proc.baseline()
}

// Hold restarts the window while the simulation is paused, so a window only
// ever spans a running game and the last live one stays on show
func (p *Profiler) Hold() {
	if !p.Profiling() {
		return
	}
	p.mu.Lock()
	p.drainAll()
	p.start.Store(now())
	p.proc.baseline()
	p.mu.Unlock()
}

// Publish closes the window once it is due. Called once per tick, off the world lock.
func (p *Profiler) Publish() {
	if !p.Profiling() {
		return
	}
	end, start := now(), p.start.Load()
	span := end - start
	if span < int64(parameter.ProfWindow) || !p.start.CompareAndSwap(start, end) {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	var ph [phaseCount]window
	for i := range p.phase {
		ph[i] = p.phase[i].drain()
	}
	for i := range phases {
		p.phaseUS[i].Store(ph[i].ns / max(ph[phases[i].per].calls, 1) / 1000)
	}
	p.tickMax.Store(ph[PhaseTick].max / 1000)
	p.frameMax.Store(ph[PhaseFrame].max / 1000)

	stats := make([]ModuleStat, 0, len(p.timers)+int(phaseCount))
	add := func(t *Timer, w window) {
		if w.calls != 0 {
			stats = append(stats, w.stat(t.label, span))
		}
	}
	for i := range p.phase {
		if phases[i].leaf {
			add(p.phase[i], ph[i])
		}
	}
	for _, t := range p.timers {
		add(t, t.drain())
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].Share > stats[j].Share })
	for i := range p.top {
		if i < len(stats) {
			p.top[i].Store(stats[i].String())
		} else {
			p.top[i].Store("")
		}
	}
	p.report.Store(&stats)
	p.proc.sample(span)
}

// Report returns the last closed window ranked by share; nil while off
func (p *Profiler) Report() []ModuleStat {
	if !p.Profiling() {
		return nil
	}
	if r := p.report.Load(); r != nil {
		return *r
	}
	return nil
}

// window is one timer's drained accumulation
type window struct{ ns, calls, max, bytes int64 }

func (t *Timer) drain() window {
	return window{ns: t.ns.Swap(0), calls: t.calls.Swap(0), max: t.max.Swap(0), bytes: t.bytes.Swap(0)}
}

func (w window) stat(label string, span int64) ModuleStat {
	perSec := float64(time.Second) / float64(span)
	return ModuleStat{
		Label:       label,
		Share:       float64(w.ns) * 100 / float64(span),
		Avg:         time.Duration(w.ns / w.calls),
		Max:         time.Duration(w.max),
		PerSec:      float64(w.calls) * perSec,
		AllocPerSec: float64(w.bytes) * perSec,
	}
}

// drainAll discards every open accumulation; caller holds mu
func (p *Profiler) drainAll() {
	for _, t := range p.phase {
		t.drain()
	}
	for _, t := range p.timers {
		t.drain()
	}
}

// clear zeroes every published cell; caller holds mu
func (p *Profiler) clear() {
	for _, c := range p.phaseUS {
		c.Store(0)
	}
	p.tickMax.Store(0)
	p.frameMax.Store(0)
	for _, c := range p.top {
		c.Store("")
	}
	p.report.Store(nil)
	p.proc.clear()
}

// formatDur fits a module's mean into a HUD column
func formatDur(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return strconv.FormatInt(d.Microseconds(), 10) + "µs"
	case d < time.Second:
		return strconv.FormatFloat(float64(d)/float64(time.Millisecond), 'f', 2, 64) + "ms"
	default:
		return strconv.FormatFloat(d.Seconds(), 'f', 2, 64) + "s"
	}
}
