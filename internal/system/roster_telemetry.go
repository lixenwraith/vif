package system

import (
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// eachRosterSlot hands fn every roster slot and the cursor in it, zero for an
// empty one.
//
// It is the telemetry counterpart of the D-2 simulation gate, and the two say
// different things. D-2 stops a system *writing* a cursor it does not simulate;
// it says nothing about reporting one. A peer's energy, heat, shield and weapons
// arrive as transported values (D-13) and are as much a fact about this world as
// the ones written here — but every owner-authored publisher wrote only the cursor
// it authored, so a peer's per-slot keys were never published by anybody. On a
// guest that also emptied the bare keys, which mirror one slot: the resource
// telemetry stood still for the whole session while the values it named moved,
// and the passive drain looked stopped when it was running.
//
// An empty slot is reported too, so a departure clears the previous holder's
// numbers instead of leaving them standing.
func eachRosterSlot(w *engine.World, fn func(slot uint8, cursor core.Entity)) {
	roster := w.Resources.Player
	for i := range parameter.MaxPlayers {
		fn(uint8(i), roster.Slot(uint8(i)))
	}
}
