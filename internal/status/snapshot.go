package status

import (
	"sort"
	"strconv"
	"strings"
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

// GroupGate selects the rule a group uses to decide whether it belongs in a
// view, snapshot or recorder window.
type GroupGate uint8

const (
	// GateAlways keeps the group unconditionally; the default for a plain domain card.
	GateAlways GroupGate = iota
	// GateSentinel keeps the group while a designated int metric is non-zero.
	GateSentinel
	// GateActivity keeps the group while any member holds a non-zero reading.
	GateActivity
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

// isZero reports whether the metric holds its type's empty reading
func (m *metricRef) isZero() bool {
	switch m.kind {
	case kindBool:
		return !m.b.Load()
	case kindInt:
		return m.i.Load() == 0
	case kindFloat:
		return m.f.Get() == 0
	case kindString:
		return m.s.Load() == ""
	}
	return true
}

// statGroup is one emitted record: a key prefix and its members
type statGroup struct {
	name    string
	members []metricRef
	visible *atomic.Int64 // Sentinel cell for GateSentinel; nil otherwise
	gate    GroupGate
}

// isVisible applies the group's gate to the current readings.
func (g *statGroup) isVisible() bool { return gateVisible(g.gate, g.visible, g.members) }

// gateVisible resolves one group's visibility; shared by the index and bound views.
// A GateSentinel group with no sentinel stays visible, so a mis-declared gate
// cannot silently hide a card.
func gateVisible(gate GroupGate, sentinel *atomic.Int64, members []metricRef) bool {
	switch gate {
	case GateSentinel:
		return sentinel == nil || sentinel.Load() != 0
	case GateActivity:
		for i := range members {
			if !members[i].isZero() {
				return true
			}
		}
		return false
	}
	return true
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
	}

	if every := r.snapEvery.Load(); every != 0 && n%every == 0 &&
		vlog.On(SubStat, vlog.LevelInfo) {
		// One explicit stamp for the whole snapshot: the frame counter belongs
		// to the render goroutine and can advance mid-emission
		run, tick, frame := r.Correlation().Stamp()
		_, _ = vlog.EmitSet(SubStat, run, tick, frame, r.emitGroups)
	}

	if rc != nil {
		rc.drain(n)
	}
}

// Snapshot emits one record per group, ordered by group then metric name.
// emit matches vlog.Info so an alternate sink can be substituted.
func (r *Registry) Snapshot(emit func(sub string, args ...any)) {
	r.SnapshotFiltered(nil, emit)
}

// SnapshotFiltered emits one record per group, omitting metrics keep rejects and
// skipping a group left with none. A nil keep admits every metric.
func (r *Registry) SnapshotFiltered(keep func(key string) bool, emit func(sub string, args ...any)) {
	r.emitGroupsFiltered(keep, func(args ...any) { emit(SubStat, args...) })
}

// emitGroups writes every grouped record through a stamp-bound emitter
func (r *Registry) emitGroups(emit func(args ...any)) {
	r.emitGroupsFiltered(nil, emit)
}

// emitGroupsFiltered writes grouped records, omitting metrics keep rejects
func (r *Registry) emitGroupsFiltered(keep func(key string) bool, emit func(args ...any)) {
	for _, g := range r.groups() {
		if !g.isVisible() {
			continue
		}
		// Fresh slice per record: vlog formats asynchronously
		args := make([]any, 0, 2+2*len(g.members))
		args = append(args, "msg", g.name)
		for i := range g.members {
			m := &g.members[i]
			if keep != nil && !keep(m.key) {
				continue
			}
			args = append(args, m.name, m.value())
		}
		if len(args) == 2 {
			continue // every member filtered out
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
	groupSlots := make(map[string]string, 32)
	playerEntities := make(map[string]*atomic.Int64, 16)
	add := func(key string, ref metricRef) {
		g, name, slot := splitKey(key)
		ref.key, ref.name = key, name
		byGroup[g] = append(byGroup[g], ref)
		if slot != "" {
			groupSlots[g] = slot
		}
	}

	r.Bools.Range(func(k string, p *atomic.Bool) { add(k, metricRef{kind: kindBool, b: p}) })
	r.Ints.Range(func(k string, p *atomic.Int64) {
		add(k, metricRef{kind: kindInt, i: p})
		if domain, name, ok := strings.Cut(k, "."); ok && domain == "player" {
			if slot, metric, ok := splitPlayerMetric(name); ok && metric == "entity" {
				playerEntities[slot] = p
			}
		}
	})
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
		slot := groupSlots[g]
		groups = append(groups, statGroup{
			name:    g,
			members: members,
			visible: playerEntities[slot],
			gate:    groupGate(g, slot),
		})
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
