package engine

import (
	"reflect"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
)

// StoreDelta is one component store's exact difference against a baseline: Changed
// holds new and differing entries, Removed the entities gone. Order is the entity
// sequence the store must end in, present only when swap-back removal makes it
// underivable, so a reconstruction reproduces the capture byte for byte and its
// integrity hash can be re-checked.
type StoreDelta[T any] struct {
	Changed []StoreEntry[T] `json:"c,omitempty"`
	Removed []core.Entity   `json:"r,omitempty"`
	Order   []core.Entity   `json:"o,omitempty"`
}

// Empty reports whether this store contributed nothing to the delta.
func (d StoreDelta[T]) Empty() bool {
	return len(d.Changed) == 0 && len(d.Removed) == 0 && len(d.Order) == 0
}

// Entries counts what one store's correction moves, which is the unit the
// correction magnitude is reported in.
func (d StoreDelta[T]) Entries() int { return len(d.Changed) + len(d.Removed) }

// snapshotDetacher is a component owning storage a capture must not share with the
// live world: one written in place through Store.GetPtr would change under whoever
// retained the capture. It is an interface on the component rather than a list, so
// a component that grows a slice is detached without anyone remembering to add it.
type snapshotDetacher[T any] interface{ DetachSnapshot() T }

// DetachSnapshotValue returns a component value that shares no storage with the
// world it came from, at both boundaries: reading a capture out of the stores and
// writing one back into them.
func DetachSnapshotValue[T any](v T) T {
	if d, ok := any(v).(snapshotDetacher[T]); ok {
		return d.DetachSnapshot()
	}
	return v
}

// diffStore computes one store's delta against a baseline. Values compare with
// reflect.DeepEqual: three shared components carry a slice, so == is not available
// to the set, and a per-component list of which need a deep compare would drift.
func diffStore[T any](base, next []StoreEntry[T]) StoreDelta[T] {
	var d StoreDelta[T]

	baseIndex := make(map[core.Entity]int, len(base))
	for i, en := range base {
		baseIndex[en.Entity] = i
	}
	inNext := make(map[core.Entity]struct{}, len(next))

	for _, en := range next {
		inNext[en.Entity] = struct{}{}
		i, ok := baseIndex[en.Entity]
		if !ok || !reflect.DeepEqual(base[i].Value, en.Value) {
			d.Changed = append(d.Changed, en)
		}
	}
	for _, en := range base {
		if _, ok := inNext[en.Entity]; !ok {
			d.Removed = append(d.Removed, en.Entity)
		}
	}
	if !sameOrder(derivedOrder(base, inNext, next), next) {
		d.Order = make([]core.Entity, len(next))
		for i, en := range next {
			d.Order[i] = en.Entity
		}
	}
	return d
}

// derivedOrder is the sequence applyStore produces without an explicit Order: the
// baseline's surviving entities in their baseline order, then whatever is new, in
// the order the sender holds it.
func derivedOrder[T any](base []StoreEntry[T], inNext map[core.Entity]struct{}, next []StoreEntry[T]) []core.Entity {
	out := make([]core.Entity, 0, len(next))
	seen := make(map[core.Entity]struct{}, len(base))
	for _, en := range base {
		if _, ok := inNext[en.Entity]; ok {
			out = append(out, en.Entity)
			seen[en.Entity] = struct{}{}
		}
	}
	for _, en := range next {
		if _, ok := seen[en.Entity]; !ok {
			out = append(out, en.Entity)
		}
	}
	return out
}

func sameOrder[T any](order []core.Entity, next []StoreEntry[T]) bool {
	if len(order) != len(next) {
		return false
	}
	for i, e := range order {
		if next[i].Entity != e {
			return false
		}
	}
	return true
}

// applyStore reconstructs one store from a baseline and a delta. The result is the
// sender's slice exactly — same entries, same order — or the integrity hash the
// caller checks next will say so.
func applyStore[T any](base []StoreEntry[T], d StoreDelta[T]) []StoreEntry[T] {
	if d.Empty() {
		return base
	}
	values := make(map[core.Entity]T, len(base)+len(d.Changed))
	for _, en := range base {
		values[en.Entity] = en.Value
	}
	for _, e := range d.Removed {
		delete(values, e)
	}
	for _, en := range d.Changed {
		values[en.Entity] = en.Value
	}

	order := d.Order
	if order == nil {
		removed := make(map[core.Entity]struct{}, len(d.Removed))
		for _, e := range d.Removed {
			removed[e] = struct{}{}
		}
		seen := make(map[core.Entity]struct{}, len(base))
		order = make([]core.Entity, 0, len(values))
		for _, en := range base {
			if _, gone := removed[en.Entity]; gone {
				continue
			}
			order = append(order, en.Entity)
			seen[en.Entity] = struct{}{}
		}
		for _, en := range d.Changed {
			if _, ok := seen[en.Entity]; ok {
				continue
			}
			order = append(order, en.Entity)
			seen[en.Entity] = struct{}{}
		}
	}

	out := make([]StoreEntry[T], 0, len(order))
	for _, e := range order {
		v, ok := values[e]
		if !ok {
			// An Order naming an entity the delta does not carry is a malformed
			// delta. Dropping it here keeps the reconstruction total; the caller's
			// integrity check is what refuses the result.
			continue
		}
		out = append(out, StoreEntry[T]{Entity: e, Value: v})
	}
	return out
}

// countStoreDifference reports how many entries differ between two readings of one
// store and records the entities behind them, which is the correction magnitude's
// unit and its entity count. Two readings of one world mostly hold a store in one
// order, so their common prefix is compared in place and only the rest is indexed.
func countStoreDifference[T any](a, b []StoreEntry[T], touched map[core.Entity]struct{}) int {
	n, k := 0, 0
	for ; k < len(a) && k < len(b) && a[k].Entity == b[k].Entity; k++ {
		if !reflect.DeepEqual(a[k].Value, b[k].Value) {
			n++
			touched[b[k].Entity] = struct{}{}
		}
	}
	a, b = a[k:], b[k:]
	index := make(map[core.Entity]int, len(a))
	for i, en := range a {
		index[en.Entity] = i
	}
	seen := make(map[core.Entity]struct{}, len(b))
	for _, en := range b {
		seen[en.Entity] = struct{}{}
		i, ok := index[en.Entity]
		if !ok || !reflect.DeepEqual(a[i].Value, en.Value) {
			n++
			touched[en.Entity] = struct{}{}
		}
	}
	for _, en := range a {
		if _, ok := seen[en.Entity]; !ok {
			n++
			touched[en.Entity] = struct{}{}
		}
	}
	return n
}

// WorldDifference is how far apart two readings of the shared world are: on a guest,
// the correction magnitude, which is telemetry rather than a fault. Entries counts
// every disagreeing component cell, Entities the shared entities behind them, and
// CellShift the largest distance a shared placement moves.
type WorldDifference struct {
	Entries   int
	Entities  int
	CellShift int
}

// positionShift is the largest distance a shared placement moves between two
// readings, in cells. It is Chebyshev because the grid is: a diagonal step is one
// cell of visible correction, not one and a half.
func positionShift(a, b []StoreEntry[component.PositionComponent]) int {
	index := make(map[core.Entity]component.PositionComponent, len(a))
	for _, en := range a {
		index[en.Entity] = en.Value
	}
	shift := 0
	for _, en := range b {
		prev, ok := index[en.Entity]
		if !ok {
			continue
		}
		if d := max(abs(prev.X-en.Value.X), abs(prev.Y-en.Value.Y)); d > shift {
			shift = d
		}
	}
	return shift
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// reconcileStore brings one component store's shared half to entries.
//
// Removals are collected before any of them runs: the stores are dense with
// swap-back removal, so removing while iterating Entities() would skip whichever
// entity the last removal moved into the freed slot.
func reconcileStore[T any](s *Store[T], entries []StoreEntry[T]) {
	target := make(map[core.Entity]struct{}, len(entries))
	for _, en := range entries {
		target[en.Entity] = struct{}{}
	}
	var stale []core.Entity
	for _, e := range s.Entities() {
		if e.Domain() != core.DomainShared {
			continue
		}
		if _, ok := target[e]; !ok {
			stale = append(stale, e)
		}
	}
	for _, e := range stale {
		s.RemoveEntity(e)
	}
	for _, en := range entries {
		s.SetComponent(en.Entity, DetachSnapshotValue(en.Value))
	}
}

// reconcilePositions is reconcileStore for the placement store, which has its own
// type because a placement also carries a spatial index cell.
func reconcilePositions(p *Position, entries []StoreEntry[component.PositionComponent]) {
	target := make(map[core.Entity]struct{}, len(entries))
	for _, en := range entries {
		target[en.Entity] = struct{}{}
	}
	var stale []core.Entity
	for _, e := range p.Entities() {
		if e.Domain() != core.DomainShared {
			continue
		}
		if _, ok := target[e]; !ok {
			stale = append(stale, e)
		}
	}
	for _, e := range stale {
		p.RemoveEntity(e)
	}
	for _, en := range entries {
		p.SetPosition(en.Entity, en.Value)
	}
}
