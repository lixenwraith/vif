// Package app: the roster an install re-derives rather than adopts.
//
// A cursor is a shared entity and its placement, heat and energy are shared
// values, so the whole component travels in a capture. Two things about it do not,
// and both are ordinary D-13: which slot *this* instance drives, and therefore
// which of the shared cursors it simulates. A capture carries the sender's answer
// to that — its own cursor is ControlHuman and everyone else's is ControlRemote —
// and a receiver that adopted it would start simulating the sender's cursor and
// stop simulating its own.
//
// The slot→entity roster is the second half of the same problem. It is a resource
// rather than a store, it mirrors the cursor store exactly, and nothing in an
// install updates it: after the shared entities are replaced it would still name
// the ones that were destroyed. It is not carried, because it is derivable — this
// is the "provably re-derivable from those at install time" clause of D-19, and
// this file is where the derivation lives.
package app

import (
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// localControl is this instance's pre-install answer to "which shared cursors do I
// simulate", by roster slot, plus the slot its input and camera follow.
type localControl struct {
	control [parameter.MaxPlayers]component.ControlKind
	held    [parameter.MaxPlayers]bool
	local   uint8
}

// captureCursorControlLocked reads the assignment before the stores are replaced.
// Caller MUST hold updateMutex.
func (a *App) captureCursorControlLocked() localControl {
	var out localControl
	out.local = a.world.Resources.Player.LocalSlot()
	a.world.Components.Cursor.Each(func(_ core.Entity, c *component.CursorComponent) bool {
		if int(c.Slot) < parameter.MaxPlayers {
			out.control[c.Slot], out.held[c.Slot] = c.Control, true
		}
		return true
	})
	return out
}

// rebindCursorRosterLocked rebuilds the roster from the installed cursor store and
// restores this instance's own control assignment.
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
