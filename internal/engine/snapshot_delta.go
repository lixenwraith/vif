// Package engine: the shared world expressed against another one.
//
// A capture describes the whole shared world, which is the right shape for a join
// and the wrong one for a correction. §8's measurement is what says so: at the
// storm high water the schema is 176 KiB, which was 859 KiB/s at 5 Hz before the
// wire codec. Compression reduces transport repetition; a delta separately avoids
// computing and carrying unchanged logical state. Between two captures 200 ms
// apart most of that is unchanged: walls do not move, a gold
// sequence's members do not move, and a swarm's genotype does not change while its
// position does.
//
// So a correction carries the difference. The diff is per store and per entity,
// because that is the granularity the stores themselves have, and it is *exact*:
// applying a delta to the baseline it was computed against reproduces the next
// capture byte for byte, entity order included, which is what lets the receiver
// re-check the capture's own integrity hash after reconstructing it. A delta that
// reconstructed something merely equivalent would pass every value comparison and
// fail that hash, and the hash is the only end-to-end statement a receiver has.
//
// The same comparison answers a second question the transport does not ask.
// Requirement 4 wants the correction *magnitude* as telemetry rather than as an
// error, and the magnitude is exactly the difference between the world a guest
// predicted and the world the host is sending it. One mechanism, two readers.
package engine

import (
	"reflect"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
)

// StoreDelta is one component store's difference against a baseline.
//
// Changed carries the entries whose value differs from the baseline's, and the
// entries the baseline does not have at all — a receiver cannot tell those apart
// and does not need to. Removed names the entities the baseline holds and this
// store no longer does.
//
// Order is the exact entity sequence the store must end up in, and it is present
// only when the sequence cannot be derived. The stores are dense with swap-back
// removal, so removing an entity moves whichever one was last into its place: a
// receiver replaying removals in baseline order would end up with the same *set*
// in a different order, and a capture is compared and hashed as bytes. When the
// derived order is already right — which is every delta that removes nothing —
// this field is absent and costs nothing.
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

// diffStore computes one store's delta against a baseline.
//
// Values are compared with reflect.DeepEqual rather than with ==, and that is a
// deliberate cost rather than an oversight: three of the shared components carry a
// slice (a snake's segments, a genotype's genes, a composite header's member
// table), so the `comparable` constraint is not available for the set as a whole,
// and a per-component declaration of which ones need a deep compare is a thing
// that can drift from the components. DeepEqual is total over all of them. The
// diff runs on the capture pump's goroutine, outside the world lock, so what it
// costs is not a tick's to pay.
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
// unit and its entity count.
func countStoreDifference[T any](a, b []StoreEntry[T], touched map[core.Entity]struct{}) int {
	index := make(map[core.Entity]int, len(a))
	for i, en := range a {
		index[en.Entity] = i
	}
	n := 0
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

// WorldDifference is how far apart two readings of the shared world are.
//
// It is the number Phase 4's requirement 6 puts where DESYNC used to be. A guest
// predicts between corrections, so a difference is the expected condition rather
// than a fault; what matters is its size and whether it stays bounded. Entries
// counts every component cell that disagrees, Entities the distinct shared
// entities behind them, and CellShift the largest distance a shared placement
// moves — the one a player would actually see.
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
		s.SetComponent(en.Entity, en.Value)
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
