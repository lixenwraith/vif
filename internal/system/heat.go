package system

import (
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/status"
)

// HeatSystem owns HeatComponent mutations; every cursor carries its own heat
type HeatSystem struct {
	world *engine.World

	statCurrent  *status.PlayerInt
	statOverheat *status.PlayerInt
	statAtMax    *status.PlayerBool
	statEmber    *status.PlayerBool
	rejects      rejectionTelemetry

	enabled bool
}

// NewHeatSystem creates a new heat system
func NewHeatSystem(world *engine.World) engine.System {
	s := &HeatSystem{world: world}

	reg := world.Resources.Status
	s.statCurrent = status.NewPlayerInt(reg, parameter.MaxPlayers, "heat.current", "heat.current")
	s.statOverheat = status.NewPlayerInt(reg, parameter.MaxPlayers, "heat.overheat", "heat.overheat")
	s.statAtMax = status.NewPlayerBool(reg, parameter.MaxPlayers, "heat.at_max", "heat.at_max")
	s.statEmber = status.NewPlayerBool(reg, parameter.MaxPlayers, "heat.ember", "heat.ember")
	s.rejects = newRejectionTelemetry(reg, "heat")

	s.Init()
	return s
}

// Init resets session state for a new game
func (s *HeatSystem) Init() {
	s.statCurrent.Reset()
	s.statOverheat.Reset()
	s.statAtMax.Reset()
	s.statEmber.Reset()
	s.rejects.Reset()
	s.enabled = true
}

// Name returns system's name
func (s *HeatSystem) Name() string { return "heat" }

// Priority returns the system's priority
func (s *HeatSystem) Priority() int { return parameter.PriorityHeat }

// Update advances burst flash and ember decay for every cursor
func (s *HeatSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	now := s.world.Resources.Time.GameTime

	s.world.Components.Cursor.Each(func(e core.Entity, _ *component.CursorComponent) bool {
		// Burst flash is local view state and ages even without a heat component
		if view, ok := s.world.Components.CursorView.GetPtr(e); ok && view.BurstFlashRemaining > 0 {
			view.BurstFlashRemaining = max(view.BurstFlashRemaining-dt, 0)
		}

		heatComp, ok := s.world.Components.Heat.GetPtr(e)
		if !ok {
			return true
		}

		// Handle ember decay
		if heatComp.EmberActive && now.Sub(heatComp.EmberDecayTime) >= parameter.EmberDecayInterval {
			heatComp.Current -= parameter.EmberDecayAmount
			heatComp.EmberDecayTime = now

			if heatComp.Current <= 0 {
				heatComp.Current = 0
				heatComp.EmberActive = false
			}
			// Enforce invariant: heat < max → no overheat
			if heatComp.Current < parameter.HeatMax {
				heatComp.Overheat = 0
			}
			s.publish(e, heatComp)
		}
		return true
	})
}

// EventTypes returns the event types HeatSystem handles
func (s *HeatSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventHeatAddRequest,
		event.EventHeatSetRequest,
		event.EventCursorDespawned,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

// HandleEvent processes heat commands, each naming the cursor it acts on
func (s *HeatSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
		s.Init()
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
	}

	if !s.enabled {
		if ev.Type != event.EventMetaSystemCommandRequest {
			s.rejects.disabled.Add(1)
		}
		return
	}

	if ev.Type == event.EventCursorDespawned {
		if p, ok := ev.Payload.(*event.CursorDespawnedPayload); ok {
			s.clearSlot(p.Slot)
		}
		return
	}

	switch ev.Type {
	case event.EventHeatAddRequest:
		if payload, ok := ev.Payload.(*event.HeatAddRequestPayload); ok {
			cursor := s.world.ResolveCursor(payload.Entity)
			if cursor == 0 {
				s.rejects.cursor.Add(1)
				return
			}
			s.addHeat(cursor, payload.Delta)
		}
	case event.EventHeatSetRequest:
		if payload, ok := ev.Payload.(*event.HeatSetRequestPayload); ok {
			cursor := s.world.ResolveCursor(payload.Entity)
			if cursor == 0 {
				s.rejects.cursor.Add(1)
				return
			}
			s.setHeat(cursor, payload.Value)
		}
	}
}

// addHeat applies a delta to one cursor with clamping and overheat rollover
func (s *HeatSystem) addHeat(cursor core.Entity, delta int) {
	heatComp, ok := s.world.Components.Heat.GetPtr(cursor)
	if !ok {
		return
	}

	// Reset overheat if heat penalty
	if delta < 0 {
		heatComp.Overheat = 0
		s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{ID: parameter.Sfx.MetalHit})
	}

	// Update heat, clamp to bounds, accumulate overheat
	newVal := max(0, heatComp.Current+delta)
	if newVal > parameter.HeatMax {
		heatComp.Overheat += newVal - parameter.HeatMax
		newVal = parameter.HeatMax
	}
	heatComp.Current = newVal

	// Trigger and reset overheat if at or above max
	if heatComp.Overheat >= parameter.HeatMaxOverheat {
		heatComp.Overheat = 0
		if view, ok := s.world.Components.CursorView.GetPtr(cursor); ok {
			view.BurstFlashRemaining = parameter.HeatBurstFlashDuration
		}
		heatComp.EmberActive = true
		heatComp.EmberDecayTime = s.world.Resources.Time.GameTime
		s.world.PushEvent(event.EventHeatBurst, &event.HeatBurstPayload{Entity: cursor})
	}

	s.publish(cursor, heatComp)
}

// setHeat writes an absolute value to one cursor with clamping
func (s *HeatSystem) setHeat(cursor core.Entity, value int) {
	heatComp, ok := s.world.Components.Heat.GetPtr(cursor)
	if !ok {
		return
	}

	// Clamp, spilling the excess into overheat
	if value < 0 {
		value = 0
	}
	if value > parameter.HeatMax {
		heatComp.Overheat = value - parameter.HeatMax
		value = parameter.HeatMax
	} else {
		heatComp.Overheat = 0
	}
	heatComp.Current = value

	s.publish(cursor, heatComp)
}

// publish mirrors one cursor's heat into its roster slot
func (s *HeatSystem) publish(cursor core.Entity, heatComp *component.HeatComponent) {
	slot, ok := s.world.CursorSlot(cursor)
	if !ok {
		return
	}
	s.statCurrent.Store(slot, int64(heatComp.Current))
	s.statOverheat.Store(slot, int64(heatComp.Overheat))
	s.statAtMax.Store(slot, heatComp.Current >= parameter.HeatMax)
	s.statEmber.Store(slot, heatComp.EmberActive)
}

// clearSlot zeroes a retired slot's cells
func (s *HeatSystem) clearSlot(slot uint8) {
	s.statCurrent.Store(slot, 0)
	s.statOverheat.Store(slot, 0)
	s.statAtMax.Store(slot, false)
	s.statEmber.Store(slot, false)
}
