package status

import "sync/atomic"

// Per-slot metrics, plus a bare key mirroring the slot this instance drives. Slot
// zero is the coordinator's cursor, so a slot-zero mirror named a cursor no guest
// authors and never published. The bare key is therefore instance-local and must
// never be a shared FSM guard input (D-20); a guard names its slot.

// PlayerInt binds one per-slot int metric and the bare key the local slot mirrors.
type PlayerInt struct {
	slots  []*atomic.Int64
	legacy *atomic.Int64
	local  *atomic.Int32
}

// NewPlayerInt registers player.<slot>.<suffix> for every slot; an empty legacy key skips the mirror
func NewPlayerInt(r *Registry, slots int, suffix, legacy string) *PlayerInt {
	m := &PlayerInt{slots: make([]*atomic.Int64, slots), local: r.localSlot()}
	for i := range slots {
		m.slots[i] = r.Ints.Get(PlayerKey(i, suffix))
	}
	if legacy != "" {
		m.legacy = r.Ints.Get(legacy)
		r.trackPlayer(m)
	}
	return m
}

// Store writes one slot, mirroring the local slot to the legacy key
func (m *PlayerInt) Store(slot uint8, v int64) {
	if int(slot) >= len(m.slots) {
		return
	}
	m.slots[slot].Store(v)
	if m.legacy != nil && int32(slot) == m.local.Load() {
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

// remirror republishes the newly local slot, so a rebind does not leave the bare
// key showing the previous holder until that cursor's next write.
func (m *PlayerInt) remirror(slot int32) {
	if m.legacy == nil {
		return
	}
	if slot < 0 || int(slot) >= len(m.slots) {
		m.legacy.Store(0)
		return
	}
	m.legacy.Store(m.slots[slot].Load())
}

// PlayerBool is the bool counterpart of PlayerInt
type PlayerBool struct {
	slots  []*atomic.Bool
	legacy *atomic.Bool
	local  *atomic.Int32
}

// NewPlayerBool registers player.<slot>.<suffix> for every slot; an empty legacy key skips the mirror
func NewPlayerBool(r *Registry, slots int, suffix, legacy string) *PlayerBool {
	m := &PlayerBool{slots: make([]*atomic.Bool, slots), local: r.localSlot()}
	for i := range slots {
		m.slots[i] = r.Bools.Get(PlayerKey(i, suffix))
	}
	if legacy != "" {
		m.legacy = r.Bools.Get(legacy)
		r.trackPlayer(m)
	}
	return m
}

// Store writes one slot, mirroring the local slot to the legacy key
func (m *PlayerBool) Store(slot uint8, v bool) {
	if int(slot) >= len(m.slots) {
		return
	}
	m.slots[slot].Store(v)
	if m.legacy != nil && int32(slot) == m.local.Load() {
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

// remirror republishes the newly local slot; see PlayerInt.remirror.
func (m *PlayerBool) remirror(slot int32) {
	if m.legacy == nil {
		return
	}
	if slot < 0 || int(slot) >= len(m.slots) {
		m.legacy.Store(false)
		return
	}
	m.legacy.Store(m.slots[slot].Load())
}
