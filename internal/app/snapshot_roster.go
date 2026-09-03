// What an install re-derives or keeps rather than adopts.
//
// A cursor is a shared entity and its placement a shared value, so the component
// travels in a capture. Three things about it do not, and all three are D-13.
//
// Which slot this instance drives, and therefore which shared cursors it simulates.
// A capture carries the sender's answer — its own cursor is ControlHuman, everyone
// else's ControlRemote — and a receiver that adopted it would start simulating the
// sender's cursor and stop simulating its own.
//
// The slot-to-entity roster. It mirrors the cursor store exactly and no install
// updates it, so after the shared entities are replaced it would still name the
// destroyed ones. It is not carried because it is derivable, which is D-19's
// "provably re-derivable at install time" clause; the derivation lives here.
//
// The owner-authored set. Energy, heat, shield, boost, weapon, combat, view, ping
// and pulse have exactly one author — the instance simulating the cursor — and they
// travel as values on their own stream. A capture carries them so a joiner can
// materialise a cursor it has never held, but for a cursor the receiver authors the
// capture holds the sender's mirror of a stream it does not write, one sync period
// behind at best. So the set is read before the stores are replaced and written back
// afterwards, for the cursors this instance still authors once the control
// assignment has been re-derived.
//
// That last rule applies only inside a session: outside one there is no second
// author to defer to, and keeping local values over the capture would make an
// install mean different things depending on who was watching.
// localParticipantLocked is the seam both rules turn on.

package app

import (
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// localControl is this instance's pre-install answer to "which shared cursors do I
// simulate", by roster slot, plus the slot its input and camera follow and the
// owner-authored state of every cursor it was authoring.
type localControl struct {
	control [parameter.MaxPlayers]component.ControlKind
	held    [parameter.MaxPlayers]bool
	local   uint8
	owned   []ownedCursorState
}

// ownedCursorState is one cursor's owner-authored set (D-13), read before a write
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

func readOwned[T any](s *engine.Store[T], e core.Entity) owned[T] {
	v, ok := s.GetComponent(e)
	return owned[T]{value: v, held: ok}
}

func writeOwned[T any](s *engine.Store[T], e core.Entity, o owned[T]) {
	if o.held {
		s.SetComponent(e, o.value)
		return
	}
	if s.HasEntity(e) {
		s.RemoveEntity(e)
	}
}

// captureCursorControlLocked reads the assignment, and the owner-authored state of
// every cursor this instance authors, before the stores are replaced.
// Caller MUST hold updateMutex.
func (a *App) captureCursorControlLocked() localControl {
	var out localControl
	out.local = a.world.Resources.Player.LocalSlot()
	session := a.localParticipantLocked() != 0
	a.world.Components.Cursor.Each(func(e core.Entity, c *component.CursorComponent) bool {
		if int(c.Slot) < parameter.MaxPlayers {
			out.control[c.Slot], out.held[c.Slot] = c.Control, true
		}
		if session && c.Control != component.ControlRemote {
			out.owned = append(out.owned, a.readOwnedCursorStateLocked(e))
		}
		return true
	})
	return out
}

// readOwnedCursorStateLocked reads one cursor's owner-authored set.
// Caller MUST hold updateMutex.
func (a *App) readOwnedCursorStateLocked(e core.Entity) ownedCursorState {
	c := a.world.Components
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

// restoreOwnedCursorStateLocked puts one cursor's owner-authored set back over what
// the capture wrote. Caller MUST hold updateMutex.
func (a *App) restoreOwnedCursorStateLocked(s ownedCursorState) {
	c := a.world.Components
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

// rebindCursorRosterLocked rebuilds the roster from the installed cursor store,
// restores this instance's own control assignment, and puts back the
// owner-authored state of every cursor that assignment leaves it authoring.
//
// Slots are walked in roster order rather than in store order so the derivation is
// a function of the capture and not of insertion history. The control rule has two
// cases and they are the same rule seen from two sides: in a session the owner is
// named by the participant identity the handshake assigned, so a cursor is this
// instance's exactly when the identities match; outside one there is no identity to
// match, so the assignment this instance already held is kept and a slot it did not
// hold takes the captured value — which for a solo capture is the only participant
// there is.
//
// The owner-authored restore runs after that, and reads its answer from it: a
// cursor keeps this instance's values exactly when this instance held them before
// the write *and* still authors the entity after it. A joiner arriving into a slot
// it never held therefore adopts the host's template, which is what creates a
// cursor it has never simulated; a guest being corrected keeps its own (D-13).
//
// Caller MUST hold updateMutex.
func (a *App) rebindCursorRosterLocked(prior localControl) {
	roster := a.world.Resources.Player
	slots := [parameter.MaxPlayers]core.Entity{}
	a.world.Components.Cursor.Each(func(e core.Entity, c *component.CursorComponent) bool {
		if int(c.Slot) < parameter.MaxPlayers && slots[c.Slot] == 0 {
			slots[c.Slot] = e
		}
		return true
	})

	localID := a.localParticipantLocked()
	roster.Clear()
	for slot := range parameter.MaxPlayers {
		e := slots[slot]
		if e == 0 {
			continue
		}
		if c, ok := a.world.Components.Cursor.GetPtr(e); ok {
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

	for _, held := range prior.owned {
		if a.world.SimulatesLocally(held.entity) {
			a.restoreOwnedCursorStateLocked(held)
		}
	}
}

// localParticipantLocked returns this instance's session identity, zero when no
// transport is attached. Caller MUST hold updateMutex.
func (a *App) localParticipantLocked() uint32 {
	r := a.world.Resources.Network
	if r == nil || r.Port == nil {
		return 0
	}
	return r.ParticipantID
}
