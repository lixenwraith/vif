package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/status"
)

// EnergySystem owns EnergyComponent mutations; every cursor carries its own energy
type EnergySystem struct {
	world *engine.World

	// Cycle difficulty scaling: a world property, not a player one
	damageMultiplier int64

	// Per-cursor state
	statCurrent *status.PlayerInt

	// Roster-wide occurrence counters
	statDamageMultiplier *atomic.Int64
	statPenaltyCount     *atomic.Int64
	statRewardCount      *atomic.Int64
	statSpendCount       *atomic.Int64
	statCrossedZeroCount *atomic.Int64
	statPenaltyRejects   *atomic.Int64
	statCursorRejects    *atomic.Int64
	statMissingEnergy    *atomic.Int64
	statDisabled         *atomic.Int64

	enabled bool
}

// NewEnergySystem creates a new energy system
func NewEnergySystem(world *engine.World) engine.System {
	s := &EnergySystem{world: world}

	reg := world.Resources.Status
	s.statCurrent = status.NewPlayerInt(reg, parameter.MaxPlayers, "energy.current", "energy.current")
	s.statDamageMultiplier = reg.Ints.Get("energy.damage_multiplier")
	s.statPenaltyCount = reg.Ints.Get("energy.penalty_count")
	s.statRewardCount = reg.Ints.Get("energy.reward_count")
	s.statSpendCount = reg.Ints.Get("energy.spend_count")
	s.statCrossedZeroCount = reg.Ints.Get("energy.crossed_zero_count")
	s.statPenaltyRejects = reg.Ints.Get("energy.penalty_rejects")
	s.statCursorRejects = reg.Ints.Get("energy.cursor_rejects")
	s.statMissingEnergy = reg.Ints.Get("energy.missing_energy_rejects")
	s.statDisabled = reg.Ints.Get("energy.disabled_rejects")

	s.Init()
	return s
}

// Init resets session state for a new game
func (s *EnergySystem) Init() {
	s.damageMultiplier = 1

	s.statCurrent.Reset()
	s.statDamageMultiplier.Store(1)
	s.statPenaltyCount.Store(0)
	s.statRewardCount.Store(0)
	s.statSpendCount.Store(0)
	s.statCrossedZeroCount.Store(0)
	s.statPenaltyRejects.Store(0)
	s.statCursorRejects.Store(0)
	s.statMissingEnergy.Store(0)
	s.statDisabled.Store(0)

	s.enabled = true
}

// Name returns system's name
func (s *EnergySystem) Name() string { return "energy" }

// Priority returns the system's priority
func (s *EnergySystem) Priority() int { return parameter.PriorityEnergy }

// EventTypes returns the event types EnergySystem handles
func (s *EnergySystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventEnergyAddRequest,
		event.EventEnergySetRequest,
		event.EventEnergyGlyphConsumed,
		event.EventEnergyBlinkStart,
		event.EventEnergyBlinkStop,
		event.EventCycleDamageMultiplierIncrease,
		event.EventCycleDamageMultiplierReset,
		event.EventCursorDespawned,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

// HandleEvent processes energy events; each command names the cursor it acts on
func (s *EnergySystem) HandleEvent(ev event.GameEvent) {
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
		switch ev.Type {
		case event.EventEnergyAddRequest, event.EventEnergySetRequest, event.EventEnergyGlyphConsumed,
			event.EventEnergyBlinkStart, event.EventEnergyBlinkStop:
			s.statDisabled.Add(1)
		}
		return
	}

	// World-scoped events resolve no cursor of their own
	switch ev.Type {
	case event.EventCursorDespawned:
		if p, ok := ev.Payload.(*event.CursorDespawnedPayload); ok {
			s.statCurrent.Store(p.Slot, 0)
		}
		return

	case event.EventCycleDamageMultiplierIncrease:
		s.damageMultiplier *= 2
		s.statDamageMultiplier.Store(s.damageMultiplier)
		return

	case event.EventCycleDamageMultiplierReset:
		s.damageMultiplier = 1
		s.statDamageMultiplier.Store(1)
		return
	}

	switch ev.Type {
	case event.EventEnergyAddRequest:
		if payload, ok := ev.Payload.(*event.EnergyAddPayload); ok {
			cursor := s.world.ResolveCursor(payload.Entity)
			if cursor == 0 {
				s.statCursorRejects.Add(1)
				return
			}
			s.addEnergy(cursor, int64(payload.Delta), payload.Percentage, payload.Type)
		}

	case event.EventEnergySetRequest:
		if payload, ok := ev.Payload.(*event.EnergySetPayload); ok {
			cursor := s.world.ResolveCursor(payload.Entity)
			if cursor == 0 {
				s.statCursorRejects.Add(1)
				return
			}
			s.setEnergy(cursor, int64(payload.Value))
		}

	case event.EventEnergyGlyphConsumed:
		if payload, ok := ev.Payload.(*event.EnergyGlyphConsumedPayload); ok {
			cursor := s.world.ResolveCursor(payload.Entity)
			if cursor == 0 {
				s.statCursorRejects.Add(1)
				return
			}
			s.handleGlyphConsumed(cursor, payload.Type, payload.Level)
		}

	case event.EventEnergyBlinkStart:
		if payload, ok := ev.Payload.(*event.EnergyBlinkPayload); ok {
			cursor := s.world.ResolveCursor(payload.Entity)
			if cursor == 0 {
				s.statCursorRejects.Add(1)
				return
			}
			s.startBlink(cursor, payload.Type, payload.Level)
		}

	case event.EventEnergyBlinkStop:
		if payload, ok := ev.Payload.(*event.EnergyBlinkStopPayload); ok {
			if cursor := s.world.ResolveCursor(payload.Entity); cursor != 0 {
				s.stopBlink(cursor)
			} else {
				s.statCursorRejects.Add(1)
			}
		}
	}
}

// Update ages blink and error flash and reconciles shield state, per cursor
func (s *EnergySystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime

	s.world.Components.Cursor.Each(func(e core.Entity, cursorComp *component.CursorComponent) bool {
		// Clear error flash after timeout
		if cursorComp.ErrorFlashRemaining > 0 {
			cursorComp.ErrorFlashRemaining = max(cursorComp.ErrorFlashRemaining-dt, 0)
		}

		// Clear energy blink after timeout
		var energy int64
		if energyComp, ok := s.world.Components.Energy.GetPtr(e); ok {
			if energyComp.BlinkActive {
				energyComp.BlinkRemaining -= dt
				if energyComp.BlinkRemaining <= 0 {
					energyComp.BlinkRemaining = 0
					energyComp.BlinkActive = false
				}
			}
			energy = energyComp.Current
		}

		// Evaluate shield activation state for this cursor alone
		if shieldComp, ok := s.world.Components.Shield.GetPtr(e); ok {
			if energy != 0 && !shieldComp.Active {
				s.world.PushEvent(event.EventShieldActivate, &event.ShieldActivatePayload{Entity: e})
			} else if energy == 0 && shieldComp.Active {
				s.world.PushEvent(event.EventShieldDeactivate, &event.ShieldDeactivatePayload{Entity: e})
			}
		}
		return true
	})
}

// addEnergy applies a delta to one cursor's energy
func (s *EnergySystem) addEnergy(cursor core.Entity, delta int64, percentage bool, deltaType component.EnergyDeltaType) {
	energyComp, ok := s.world.Components.Energy.GetPtr(cursor)
	if !ok {
		s.statMissingEnergy.Add(1)
		return
	}

	currentEnergy := energyComp.Current

	if percentage {
		delta = (delta * currentEnergy) / 100
		if delta == 0 {
			delta = 1 // Min value clamp
		}
	}

	if delta == 0 {
		return
	}

	// Defensive absolute magnitude; the delta type carries the direction
	absDelta := absI64(delta)

	// Apply cycle damage multiplier to penalties
	if deltaType == component.EnergyDeltaPenalty {
		absDelta *= s.damageMultiplier
	}

	var newEnergy int64
	var crossedZero bool
	switch deltaType {
	case component.EnergyDeltaReward:
		s.statRewardCount.Add(1)
		// Absolute value increase, can't cross zero
		if currentEnergy < 0 {
			newEnergy = currentEnergy - absDelta
		} else {
			newEnergy = currentEnergy + absDelta
		}

	case component.EnergyDeltaPenalty:
		// Boost protects from penalties
		if boostComp, ok := s.world.Components.Boost.GetPtr(cursor); ok && boostComp.Active {
			s.statPenaltyRejects.Add(1)
			return
		}
		// Ember protects from penalties
		if heatComp, ok := s.world.Components.Heat.GetPtr(cursor); ok && heatComp.EmberActive {
			s.statPenaltyRejects.Add(1)
			return
		}
		s.statPenaltyCount.Add(1)
		newEnergy, crossedZero = convergeToZero(currentEnergy, absDelta, true)

	case component.EnergyDeltaPassive:
		// Bypasses ember/boost, convergent clamp to zero
		newEnergy, crossedZero = convergeToZero(currentEnergy, absDelta, true)

	case component.EnergyDeltaSpend:
		s.statSpendCount.Add(1)
		// Convergent to zero, can cross zero
		newEnergy, crossedZero = convergeToZero(currentEnergy, absDelta, false)
	}

	energyComp.Current = newEnergy
	s.publish(cursor, newEnergy)

	// Preventing one frame flickering of shield at zero energy
	if newEnergy == 0 {
		s.world.PushEvent(event.EventShieldDeactivate, &event.ShieldDeactivatePayload{Entity: cursor})
		s.world.PushEvent(event.EventEnergyCrossedZero, &event.EnergyCrossedZeroPayload{Entity: cursor})
		s.statCrossedZeroCount.Add(1)
		return
	}

	// Signal to remove buffs
	if crossedZero {
		s.world.PushEvent(event.EventEnergyCrossedZero, &event.EnergyCrossedZeroPayload{Entity: cursor})
		s.statCrossedZeroCount.Add(1)
	}
}

// convergeToZero moves value toward zero by mag; clamp stops it at zero
func convergeToZero(value, mag int64, clamp bool) (result int64, crossed bool) {
	if value < 0 {
		result = value + mag
		if result > 0 {
			crossed = true
			if clamp {
				result = 0
			}
		}
		return result, crossed
	}
	result = value - mag
	if result < 0 {
		crossed = true
		if clamp {
			result = 0
		}
	}
	return result, crossed
}

// setEnergy writes an absolute value to one cursor
func (s *EnergySystem) setEnergy(cursor core.Entity, value int64) {
	energyComp, ok := s.world.Components.Energy.GetPtr(cursor)
	if !ok {
		return
	}

	currentEnergy := energyComp.Current
	if (currentEnergy < 0 && value > 0) || (currentEnergy >= 0 && value < 0) {
		s.world.PushEvent(event.EventEnergyCrossedZero, &event.EnergyCrossedZeroPayload{Entity: cursor})
		s.statCrossedZeroCount.Add(1)
	}
	if value == 0 {
		s.world.PushEvent(event.EventShieldDeactivate, &event.ShieldDeactivatePayload{Entity: cursor})
		s.world.PushEvent(event.EventEnergyCrossedZero, &event.EnergyCrossedZeroPayload{Entity: cursor})
	}

	energyComp.Current = value
	s.publish(cursor, value)
}

// handleGlyphConsumed applies energy from a glyph destroyed by one cursor
func (s *EnergySystem) handleGlyphConsumed(cursor core.Entity, glyphType component.GlyphType, _ component.GlyphLevel) {
	heatComp, ok := s.world.Components.Heat.GetPtr(cursor)
	if !ok {
		return
	}

	energyComp, ok := s.world.Components.Energy.GetPtr(cursor)
	if !ok {
		return
	}

	heat := heatComp.Current
	var delta int
	switch glyphType {
	case component.GlyphBlue:
		delta = parameter.EnergyBaseBlue * heat
	case component.GlyphGreen:
		delta = parameter.EnergyBaseGreen * heat
	case component.GlyphRed:
		delta = parameter.EnergyBaseRed * heat
	default:
		return
	}

	currentEnergy := energyComp.Current
	newEnergy := currentEnergy + int64(delta)

	energyComp.Current = newEnergy
	s.publish(cursor, newEnergy)

	if newEnergy == 0 {
		s.world.PushEvent(event.EventShieldDeactivate, &event.ShieldDeactivatePayload{Entity: cursor})
		s.world.PushEvent(event.EventEnergyCrossedZero, &event.EnergyCrossedZeroPayload{Entity: cursor})
		return
	}

	if (newEnergy > 0 && currentEnergy < 0) || (newEnergy < 0 && currentEnergy > 0) {
		s.world.PushEvent(event.EventEnergyCrossedZero, &event.EnergyCrossedZeroPayload{Entity: cursor})
	}
}

// startBlink activates blink state on one cursor
func (s *EnergySystem) startBlink(cursor core.Entity, blinkType, blinkLevel int) {
	energyComp, ok := s.world.Components.Energy.GetPtr(cursor)
	if !ok {
		return
	}
	energyComp.BlinkActive = true
	energyComp.BlinkType = blinkType
	energyComp.BlinkLevel = blinkLevel
	energyComp.BlinkRemaining = parameter.EnergyBlinkTimeout
}

// stopBlink clears blink state on one cursor
func (s *EnergySystem) stopBlink(cursor core.Entity) {
	energyComp, ok := s.world.Components.Energy.GetPtr(cursor)
	if !ok {
		return
	}
	energyComp.BlinkActive = false
	energyComp.BlinkRemaining = 0
}

// publish mirrors one cursor's energy into its roster slot
func (s *EnergySystem) publish(cursor core.Entity, value int64) {
	if slot, ok := s.world.CursorSlot(cursor); ok {
		s.statCurrent.Store(slot, value)
	}
}

// absI64 returns the magnitude of v
func absI64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
