package status

import "sync/atomic"

// PlayerInt binds one per-slot int metric and the bare key slot 0 mirrors.
// The mirror is a migration shim for configs written against a single cursor;
// delete the legacy argument once every reader is slot-aware.
type PlayerInt struct {
	slots  []*atomic.Int64
	legacy *atomic.Int64
}

// NewPlayerInt registers player.<slot>.<suffix> for every slot; an empty legacy key skips the mirror
func NewPlayerInt(r *Registry, slots int, suffix, legacy string) *PlayerInt {
	m := &PlayerInt{slots: make([]*atomic.Int64, slots)}
	for i := range slots {
		m.slots[i] = r.Ints.Get(PlayerKey(i, suffix))
	}
	if legacy != "" {
		m.legacy = r.Ints.Get(legacy)
	}
	return m
}

// Store writes one slot, mirroring slot 0 to the legacy key
func (m *PlayerInt) Store(slot uint8, v int64) {
	if int(slot) >= len(m.slots) {
		return
	}
	m.slots[slot].Store(v)
	if slot == 0 && m.legacy != nil {
		m.legacy.Store(v)
	}
}

// Load reads one slot; an out-of-range slot reads zero
func (m *PlayerInt) Load(slot uint8) int64 {
	if int(slot) >= len(m.slots) {
		return 0
	}
	return m.slots[slot].Load()
}

// Reset zeroes every slot and the legacy mirror
func (m *PlayerInt) Reset() {
	for i := range m.slots {
		m.slots[i].Store(0)
	}
	if m.legacy != nil {
		m.legacy.Store(0)
	}
}

// PlayerBool is the bool counterpart of PlayerInt
type PlayerBool struct {
	slots  []*atomic.Bool
	legacy *atomic.Bool
}

// NewPlayerBool registers player.<slot>.<suffix> for every slot; an empty legacy key skips the mirror
func NewPlayerBool(r *Registry, slots int, suffix, legacy string) *PlayerBool {
	m := &PlayerBool{slots: make([]*atomic.Bool, slots)}
	for i := range slots {
		m.slots[i] = r.Bools.Get(PlayerKey(i, suffix))
	}
	if legacy != "" {
		m.legacy = r.Bools.Get(legacy)
	}
	return m
}

// Store writes one slot, mirroring slot 0 to the legacy key
func (m *PlayerBool) Store(slot uint8, v bool) {
	if int(slot) >= len(m.slots) {
		return
	}
	m.slots[slot].Store(v)
	if slot == 0 && m.legacy != nil {
		m.legacy.Store(v)
	}
}

// Load reads one slot
func (m *PlayerBool) Load(slot uint8) bool {
	if int(slot) >= len(m.slots) {
		return false
	}
	return m.slots[slot].Load()
}

// Reset clears every slot and the legacy mirror
func (m *PlayerBool) Reset() {
	for i := range m.slots {
		m.slots[i].Store(false)
	}
	if m.legacy != nil {
		m.legacy.Store(false)
	}
}
