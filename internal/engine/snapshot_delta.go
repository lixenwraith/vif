package engine

import (
	"slices"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/position"
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

// diffStore computes one store's delta against a baseline. equal is the store's
// generated comparison, so an entry it omits encodes as the baseline's does. The
// common prefix of the two orders is compared in place, so a store that kept its
// order, as a static one does, is diffed without being indexed.
func diffStore[T any](base, next []StoreEntry[T], equal func(a, b *T) bool) StoreDelta[T] {
	var d StoreDelta[T]
	k := 0
	for ; k < len(base) && k < len(next) && base[k].Entity == next[k].Entity; k++ {
		if !equal(&base[k].Value, &next[k].Value) {
			d.Changed = append(d.Changed, next[k])
		}
	}
	restBase, restNext := base[k:], next[k:]
	if len(restBase) == 0 && len(restNext) == 0 {
		return d
	}

	baseIndex := make(map[core.Entity]int, len(restBase))
	for i, en := range restBase {
		baseIndex[en.Entity] = i
	}
	inNext := make(map[core.Entity]struct{}, len(restNext))
	for j := range restNext {
		en := &restNext[j]
		inNext[en.Entity] = struct{}{}
		i, ok := baseIndex[en.Entity]
		if !ok || !equal(&restBase[i].Value, &en.Value) {
			d.Changed = append(d.Changed, *en)
		}
	}
	for _, en := range restBase {
		if _, ok := inNext[en.Entity]; !ok {
			d.Removed = append(d.Removed, en.Entity)
		}
	}
	if !sameOrder(derivedOrder(restBase, inNext, restNext), restNext) {
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
func countStoreDifference[T any](a, b []StoreEntry[T], touched map[core.Entity]struct{}, equal func(a, b *T) bool) int {
	n, k := 0, 0
	for ; k < len(a) && k < len(b) && a[k].Entity == b[k].Entity; k++ {
		if !equal(&a[k].Value, &b[k].Value) {
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
	for j := range b {
		en := &b[j]
		seen[en.Entity] = struct{}{}
		i, ok := index[en.Entity]
		if !ok || !equal(&a[i].Value, &en.Value) {
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
	if s.bit == WallBit { // an overwrite can rewrite a BlockMask
		s.world.Positions.BlocksChanged()
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

// captureRows reads one domain's rows of a store in dense order, detached from it.
func captureRows[T any](dense []T, entities []core.Entity, d core.Domain) []StoreEntry[T] {
	var out []StoreEntry[T]
	for i, e := range entities {
		if e.Domain() == d {
			out = append(out, StoreEntry[T]{Entity: e, Value: DetachSnapshotValue(dense[i])})
		}
	}
	return out
}

// installPlayerRows writes a capture's player rows into a store in capture order,
// passing over any naming a shared entity, which only the shared world writes.
func installPlayerRows[T any](s *Store[T], entries []StoreEntry[T]) {
	for _, en := range entries {
		if en.Entity.Domain() == core.DomainPlayer {
			s.SetComponent(en.Entity, DetachSnapshotValue(en.Value))
		}
	}
}

// capturePlayer reads the player placements in dense order, and the ones the
// spatial index holds out of their cell.
func (p *Position) capturePlayer() ([]StoreEntry[component.PositionComponent], []core.Entity) {
	placed := captureRows(p.dense, p.entities, core.DomainPlayer)
	var out []core.Entity
	for _, en := range placed {
		if !p.Index.Holds(en.Entity, EntityClass(en.Entity), en.Value.X, en.Value.Y) {
			out = append(out, en.Entity)
		}
	}
	return placed, out
}

// installPlayer places captured player entities in capture order, holding out of
// the spatial index the ones out names, as the capture's cells did.
func (p *Position) installPlayer(placed []StoreEntry[component.PositionComponent], out []core.Entity) {
	held := make(map[core.Entity]struct{}, len(out))
	for _, e := range out {
		held[e] = struct{}{}
	}
	for _, en := range placed {
		if en.Entity.Domain() != core.DomainPlayer {
			continue
		}
		if _, ok := held[en.Entity]; ok && !p.HasPosition(en.Entity) {
			p.insert(en.Entity, en.Value)
			continue
		}
		p.SetPosition(en.Entity, en.Value)
	}
}

var errOrderMismatch = position.ErrOrderMismatch

// orderOf is a store's dense sequence, where two or more rows give it one.
func orderOf(entities []core.Entity) []core.Entity {
	if len(entities) < 2 {
		return nil
	}
	return slices.Clone(entities)
}

// permuted reads dense in order through index, refusing an order that repeats an
// entity or names one index lacks: either leaves a row of the store out.
func permuted[T any](index *sparseIndex, dense []T, order []core.Entity) ([]T, error) {
	out := make([]T, len(order))
	seen := make(map[core.Entity]struct{}, len(order))
	for i, e := range order {
		j, ok := index.get(e)
		if _, dup := seen[e]; !ok || dup {
			return nil, errOrderMismatch
		}
		seen[e] = struct{}{}
		out[i] = dense[j]
	}
	return out, nil
}

// reorderStore puts a store's rows in the order orderOf read.
func reorderStore[T any](s *Store[T], order []core.Entity) error {
	return reorderRows(&s.index, s.dense, s.entities, order)
}

// reorderRows puts dense rows, their entities and index in order; an empty order
// leaves fewer than two rows as they are
func reorderRows[T any](index *sparseIndex, dense []T, entities, order []core.Entity) error {
	if len(order) == 0 && len(entities) < 2 {
		return nil
	}
	if len(order) != len(entities) {
		return errOrderMismatch
	}
	rows, err := permuted(index, dense, order)
	if err != nil {
		return err
	}
	copy(dense, rows)
	copy(entities, order)
	for i, e := range order {
		index.set(e, int32(i))
	}
	return nil
}

// reorder puts the placements in order and each cell's occupants in its own.
func (p *Position) reorder(order []core.Entity, cells []position.CellOrder[core.Entity]) error {
	if err := reorderRows(&p.slots, p.dense, p.entities, order); err != nil {
		return err
	}
	return p.Index.Reorder(cells, EntityClass)
}

// cellOrder reads the occupants of every cell holding two or more.
func (p *Position) cellOrder() []position.CellOrder[core.Entity] { return p.Index.Order() }

// WorldReconcile is one world brought to another's shared half in place, store by
// store, with no capture of either: the comparison first, reading both and writing
// nothing, then Finish, writing in ReconcileSharedWorld's order so the two leave one
// dense order. Difference is the correction magnitude over the compared surface
// (WithoutLocalCursorState, as SharedWorldDifference reads it); Delta is DiffSharedWorld's.
type WorldReconcile struct {
	Difference WorldDifference
	Delta      *SharedWorldDelta

	dst, src                 *World
	record                   bool
	touched                  map[core.Entity]struct{}
	dstAuthored, srcAuthored map[core.Entity]bool
	writes                   []func() // one store each, in store order
}

func newWorldReconcile(dst, src *World, record bool) *WorldReconcile {
	r := &WorldReconcile{dst: dst, src: src, record: record, touched: make(map[core.Entity]struct{}, 64)}
	r.dstAuthored, r.srcAuthored = authoredCursors(dst), authoredCursors(src)
	return r
}

// authoredCursors are the cursors a participant owns, whose owner-authored cells
// the compared surface leaves out.
func authoredCursors(w *World) map[core.Entity]bool {
	authored := make(map[core.Entity]bool, parameter.MaxPlayers)
	w.Components.Cursor.Each(func(e core.Entity, c *component.CursorComponent) bool {
		if c.PeerID != 0 {
			authored[e] = true
		}
		return true
	})
	return authored
}

func (r *WorldReconcile) count(e core.Entity) {
	r.Difference.Entries++
	r.touched[e] = struct{}{}
}

// compared closes the comparison: the entities behind the count and, when recorded,
// the delta with the scalars DiffSharedWorld carries whole.
func (r *WorldReconcile) compared(d SharedWorldDelta) *WorldReconcile {
	r.Difference.Entities = len(r.touched)
	if r.record {
		d.NextEntity = r.src.nextEntityID[core.DomainShared]
		d.Created, d.Destroyed = r.src.createdCount[core.DomainShared].Load(), r.src.destroyedCount[core.DomainShared].Load()
		r.Delta = &d
	}
	return r
}

// Finish writes what the comparison found, as ReconcileSharedWorld writes a capture:
// the entities src no longer holds leave whole first, then each store removes its
// stale cells and writes the differing ones in src's order, then the allocator is
// src's. Caller MUST hold both worlds' updateMutex.
func (r *WorldReconcile) Finish() {
	gone := make([]core.Entity, 0, 8)
	for e := range r.dst.componentMask {
		if e.Domain() != core.DomainShared {
			continue
		}
		if mask, ok := r.src.componentMask[e]; !ok || mask == 0 {
			gone = append(gone, e)
		}
	}
	slices.Sort(gone)
	for _, e := range gone {
		r.dst.removeEntity(e)
	}
	for _, write := range r.writes {
		write()
	}
	r.writes = nil
	if n := r.src.nextEntityID[core.DomainShared]; n > 0 {
		r.dst.nextEntityID[core.DomainShared] = n
	}
	r.dst.createdCount[core.DomainShared].Store(r.src.createdCount[core.DomainShared].Load())
	r.dst.destroyedCount[core.DomainShared].Store(r.src.destroyedCount[core.DomainShared].Load())
	r.dst.Positions.PublishTelemetry()
}

// compareStoreFrom compares one store's shared half against src's: a cell dst lacks
// or holds differently is noted for the write, and counted when the compared
// surface holds it on both sides or one, a cursor's control and an authored
// cursor's owned cells being off it. The delta is diffStore's over the two stores.
func compareStoreFrom[T any](r *WorldReconcile, dst, src *Store[T], equal func(a, b *T) bool) StoreDelta[T] {
	owned, cursor := ownedCursorBit(dst.bit), dst.bit == CursorBit
	var d StoreDelta[T]
	var changed []int32
	for i, e := range src.entities {
		if e.Domain() != core.DomainShared {
			continue
		}
		v := &src.dense[i]
		p, ok := dst.GetPtr(e)
		onSrc := !owned || !r.srcAuthored[e]
		onDst := ok && (!owned || !r.dstAuthored[e])
		same := ok && equal(p, v)
		if !same {
			changed = append(changed, int32(i))
			if r.record {
				d.Changed = append(d.Changed, StoreEntry[T]{Entity: e, Value: DetachSnapshotValue(*v)})
			}
		}
		switch {
		case same:
			if onSrc != onDst {
				r.count(e)
			}
		case !ok:
			if onSrc {
				r.count(e)
			}
		case onSrc && onDst:
			if !cursor || controlFreeDiffers(p, v) {
				r.count(e)
			}
		case onSrc != onDst:
			r.count(e)
		}
	}
	for _, e := range dst.entities {
		if e.Domain() != core.DomainShared || src.HasEntity(e) {
			continue
		}
		if r.record {
			d.Removed = append(d.Removed, e)
		}
		if !owned || !r.dstAuthored[e] {
			r.count(e)
		}
	}
	if r.record {
		d.Order = orderDelta(sharedOrder(dst.entities), sharedOrder(src.entities))
	}
	r.writes = append(r.writes, func() { writeStoreFrom(dst, src, changed) })
	return d
}

// writeStoreFrom is reconcileStore over src's store: the stale cells leave, then the
// differing cells are written in src's order.
func writeStoreFrom[T any](dst, src *Store[T], changed []int32) {
	var stale []core.Entity
	for _, e := range dst.entities {
		if e.Domain() == core.DomainShared && !src.HasEntity(e) {
			stale = append(stale, e)
		}
	}
	for _, e := range stale {
		dst.RemoveEntity(e)
	}
	for _, i := range changed {
		dst.SetComponent(src.entities[i], DetachSnapshotValue(src.dense[i]))
	}
	if dst.bit == WallBit { // an overwrite can rewrite a BlockMask
		dst.world.Positions.BlocksChanged()
	}
}

// controlFreeDiffers compares two cursor cells as the surface does, control aside.
func controlFreeDiffers[T any](a, b *T) bool {
	ca, cb := *any(a).(*component.CursorComponent), *any(b).(*component.CursorComponent)
	ca.Control, cb.Control = 0, 0
	return !equalCursor(&ca, &cb)
}

// comparePositionsFrom is compareStoreFrom for the placement store, which also
// measures the largest shift a placement makes; its write is reconcilePositions',
// every placement set so a cell held out of the grid retries its insertion.
func comparePositionsFrom(r *WorldReconcile) StoreDelta[component.PositionComponent] {
	dst, src := r.dst.Positions, r.src.Positions
	var d StoreDelta[component.PositionComponent]
	for i, e := range src.entities {
		if e.Domain() != core.DomainShared {
			continue
		}
		v := src.dense[i]
		p, ok := dst.GetPosition(e)
		if ok && p == v {
			continue
		}
		if r.record {
			d.Changed = append(d.Changed, StoreEntry[component.PositionComponent]{Entity: e, Value: v})
		}
		if ok {
			r.Difference.CellShift = max(r.Difference.CellShift, abs(p.X-v.X), abs(p.Y-v.Y))
		}
		r.count(e)
	}
	for _, e := range dst.entities {
		if _, held := src.GetPosition(e); e.Domain() != core.DomainShared || held {
			continue
		}
		if r.record {
			d.Removed = append(d.Removed, e)
		}
		r.count(e)
	}
	if r.record {
		d.Order = orderDelta(sharedOrder(dst.entities), sharedOrder(src.entities))
	}
	r.writes = append(r.writes, func() {
		var stale []core.Entity
		for _, e := range dst.entities {
			if _, held := src.GetPosition(e); e.Domain() == core.DomainShared && !held {
				stale = append(stale, e)
			}
		}
		for _, e := range stale {
			dst.RemoveEntity(e)
		}
		for i, e := range src.entities {
			if e.Domain() == core.DomainShared {
				dst.SetPosition(e, src.dense[i])
			}
		}
	})
	return d
}

// sharedOrder is a store's shared entities in its dense order.
func sharedOrder(entities []core.Entity) []core.Entity {
	out := make([]core.Entity, 0, len(entities))
	for _, e := range entities {
		if e.Domain() == core.DomainShared {
			out = append(out, e)
		}
	}
	return out
}

// orderDelta is the Order diffStore records between two readings of one store: the
// whole of next when applying the delta to base would not reproduce next's order.
func orderDelta(base, next []core.Entity) []core.Entity {
	k := 0
	for ; k < len(base) && k < len(next) && base[k] == next[k]; k++ {
	}
	restBase, restNext := base[k:], next[k:]
	if len(restBase) == 0 && len(restNext) == 0 {
		return nil
	}
	inNext := make(map[core.Entity]struct{}, len(restNext))
	for _, e := range restNext {
		inNext[e] = struct{}{}
	}
	derived := make([]core.Entity, 0, len(restNext))
	seen := make(map[core.Entity]struct{}, len(restBase))
	for _, e := range restBase {
		if _, ok := inNext[e]; ok {
			derived = append(derived, e)
			seen[e] = struct{}{}
		}
	}
	for _, e := range restNext {
		if _, ok := seen[e]; !ok {
			derived = append(derived, e)
		}
	}
	if slices.Equal(derived, restNext) {
		return nil
	}
	return slices.Clone(next)
}
