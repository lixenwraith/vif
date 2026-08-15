package status

import (
	"sort"
	"strconv"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// SubStat is the vlog subsystem tag on every snapshot record.
// Record shape: sub="stat", msg=<group>, one record per group per snapshot.
// Records of one snapshot share the run/tick/frame stamp.
const SubStat = "stat"

// Metric kinds in the snapshot index
const (
	kindBool uint8 = iota
	kindInt
	kindFloat
	kindString
)

// metricRef binds a metric's short name to its live storage
type metricRef struct {
	b    *atomic.Bool
	i    *atomic.Int64
	f    *AtomicFloat
	s    *AtomicString
	key  string // Full "group.name"; carries the unit convention
	name string
	kind uint8
}

// value loads the current reading; primitives only, never a shared pointer
func (m *metricRef) value() any {
	switch m.kind {
	case kindBool:
		return m.b.Load()
	case kindInt:
		return m.i.Load()
	case kindFloat:
		return m.f.Get()
	case kindString:
		return m.s.Load()
	}
	return nil
}

// statGroup is one emitted record: a key prefix and its members
type statGroup struct {
	name    string
	members []metricRef
}

// SetSnapshotInterval sets the game-tick period between snapshots; 0 disables
func (r *Registry) SetSnapshotInterval(ticks uint64) { r.snapEvery.Store(ticks) }

// SnapshotInterval returns the current period in game ticks
func (r *Registry) SnapshotInterval() uint64 { return r.snapEvery.Load() }

// Tick reports a completed game tick: samples the flight recorder, emits the
// periodic snapshot on its period, then drains any pending flush request.
// Every read is atomic, caller MUST NOT hold the world lock; call it once the
// tick's writes are settled or the readings straddle two ticks.
func (r *Registry) Tick(n uint64) {
	rc := r.rec.Load()
	if rc != nil {
		rc.sample(n)
	}
	if late := r.lateCount(); late != 0 {
		r.statLate.Store(late)
		r.statLateKey.StoreIfChanged(r.lateKey())
	}

	if every := r.snapEvery.Load(); every != 0 && n%every == 0 &&
		vlog.On(SubStat, vlog.LevelInfo) {
		// One explicit stamp for the whole snapshot: the frame counter belongs
		// to the render goroutine and can advance mid-emission
		run, tick, frame := vlog.Stamp()
		_, _ = vlog.EmitSet(SubStat, run, tick, frame, r.emitGroups)
	}

	if rc != nil {
		rc.drain(n)
	}
}

// Snapshot emits one record per group, ordered by group then metric name.
// emit matches vlog.Info so an alternate sink can be substituted.
func (r *Registry) Snapshot(emit func(sub string, args ...any)) {
	r.emitGroups(func(args ...any) { emit(SubStat, args...) })
}

// emitGroups writes the grouped records through a stamp-bound emitter
func (r *Registry) emitGroups(emit func(args ...any)) {
	for _, g := range r.groups() {
		// Fresh slice per record: vlog formats asynchronously
		args := make([]any, 0, 2+2*len(g.members))
		args = append(args, "msg", g.name)
		for i := range g.members {
			args = append(args, g.members[i].name, g.members[i].value())
		}
		emit(args...)
	}
}

// groups returns the cached index; immutable and lock-free after Freeze
func (r *Registry) groups() []statGroup {
	if r.frozen.Load() {
		if p := r.idxFast.Load(); p != nil {
			return *p
		}
	}
	gen := r.gen()

	r.idxMu.Lock()
	defer r.idxMu.Unlock()
	if r.idx != nil && r.idxGen == gen {
		return r.idx
	}
	r.idx = r.buildIndex()
	r.idxGen = gen
	return r.idx
}

// gen combines the four registration counters into one invalidation key
func (r *Registry) gen() uint64 {
	return r.Bools.Gen() + r.Ints.Gen() + r.Floats.Gen() + r.Strings.Gen()
}

// buildIndex flattens all four maps into group-ordered metric references
func (r *Registry) buildIndex() []statGroup {
	byGroup := make(map[string][]metricRef, 48)
	add := func(key string, ref metricRef) {
		g, name := SplitKey(key)
		ref.key, ref.name = key, name
		byGroup[g] = append(byGroup[g], ref)
	}

	r.Bools.Range(func(k string, p *atomic.Bool) { add(k, metricRef{kind: kindBool, b: p}) })
	r.Ints.Range(func(k string, p *atomic.Int64) { add(k, metricRef{kind: kindInt, i: p}) })
	r.Floats.Range(func(k string, p *AtomicFloat) { add(k, metricRef{kind: kindFloat, f: p}) })
	r.Strings.Range(func(k string, p *AtomicString) { add(k, metricRef{kind: kindString, s: p}) })

	names := make([]string, 0, len(byGroup))
	for g := range byGroup {
		names = append(names, g)
	}
	sort.Strings(names)

	// Members interleave across the four maps, so each group re-sorts by name
	groups := make([]statGroup, 0, len(names))
	for _, g := range names {
		members := byGroup[g]
		sort.Slice(members, func(i, j int) bool { return members[i].name < members[j].name })
		groups = append(groups, statGroup{name: g, members: members})
	}
	return groups
}

// display renders the current reading for a UI consumer; int metrics resolve
// their unit through the key convention
func (m *metricRef) display() string {
	switch m.kind {
	case kindBool:
		return strconv.FormatBool(m.b.Load())
	case kindInt:
		return FormatInt(m.key, m.i.Load())
	case kindFloat:
		return strconv.FormatFloat(m.f.Get(), 'f', 3, 64)
	case kindString:
		return m.s.Load()
	}
	return ""
}
