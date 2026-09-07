package status

import "sync/atomic"

// Per-slot metrics and the bare key that mirrors one of them.
//
// The bare key — energy.current, heat.current, shield.active — is what a config,
// a status bar or an operator means by "the player". With one cursor per instance
// that was slot zero and the two were the same thing. With a roster they are not:
// slot zero is the coordinator's cursor, so on every guest the bare key named a
// cursor that instance does not author and never publishes, and it sat still for
// the whole session while the value it was supposed to describe moved.
//
// So the mirror follows the roster's local binding instead, which is the reading
// that makes the bare key mean the same thing it always meant. Two consequences
// are deliberate:
//
//   - The bare key is instance-local by construction. It is a presentation and
//     operator value and must never be a shared FSM guard input (D-20); a guard
//     that has to name a cursor names its slot.
//   - A participant that drives no cursor — a dedicated host — mirrors nothing,
//     and the bare key stays at its reset value rather than borrowing a guest's.

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
