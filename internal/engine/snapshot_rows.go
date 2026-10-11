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
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stores == nil {
		r.stores = newStoreEncoders()
	}
	out := make([][]StoreRow, len(r.stores))
	for i, st := range r.stores {
		rows, err := st.encode(s, hash)
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", SharedWorldStoreNames[i], err)
		}
		out[i] = rows
	}
	return out, nil
}

type storeEncoder interface {
	encode(*SharedWorldState, RowHash) ([]StoreRow, error)
}

// rowEncoder is one store's half of a RowEncoder: values are the owned copies rows
// were encoded from, in the same capture order, and held whether each copy compares
// as its value did. A detach that turns an empty slice nil does not, and is never reused.
type rowEncoder[T any] struct {
	read   func(*SharedWorldState) []StoreEntry[T]
	equal  func(a, b *T) bool
	values []StoreEntry[T]
	held   []bool
	rows   []StoreRow
}

func (c *rowEncoder[T]) encode(s *SharedWorldState, hash RowHash) ([]StoreRow, error) {
	entries, old := c.read(s), c.values
	fresh := len(entries) != len(old)
	values, held, rows := old, c.held, []StoreRow(nil)
	if fresh {
		values, held, rows = make([]StoreEntry[T], len(entries)), make([]bool, len(entries)), make([]StoreRow, len(entries))
	}
	// rows stays nil while nothing differs, so an unchanged store returns c.rows itself.
	set := func(i int, row StoreRow, v StoreEntry[T], h bool) {
		if rows == nil {
			rows = slices.Clone(c.rows)
		}
		rows[i], values[i], held[i] = row, v, h
	}
	var moved []int
	for i := range entries {
		en := &entries[i]
		if i >= len(old) || old[i].Entity != en.Entity {
			moved = append(moved, i)
			continue
		}
		if c.held[i] && c.equal(&en.Value, &old[i].Value) {
			if fresh {
				set(i, c.rows[i], old[i], true)
			}
			continue
		}
		row, err := encodeRow(en, hash)
		if err != nil {
			c.values, c.held, c.rows = nil, nil, nil // some copies may already have moved
			return nil, err
		}
		v, h := c.own(en)
		set(i, row, v, h)
	}
	if len(moved) > 0 {
		// Only cells no longer at their old position are indexed: under swap-back
		// removal a handful, and an entity at its old position cannot also be here.
		index := make(map[core.Entity]int, len(moved))
		for j := range old {
			if j >= len(entries) || entries[j].Entity != old[j].Entity {
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
			en := &entries[i]
			if j, ok := index[en.Entity]; ok && c.held[j] && c.equal(&en.Value, &old[j].Value) {
				pending[k] = found{c.rows[j], old[j], true}
				continue
			}
			row, err := encodeRow(en, hash)
			if err != nil {
				c.values, c.held, c.rows = nil, nil, nil
				return nil, err
			}
			v, h := c.own(en)
			pending[k] = found{row, v, h}
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

// own copies an entry's value out of the capture and reports whether the copy
// compares as the value.
func (c *rowEncoder[T]) own(en *StoreEntry[T]) (StoreEntry[T], bool) {
	v := DetachSnapshotValue(en.Value)
	return StoreEntry[T]{Entity: en.Entity, Value: v}, c.equal(&v, &en.Value)
}

// encodeRow marshals through a pointer, so a value encodes as it does inside a slice.
func encodeRow[T any](en *StoreEntry[T], hash RowHash) (StoreRow, error) {
	v, err := json.Marshal(&en.Value)
	if err != nil {
		return StoreRow{}, err
	}
	return StoreRow{Entity: en.Entity, Value: v, Hash: hash(en.Entity, v)}, nil
}
