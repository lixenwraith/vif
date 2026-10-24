package engine

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/lixenwraith/vif/internal/core"
)

// RowHash hashes one encoded cell: its entity and its value's JSON.
type RowHash func(core.Entity, []byte) uint64

// RowEncoder keeps every store's last encoding beside an owned copy of each value,
// so a cell whose value its store's generated comparison finds equal keeps its bytes
// and hash. A row whose Value shares its predecessor's backing array is unchanged; a
// store with no change returns the previous slice itself. Callers treat rows as read-only.
type RowEncoder struct {
	mu     sync.Mutex
	stores []storeEncoder
}

// Encode returns every store of s in capture order, in SharedWorldStoreNames order.
// hash must be the same function on every call.
func (r *RowEncoder) Encode(s *SharedWorldState, hash RowHash) ([][]StoreRow, error) {
	return r.encode(hash, func(st storeEncoder) ([]StoreRow, error) { return st.encode(s, hash) })
}

// EncodeLive returns every store of the live world as Encode returns a capture's,
// reading each shared cell where it stands. Caller MUST hold updateMutex.
func (r *RowEncoder) EncodeLive(w *World, hash RowHash) ([][]StoreRow, error) {
	return r.encode(hash, func(st storeEncoder) ([]StoreRow, error) { return st.encodeLive(w, hash) })
}

func (r *RowEncoder) encode(hash RowHash, rows func(storeEncoder) ([]StoreRow, error)) ([][]StoreRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stores == nil {
		r.stores = newStoreEncoders()
	}
	out := make([][]StoreRow, len(r.stores))
	for i, st := range r.stores {
		rows, err := rows(st)
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", SharedWorldStoreNames[i], err)
		}
		out[i] = rows
	}
	return out, nil
}

type storeEncoder interface {
	encode(*SharedWorldState, RowHash) ([]StoreRow, error)
	encodeLive(*World, RowHash) ([]StoreRow, error)
}

// rowEncoder is one store's half of a RowEncoder: values are the owned copies rows
// were encoded from, in the same capture order, and held whether each copy compares
// as its value did. A detach that turns an empty slice nil does not, and is never reused.
type rowEncoder[T any] struct {
	read   func(*SharedWorldState) []StoreEntry[T]
	live   func(*World) ([]T, []core.Entity)
	equal  func(a, b *T) bool
	values []StoreEntry[T]
	held   []bool
	rows   []StoreRow
	picks  []int32 // the live store's shared cells, in its dense order
}

func (c *rowEncoder[T]) encode(s *SharedWorldState, hash RowHash) ([]StoreRow, error) {
	entries := c.read(s)
	return c.encodeRows(len(entries), hash,
		func(i int) core.Entity { return entries[i].Entity }, func(i int) *T { return &entries[i].Value })
}

// encodeLive reads the shared cells of the live store in the order a capture copies
// them, so the rows and their reuse are the capture's.
func (c *rowEncoder[T]) encodeLive(w *World, hash RowHash) ([]StoreRow, error) {
	dense, entities := c.live(w)
	picks := c.picks[:0]
	for i, e := range entities {
		if e.Domain() == core.DomainShared {
			picks = append(picks, int32(i))
		}
	}
	c.picks = picks
	return c.encodeRows(len(picks), hash,
		func(i int) core.Entity { return entities[picks[i]] }, func(i int) *T { return &dense[picks[i]] })
}

// encodeRows encodes n rows read through entity and value against the owned copies.
func (c *rowEncoder[T]) encodeRows(n int, hash RowHash, entity func(int) core.Entity, value func(int) *T) ([]StoreRow, error) {
	old := c.values
	fresh := n != len(old)
	values, held, rows := old, c.held, []StoreRow(nil)
	if fresh {
		values, held, rows = make([]StoreEntry[T], n), make([]bool, n), make([]StoreRow, n)
	}
	// rows stays nil while nothing differs, so an unchanged store returns c.rows itself.
	set := func(i int, row StoreRow, v StoreEntry[T], h bool) {
		if rows == nil {
			rows = slices.Clone(c.rows)
		}
		rows[i], values[i], held[i] = row, v, h
	}
	var moved []int
	for i := range n {
		e, v := entity(i), value(i)
		if i >= len(old) || old[i].Entity != e {
			moved = append(moved, i)
			continue
		}
		if c.held[i] && c.equal(v, &old[i].Value) {
			if fresh {
				set(i, c.rows[i], old[i], true)
			}
			continue
		}
		row, err := encodeRow(e, v, hash)
		if err != nil {
			c.values, c.held, c.rows = nil, nil, nil // some copies may already have moved
			return nil, err
		}
		own, h := c.own(e, v)
		set(i, row, own, h)
	}
	if len(moved) > 0 {
		// Only cells no longer at their old position are indexed: under swap-back
		// removal a handful, and an entity at its old position cannot also be here.
		index := make(map[core.Entity]int, len(moved))
		for j := range old {
			if j >= n || entity(j) != old[j].Entity {
				index[old[j].Entity] = j
			}
		}
		type found struct {
			row StoreRow
			v   StoreEntry[T]
			h   bool
		}
		pending := make([]found, len(moved))
		for k, i := range moved {
			e, v := entity(i), value(i)
			if j, ok := index[e]; ok && c.held[j] && c.equal(v, &old[j].Value) {
				pending[k] = found{c.rows[j], old[j], true}
				continue
			}
			row, err := encodeRow(e, v, hash)
			if err != nil {
				c.values, c.held, c.rows = nil, nil, nil
				return nil, err
			}
			own, h := c.own(e, v)
			pending[k] = found{row, own, h}
		}
		for k, i := range moved {
			set(i, pending[k].row, pending[k].v, pending[k].h)
		}
	}
	if rows == nil {
		return c.rows, nil
	}
	c.values, c.held, c.rows = values, held, rows
	return rows, nil
}

// own copies a value out of the store and reports whether the copy compares as it.
func (c *rowEncoder[T]) own(e core.Entity, v *T) (StoreEntry[T], bool) {
	copied := DetachSnapshotValue(*v)
	return StoreEntry[T]{Entity: e, Value: copied}, c.equal(&copied, v)
}

// encodeRow marshals through a pointer, so a value encodes as it does inside a slice.
func encodeRow[T any](e core.Entity, v *T, hash RowHash) (StoreRow, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return StoreRow{}, err
	}
	return StoreRow{Entity: e, Value: b, Hash: hash(e, b)}, nil
}
