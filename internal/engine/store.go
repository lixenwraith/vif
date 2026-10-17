package engine

import (
	"unsafe"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter"
)

// Store is a sparse-set container for component type T.
//
// SYNC INVARIANT: all access occurs under World.updateMutex. The tick
// scheduler, event loop, render pass, and input path each acquire it;
// there is no internal locking.
//
// Layout: dense[i] pairs with entities[i]; index maps entity -> dense slot.
// Removal swap-removes and rewrites the moved entity's index, so indices
// are valid by construction — no tombstones, no stale slots.
type Store[T any] struct {
	world    *World
	index    sparseIndex
	dense    []T
	entities []core.Entity // parallel to dense

	bit uint64
}

// NewStore creates a new component store for type T
func NewStore[T any](w *World, bit uint64) *Store[T] {
	return &Store[T]{
		dense:    make([]T, 0, 256),
		entities: make([]core.Entity, 0, 256),
		world:    w,
		bit:      bit,
	}
}

// SetComponent inserts or overwrites a component for an entity
func (s *Store[T]) SetComponent(e core.Entity, val T) {
	if i, ok := s.index.get(e); ok {
		s.dense[i] = val
		return
	}
	s.index.set(e, int32(len(s.dense)))
	s.dense = append(s.dense, val)
	s.entities = append(s.entities, e)
	s.world.AddComponentMask(e, s.bit)
}

// GetComponent retrieves a component by value (compatibility API)
func (s *Store[T]) GetComponent(e core.Entity) (T, bool) {
	if i, ok := s.index.get(e); ok {
		return s.dense[i], true
	}
	var zero T
	return zero, false
}

// GetPtr returns a direct pointer to the stored component.
// The pointer is valid until the next structural change on THIS store:
// SetComponent of a new entity (append may reallocate) or any removal
// (swap-remove moves elements). Mutations through the pointer persist
// without a SetComponent write-back.
func (s *Store[T]) GetPtr(e core.Entity) (*T, bool) {
	if i, ok := s.index.get(e); ok {
		return &s.dense[i], true
	}
	return nil, false
}

// HasEntity checks if entity has this component
func (s *Store[T]) HasEntity(e core.Entity) bool {
	_, ok := s.index.get(e)
	return ok
}

// removeAt swap-removes dense slot i, fixing the moved entity's index
func (s *Store[T]) removeAt(i int32) {
	e := s.entities[i]
	last := int32(len(s.dense) - 1)

	if i != last {
		moved := s.entities[last]
		s.dense[i] = s.dense[last]
		s.entities[i] = moved
		s.index.set(moved, i)
	}

	// Zero the vacated tail slot so pointer-bearing components
	// (Genes []float64, MemberEntries, etc.) release their references
	var zero T
	s.dense[last] = zero
	s.dense = s.dense[:last]
	s.entities = s.entities[:last]
	s.index.del(e)
}

// RemoveEntity deletes a component from an entity. O(1)
func (s *Store[T]) RemoveEntity(e core.Entity, skipMask ...bool) {
	i, ok := s.index.get(e)
	if !ok {
		return
	}
	s.removeAt(i)
	if len(skipMask) == 0 || !skipMask[0] {
		s.world.RemoveComponentMask(e, s.bit)
	}
}

// RemoveBatch deletes multiple entities. O(m), no allocation
func (s *Store[T]) RemoveBatch(entities []core.Entity, skipMask ...bool) {
	if len(s.entities) == 0 {
		return
	}
	clearMask := len(skipMask) == 0 || !skipMask[0]
	for _, e := range entities {
		i, ok := s.index.get(e)
		if !ok {
			continue
		}
		s.removeAt(i)
		if clearMask {
			s.world.RemoveComponentMask(e, s.bit)
		}
	}
}

// GetAllEntities returns a detached copy (compatibility API).
// Safe to iterate while destroying. Prefer Entities()/Each for hot paths.
func (s *Store[T]) GetAllEntities() []core.Entity {
	result := make([]core.Entity, len(s.entities))
	copy(result, s.entities)
	return result
}

// Entities returns the live dense entity slice — zero allocation.
// CONTRACT: no removals from this store while ranging it; collect
// candidates and remove after the loop (the existing system pattern).
// Additions during the range are visible only via re-checking len.
func (s *Store[T]) Entities() []core.Entity {
	return s.entities
}

// Each iterates entity/component pairs in dense order with direct pointers.
// Return false to stop. Same removal contract as Entities().
// Additions during iteration are not visited (length snapshotted).
func (s *Store[T]) Each(fn func(e core.Entity, c *T) bool) {
	n := len(s.entities)
	for i := range n {
		if !fn(s.entities[i], &s.dense[i]) {
			return
		}
	}
}

// CountEntities returns number of entities with this component
func (s *Store[T]) CountEntities() int {
	return len(s.entities)
}

// ClearAllComponents removes all components, retaining capacity
func (s *Store[T]) ClearAllComponents() {
	s.index.clear()
	clear(s.dense) // zero elements: release inner pointers
	s.dense = s.dense[:0]
	s.entities = s.entities[:0]
}

// storeCopy is a store's contents detached from its world, in dense order, which
// is the order its systems visit it in.
type storeCopy[T any] struct {
	dense    []T
	entities []core.Entity
}

// copyOut detaches the contents into reuse's storage, which nothing may read after.
func (s *Store[T]) copyOut(reuse storeCopy[T]) storeCopy[T] {
	c := storeCopy[T]{dense: sized(reuse.dense, len(s.dense), len(s.dense)), entities: append(reuse.entities[:0], s.entities...)}
	for i, v := range s.dense {
		c.dense[i] = DetachSnapshotValue(v)
	}
	return c
}

// copyIn replaces the contents with c, layout included; the caller restores masks.
func (s *Store[T]) copyIn(c storeCopy[T]) {
	s.ClearAllComponents()
	for i, e := range c.entities {
		s.index.set(e, int32(i))
		s.dense = append(s.dense, DetachSnapshotValue(c.dense[i]))
		s.entities = append(s.entities, e)
	}
}

const (
	indexPageSize  = 1 << parameter.StoreIndexPageBits
	indexPageMask  = indexPageSize - 1
	indexPageBytes = int(unsafe.Sizeof([indexPageSize]int32{}))
	indexRefBytes  = int(unsafe.Sizeof(indexRef{}))

	// indexDomainBits are the bits a valid domain sets in an entity. Clearing them
	// leaves any other domain's ID past every page table.
	indexDomainBits = core.Entity((core.EntityIDMax + 1) * uint64(core.DomainCount-1))
)

// get masks a domain into its page table, which needs DomainCount a power of two
var _ = [1]struct{}{}[core.DomainCount&(core.DomainCount-1)]

type indexRef struct {
	slot *[indexPageSize]int32 // dense slot + 1, zero absent; emptyPage when none is held
	live int32
}

// emptyPage stands in for every page not held, so a lookup needs no nil check
var emptyPage [indexPageSize]int32

// sparseIndex maps an entity to its dense slot through a page table per domain
// indexed by the entity's ID. A page is freed when its last entity leaves, so memory
// follows the live IDs. Table and pages stay within a byte bound of the entities
// indexed; an entity the bound refuses a page, an ID a peer chose, is kept in far.
type sparseIndex struct {
	pages [core.DomainCount][]indexRef
	far   map[core.Entity]int32
	n     int                     // entities indexed, paged or far
	held  int                     // bytes of page table and pages
	spare []*[indexPageSize]int32 // empty pages: the one a release keeps, all a clear frees
}

func (x *sparseIndex) get(e core.Entity) (int32, bool) {
	t := x.pages[e.Domain()&(core.DomainCount-1)]
	if pi := uint64(e&^indexDomainBits) >> parameter.StoreIndexPageBits; pi < uint64(len(t)) {
		if v := t[pi].slot[e&indexPageMask]; v != 0 {
			return v - 1, true
		}
	}
	if x.far == nil {
		return 0, false
	}
	i, ok := x.far[e]
	return i, ok
}

// ref returns e's page table entry, nil where the table does not reach
func (x *sparseIndex) ref(e core.Entity) *indexRef {
	d, pi := e.Domain(), e.ID()>>parameter.StoreIndexPageBits
	if d >= core.DomainCount || pi >= uint64(len(x.pages[d])) {
		return nil
	}
	return &x.pages[d][pi]
}

// set inserts e or rewrites its slot
func (x *sparseIndex) set(e core.Entity, slot int32) {
	r := x.ref(e)
	if r != nil && r.slot[e&indexPageMask] != 0 {
		r.slot[e&indexPageMask] = slot + 1
		return
	}
	if _, ok := x.far[e]; ok {
		x.far[e] = slot
		return
	}
	x.n++
	if r = x.hold(e, r); r == nil {
		if x.far == nil {
			x.far = make(map[core.Entity]int32)
		}
		x.far[e] = slot
		return
	}
	r.slot[e&indexPageMask] = slot + 1
	r.live++
}

// hold returns e's page, growing the table and taking a page if the byte bound
// leaves room; nil when it does not
func (x *sparseIndex) hold(e core.Entity, r *indexRef) *indexRef {
	if r != nil && r.slot != &emptyPage {
		return r
	}
	d, pi := e.Domain(), e.ID()>>parameter.StoreIndexPageBits
	if d >= core.DomainCount {
		return nil
	}
	room := parameter.StoreIndexFloorBytes + x.n*parameter.StoreIndexEntityBytes - x.held - indexPageBytes
	var grow uint64
	if n := uint64(len(x.pages[d])); pi >= n {
		grow = pi + 1 - n
	}
	if room < 0 || grow > uint64(room/indexRefBytes) {
		return nil
	}
	for range grow {
		x.pages[d] = append(x.pages[d], indexRef{slot: &emptyPage})
	}
	r = &x.pages[d][pi]
	if n := len(x.spare); n > 0 {
		r.slot, x.spare = x.spare[n-1], x.spare[:n-1]
	} else {
		r.slot = new([indexPageSize]int32)
	}
	x.held += int(grow)*indexRefBytes + indexPageBytes
	return r
}

// del removes e, which must be indexed
func (x *sparseIndex) del(e core.Entity) {
	x.n--
	if r := x.ref(e); r != nil && r.slot[e&indexPageMask] != 0 {
		r.slot[e&indexPageMask] = 0
		if r.live--; r.live == 0 {
			if len(x.spare) == 0 {
				x.spare = append(x.spare, r.slot)
			}
			r.slot = &emptyPage
			x.held -= indexPageBytes
		}
		return
	}
	if delete(x.far, e); len(x.far) == 0 {
		x.far = nil
	}
}

func (x *sparseIndex) clear() {
	for d := range x.pages {
		for _, r := range x.pages[d] {
			if r.slot != &emptyPage {
				clear(r.slot[:])
				x.spare = append(x.spare, r.slot)
			}
		}
		clear(x.pages[d])
		x.pages[d] = x.pages[d][:0]
	}
	x.far, x.n, x.held = nil, 0, 0
}
