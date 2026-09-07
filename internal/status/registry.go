package status

import (
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// Registry is the central metrics facade.
// Systems cache pointers during construction; Update loops write directly to atomics.
// Also owns periodic snapshot emission (snapshot.go) and the flight recorder
// (recorder.go), which share one group index
type Registry struct {
	Bools   *MetricMap[atomic.Bool]
	Ints    *MetricMap[atomic.Int64]
	Floats  *MetricMap[AtomicFloat]
	Strings *MetricMap[AtomicString]

	// Snapshot state
	snapEvery atomic.Uint64
	idxMu     sync.Mutex
	idxGen    uint64
	idx       []statGroup

	// Post-Freeze the index is immutable, so the tick path loads it without
	// a generation probe or a lock
	frozen  atomic.Bool
	idxFast atomic.Pointer[[]statGroup]

	rec      atomic.Pointer[Recorder]
	statLate *atomic.Int64
	corr     *vlog.Correlation

	// The roster slot the bare per-player keys mirror, and the metrics that hold
	// one. Registration happens before Freeze like every other metric; the slot
	// moves whenever the roster rebinds, which is a tick-path write of one int.
	slot     atomic.Int32
	playerMu sync.Mutex
	players  []playerMirror
}

// playerMirror is a per-slot metric that also publishes a bare key. It exists so a
// rebind can republish, rather than leaving the bare key on the previous holder
// until that cursor happens to write again.
type playerMirror interface{ remirror(slot int32) }

// NewRegistry creates an initialized Registry with snapshots disabled
func NewRegistry() *Registry {
	r := &Registry{
		Bools:   NewMetricMap[atomic.Bool](),
		Ints:    NewMetricMap[atomic.Int64](),
		Floats:  NewMetricMap[AtomicFloat](),
		Strings: NewMetricMap[AtomicString](),
		corr:    vlog.DefaultCorrelation(),
	}
	r.statLate = r.Ints.Get("stat.late")
	r.slot.Store(0)
	// Reserve the recorder's counters even when none is installed: EnableRecorder
	// can run after Freeze (":log rec N"), and detached cells would make the
	// recorder invisible to its own windows.
	for _, k := range []string{"rec.depth", "rec.flushes", "rec.records", "rec.skipped"} {
		r.Ints.Get(k)
	}
	return r
}

// localSlot exposes the mirrored slot cell to the per-player metrics.
func (r *Registry) localSlot() *atomic.Int32 { return &r.slot }

// trackPlayer records a metric that mirrors a bare key, for SetLocalSlot.
func (r *Registry) trackPlayer(m playerMirror) {
	r.playerMu.Lock()
	r.players = append(r.players, m)
	r.playerMu.Unlock()
}

// SetLocalSlot names the roster slot the bare per-player keys describe, and
// republishes them. A slot outside the roster — a participant that drives no
// cursor — mirrors nothing and the bare keys read as reset.
func (r *Registry) SetLocalSlot(slot int) {
	v := int32(slot)
	if v < 0 || v > 255 {
		v = -1
	}
	if r.slot.Swap(v) == v {
		return
	}
	r.playerMu.Lock()
	defer r.playerMu.Unlock()
	for _, m := range r.players {
		m.remirror(v)
	}
}

// SetCorrelation binds snapshots and recorder output to one runtime's stamp.
func (r *Registry) SetCorrelation(c *vlog.Correlation) {
	if c != nil {
		r.corr = c
	}
}

// Correlation returns the stamp owner bound before the registry freezes.
func (r *Registry) Correlation() *vlog.Correlation { return r.corr }

// TotalCount returns total metrics across all types
func (r *Registry) TotalCount() int {
	return r.Bools.Count() + r.Ints.Count() + r.Floats.Count() + r.Strings.Count()
}

// Freeze closes the metric set and caches the group index. Called once from
// ClockScheduler.Start, after World.Seal and before the first tick: every
// system and renderer has registered by then and nothing registers later.
func (r *Registry) Freeze() {
	r.idxMu.Lock()
	defer r.idxMu.Unlock()
	if r.frozen.Load() {
		return
	}
	// Reserve the self-report keys before the maps close, or they become
	// detached cells: absent from every snapshot and counted as late.
	statGroups := r.Ints.Get("stat.groups")
	statMetrics := r.Ints.Get("stat.metrics")

	r.Bools.Freeze()
	r.Ints.Freeze()
	r.Floats.Freeze()
	r.Strings.Freeze()

	idx := r.buildIndex()
	r.idx, r.idxGen = idx, r.gen()
	r.idxFast.Store(&idx)
	r.frozen.Store(true)

	statGroups.Store(int64(len(idx)))
	statMetrics.Store(int64(r.TotalCount()))

	if rc := r.rec.Load(); rc != nil {
		rc.bind(idx)
	}
}

// Frozen reports whether the metric set is closed
func (r *Registry) Frozen() bool { return r.frozen.Load() }

// lateCount sums registrations rejected after Freeze; non-zero is a
// regression, not a supported path
func (r *Registry) lateCount() int64 {
	return r.Bools.Late() + r.Ints.Late() + r.Floats.Late() + r.Strings.Late()
}

// EnableRecorder installs a flight recorder of the given tick depth; 0 removes
// it. Call before Freeze so the ring is laid out with the final metric set.
func (r *Registry) EnableRecorder(depth int) {
	if depth <= 0 {
		// Release the process-wide hook only if this registry still owns it
		if old := r.rec.Swap(nil); old != nil {
			active.CompareAndSwap(old, nil)
		}
		r.Ints.Get("rec.depth").Store(0)
		return
	}
	rc := newRecorder(r, depth)
	if r.frozen.Load() {
		rc.bind(r.groups())
	}
	r.rec.Store(rc)
	active.Store(rc)
}

// RecorderDepth returns the configured ring depth in ticks, 0 when disabled
func (r *Registry) RecorderDepth() int {
	if rc := r.rec.Load(); rc != nil {
		return rc.depth
	}
	return 0
}

// Recorder returns the installed recorder, nil when disabled
func (r *Registry) Recorder() *Recorder { return r.rec.Load() }
