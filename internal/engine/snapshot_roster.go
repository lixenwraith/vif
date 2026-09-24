package engine

import (
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// LocalControl is one instance's pre-install answer to "which shared cursors do I
// simulate", by roster slot, plus the slot its input and camera follow and the
// owner-authored state of every cursor it was authoring.
//
// Three things about a cursor do not travel in a capture, all of them D-13: the
// control assignment, which names the sender rather than the receiver; the
// slot-to-entity roster, which mirrors the cursor store and is re-derived; and the
// owner-authored set below, which the receiver writes and the sender only mirrors.
type LocalControl struct {
	control [parameter.MaxPlayers]component.ControlKind
	held    [parameter.MaxPlayers]bool
	local   uint8
	owned   []ownedCursorState
}

// ownedCursorState is one cursor's owner-authored set, read before a write
// replaces the stores. It is keyed by entity rather than by slot: a shared entity's
// identity is the one thing both instances agree on, and a slot can be reassigned
// by the same capture that carries the state.
type ownedCursorState struct {
	entity core.Entity
	energy owned[component.EnergyComponent]
	heat   owned[component.HeatComponent]
	shield owned[component.ShieldComponent]
	boost  owned[component.BoostComponent]
	weapon owned[component.WeaponComponent]
	combat owned[component.CombatComponent]
	view   owned[component.CursorViewComponent]
	ping   owned[component.PingComponent]
	pulse  owned[component.PulseComponent]
}

// owned is one component as this instance held it, absence included: pulse runs
// only while a disruptor does, and a capture that added one must not leave it
// behind on a cursor whose owner has none.
type owned[T any] struct {
	value T
	held  bool
}

func readOwned[T any](s *Store[T], e core.Entity) owned[T] {
	v, ok := s.GetComponent(e)
	return owned[T]{value: v, held: ok}
}

func writeOwned[T any](s *Store[T], e core.Entity, o owned[T]) {
	if o.held {
		s.SetComponent(e, o.value)
		return
	}
	if s.HasEntity(e) {
		s.RemoveEntity(e)
	}
}

// CaptureCursorControl reads the assignment, and the owner-authored state of every
// cursor this instance authors, before the stores are replaced. Outside a session
// there is no second author to defer to, so nothing is held back.
// Caller MUST hold updateMutex.
func (w *World) CaptureCursorControl() LocalControl {
	var out LocalControl
	out.local = w.Resources.Player.LocalSlot()
	session := w.LocalParticipant() != 0
	w.Components.Cursor.Each(func(e core.Entity, c *component.CursorComponent) bool {
		if int(c.Slot) < parameter.MaxPlayers {
			out.control[c.Slot], out.held[c.Slot] = c.Control, true
		}
		if session && c.Control != component.ControlRemote {
			out.owned = append(out.owned, w.readOwnedCursorState(e))
		}
		return true
	})
	return out
}

// readOwnedCursorState reads one cursor's owner-authored set.
// Caller MUST hold updateMutex.
func (w *World) readOwnedCursorState(e core.Entity) ownedCursorState {
	c := w.Components
	return ownedCursorState{
		entity: e,
		energy: readOwned(c.Energy, e),
		heat:   readOwned(c.Heat, e),
		shield: readOwned(c.Shield, e),
		boost:  readOwned(c.Boost, e),
		weapon: readOwned(c.Weapon, e),
		combat: readOwned(c.Combat, e),
		view:   readOwned(c.CursorView, e),
		ping:   readOwned(c.Ping, e),
		pulse:  readOwned(c.Pulse, e),
	}
}

// restoreOwnedCursorState puts one cursor's owner-authored set back over what the
// capture wrote. Caller MUST hold updateMutex.
func (w *World) restoreOwnedCursorState(s ownedCursorState) {
	c := w.Components
	writeOwned(c.Energy, s.entity, s.energy)
	writeOwned(c.Heat, s.entity, s.heat)
	writeOwned(c.Shield, s.entity, s.shield)
	writeOwned(c.Boost, s.entity, s.boost)
	writeOwned(c.Weapon, s.entity, s.weapon)
	writeOwned(c.Combat, s.entity, s.combat)
	writeOwned(c.CursorView, s.entity, s.view)
	writeOwned(c.Ping, s.entity, s.ping)
	writeOwned(c.Pulse, s.entity, s.pulse)
}

// WithoutLocalCursorState returns s without what RebindCursorRoster re-derives or
// restores: every cursor's control assignment, and the owner-authored cells of each
// one a participant owns, whose carrier is the sync stream. A correction that
// differs only there has moved no shared state; the manifest compares the same.
func (s SharedWorldState) WithoutLocalCursorState() SharedWorldState {
	authored := make(map[core.Entity]bool, len(s.Cursor))
	cursors := make([]StoreEntry[component.CursorComponent], len(s.Cursor))
	for i, en := range s.Cursor {
		if en.Value.PeerID != 0 {
			authored[en.Entity] = true
		}
		en.Value.Control = 0
		cursors[i] = en
	}
	s.Cursor = cursors
	s.Energy = dropOwned(s.Energy, authored)
	s.Heat = dropOwned(s.Heat, authored)
	s.Shield = dropOwned(s.Shield, authored)
	s.Boost = dropOwned(s.Boost, authored)
	s.Weapon = dropOwned(s.Weapon, authored)
	s.Combat = dropOwned(s.Combat, authored)
	s.CursorView = dropOwned(s.CursorView, authored)
	s.Ping = dropOwned(s.Ping, authored)
	s.Pulse = dropOwned(s.Pulse, authored)
	return s
}

func dropOwned[T any](rows []StoreEntry[T], authored map[core.Entity]bool) []StoreEntry[T] {
	out := make([]StoreEntry[T], 0, len(rows))
	for _, r := range rows {
		if !authored[r.Entity] {
			out = append(out, r)
		}
	}
	return out
}

// RebindCursorRoster rebuilds the roster from the installed cursor store, restores
// this instance's own control assignment, and puts back the owner-authored state of
// every cursor that assignment leaves it authoring.
//
// Slots are walked in roster order so the derivation is a function of the capture
// rather than of insertion history. In a session the owner is the participant
// identity the handshake assigned; outside one the assignment already held is kept,
// which for a solo capture is the only participant there is. A cursor keeps this
// instance's owner-authored values exactly when it held them before the write and
// still authors the entity after it, so a joiner arriving into a slot it never held
// adopts the host's template.
//
// Caller MUST hold updateMutex.
func (w *World) RebindCursorRoster(prior LocalControl) {
	roster := w.Resources.Player
	slots := [parameter.MaxPlayers]core.Entity{}
	w.Components.Cursor.Each(func(e core.Entity, c *component.CursorComponent) bool {
		if int(c.Slot) < parameter.MaxPlayers && slots[c.Slot] == 0 {
			slots[c.Slot] = e
		}
		return true
	})

	localID := w.LocalParticipant()
	// The placements this instance has requested are still pending on the barrier
	// after the write; dropping them would snap the local cell back for a lead.
	// They are kept exactly when the same entity is still the cursor it drives.
	driven, queue := roster.Entity, roster.prediction
	roster.Clear()
	for slot := range parameter.MaxPlayers {
		e := slots[slot]
		if e == 0 {
			continue
		}
		if c, ok := w.Components.Cursor.GetPtr(e); ok {
			switch {
			case localID != 0:
				c.Control = component.ControlRemote
				if c.PeerID == localID {
					c.Control = component.ControlHuman
				}
			case prior.held[slot]:
				c.Control = prior.control[slot]
			}
		}
		roster.Bind(uint8(slot), e)
	}
	// Bind only re-points Entity for the slot that was local at the time, and the
	// roster was cleared, so the binding is restored explicitly afterwards.
	roster.SetLocal(prior.local)
	if roster.Entity != 0 && roster.Entity == driven {
		roster.prediction = queue
	}

	for _, held := range prior.owned {
		if w.SimulatesLocally(held.entity) {
			w.restoreOwnedCursorState(held)
		}
	}
}

// DisownCursors leaves this world driving no cursor: every cursor is remote and
// input follows none. A projection world predicts the shared domain for another
// instance and must simulate nothing that instance's own player systems own.
// Caller MUST hold updateMutex.
func (w *World) DisownCursors() {
	w.Components.Cursor.Each(func(_ core.Entity, c *component.CursorComponent) bool {
		c.Control = component.ControlRemote
		return true
	})
	w.Resources.Player.SetLocal(parameter.NoPlayerSlot)
}

// AdoptOwnedCursorState writes another instance's owner-authored cursor values
// over this world's, whoever this world thinks drives them. A projection reads the
// shield and energy the shared species react to as the instance it predicts for
// holds them, not as the authority's stale copy has them.
// Caller MUST hold updateMutex.
func (w *World) AdoptOwnedCursorState(prior LocalControl) {
	for _, held := range prior.owned {
		if w.Components.Cursor.HasEntity(held.entity) {
			w.restoreOwnedCursorState(held)
		}
	}
}
