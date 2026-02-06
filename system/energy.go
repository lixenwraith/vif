package system

import (
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// EnergySystem handles character typing and energy calculation
type EnergySystem struct {
	world *engine.World

	lastCorrect    time.Time
	errorCursorSet bool

	enabled bool
}

// NewEnergySystem creates a new energy system
func NewEnergySystem(world *engine.World) engine.System {
	s := &EnergySystem{
		world: world,
	}
	s.Init()
	return s
}

// Init resets session state for new game
func (s *EnergySystem) Init() {
	s.lastCorrect = time.Time{}
	s.errorCursorSet = false
	s.enabled = true
}

// Name returns system's name
func (s *EnergySystem) Name() string {
	return "energy"
}

// Priority returns the system's priority
func (s *EnergySystem) Priority() int {
	return parameter.PriorityEnergy
}

// EventTypes returns the event types EnergySystem handles
func (s *EnergySystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventEnergyAddRequest,
		event.EventEnergySetRequest,
		event.EventEnergyGlyphConsumed,
		event.EventEnergyBlinkStart,
		event.EventEnergyBlinkStop,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

// HandleEvent processes input-related events from the router
func (s *EnergySystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
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
		return
	}

	switch ev.Type {
	case event.EventEnergyAddRequest:
		if payload, ok := ev.Payload.(*event.EnergyAddPayload); ok {
			s.addEnergy(int64(payload.Delta), payload.Percentage, payload.Type)
		}

	case event.EventEnergySetRequest:
		if payload, ok := ev.Payload.(*event.EnergySetPayload); ok {
			s.setEnergy(int64(payload.Value))
		}

	case event.EventEnergyGlyphConsumed:
		if payload, ok := ev.Payload.(*event.GlyphConsumedPayload); ok {
			s.handleGlyphConsumed(payload.Type, payload.Level)
		}

	case event.EventEnergyBlinkStart:
		if payload, ok := ev.Payload.(*event.EnergyBlinkPayload); ok {
			s.startBlink(payload.Type, payload.Level)
		}

	case event.EventEnergyBlinkStop:
		s.stopBlink()
	}
}

// Update manages blink timeout and shield activation state
func (s *EnergySystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	cursorEntity := s.world.Resources.Player.Entity

	// Clear error flash after timeout
	cursorComp, ok := s.world.Components.Cursor.GetComponent(cursorEntity)
	if ok && cursorComp.ErrorFlashRemaining > 0 {
		cursorComp.ErrorFlashRemaining -= dt
		if cursorComp.ErrorFlashRemaining <= 0 {
			cursorComp.ErrorFlashRemaining = 0
		}
		s.world.Components.Cursor.SetComponent(cursorEntity, cursorComp)
	}

	// Clear energy blink after timeout
	energyComp, ok := s.world.Components.Energy.GetComponent(cursorEntity)
	if ok && energyComp.BlinkActive {
		remaining := energyComp.BlinkRemaining - dt
		if remaining <= 0 {
			remaining = 0
			energyComp.BlinkActive = false
		}
		energyComp.BlinkRemaining = remaining
		s.world.Components.Energy.SetComponent(cursorEntity, energyComp)
	}

	// Evaluate shield activation state
	energy := energyComp.Current
	shieldComp, ok := s.world.Components.Shield.GetComponent(cursorEntity)
	if ok {
		shieldActive := shieldComp.Active
		if energy != 0 && !shieldActive {
			s.world.PushEvent(event.EventShieldActivate, nil)
		} else if energy == 0 && shieldActive {
			s.world.PushEvent(event.EventShieldDeactivate, nil)
		}
	}
}

// addEnergy modifies energy on cursor entity
func (s *EnergySystem) addEnergy(delta int64, percentage bool, deltaType event.EnergyDeltaType) {
	cursorEntity := s.world.Resources.Player.Entity
	energyComp, ok := s.world.Components.Energy.GetComponent(cursorEntity)
	if !ok {
		return
	}

	currentEnergy := energyComp.Current

	if percentage {
		// Letting low energy and low percentage to fall to zero
		delta = (delta * currentEnergy) / 100
	}

	if delta == 0 {
		return
	}

	// Calculate defensive absolute magnitude
	absDelta := delta
	if absDelta < 0 {
		absDelta = -absDelta
	}

	negativeEnergy := currentEnergy < 0

	var newEnergy int64
	var crossedZero bool
	switch deltaType {
	case event.EnergyDeltaReward:
		// Absolute value increase, can't cross zero
		if negativeEnergy {
			newEnergy = currentEnergy - absDelta
		} else {
			newEnergy = currentEnergy + absDelta
		}
	case event.EnergyDeltaPenalty:
		// Boost protects from penalties
		if boostComp, ok := s.world.Components.Boost.GetComponent(cursorEntity); ok && boostComp.Active {
			return
		}
		// Convergent to zero and clamps to zero
		if negativeEnergy {
			newEnergy = currentEnergy + absDelta
			if newEnergy > 0 {
				crossedZero = true
				newEnergy = 0
			}
		} else {
			newEnergy = currentEnergy - absDelta
			if newEnergy < 0 {
				crossedZero = true
				newEnergy = 0
			}
		}
	case event.EnergyDeltaSpend:
		// Convergent to zero, can cross zero
		if negativeEnergy {
			newEnergy = currentEnergy + absDelta
			if newEnergy > 0 {
				crossedZero = true
			}
		} else {
			newEnergy = currentEnergy - absDelta
			if newEnergy < 0 {
				crossedZero = true
			}
		}
	}

	energyComp.Current = newEnergy
	s.world.Components.Energy.SetComponent(cursorEntity, energyComp)

	// Preventing one frame flickering of shield at zero energy
	if newEnergy == 0 {
		s.world.PushEvent(event.EventShieldDeactivate, nil)
		s.world.PushEvent(event.EventEnergyCrossedZeroNotification, nil)
		return
	}

	// Signal to remove buffs
	if crossedZero {
		s.world.PushEvent(event.EventEnergyCrossedZeroNotification, nil)
	}
}

// setEnergy sets energy value
func (s *EnergySystem) setEnergy(value int64) {
	cursorEntity := s.world.Resources.Player.Entity
	energyComp, ok := s.world.Components.Energy.GetComponent(cursorEntity)
	if !ok {
		return
	}

	currentEnergy := energyComp.Current
	if (currentEnergy < 0 && value >= 0) || (currentEnergy >= 0 && value < 0) {
		s.world.PushEvent(event.EventEnergyCrossedZeroNotification, nil)
	}
	energyComp.Current = value
	s.world.Components.Energy.SetComponent(cursorEntity, energyComp)

	if value == 0 {
		s.world.PushEvent(event.EventShieldDeactivate, nil)
		s.world.PushEvent(event.EventEnergyCrossedZeroNotification, nil)
	}
}

// handleGlyphConsumed calculates and applies energy from glyph destruction
func (s *EnergySystem) handleGlyphConsumed(glyphType component.GlyphType, _ component.GlyphLevel) {
	cursorEntity := s.world.Resources.Player.Entity

	heatComp, ok := s.world.Components.Heat.GetComponent(cursorEntity)
	if !ok {
		return
	}

	energyComp, ok := s.world.Components.Energy.GetComponent(cursorEntity)
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
	s.world.Components.Energy.SetComponent(cursorEntity, energyComp)

	if newEnergy == 0 {
		s.world.PushEvent(event.EventShieldDeactivate, nil)
		s.world.PushEvent(event.EventEnergyCrossedZeroNotification, nil)
		return
	}

	if (newEnergy > 0 && currentEnergy < 0) || (newEnergy < 0 && currentEnergy > 0) {
		s.world.PushEvent(event.EventEnergyCrossedZeroNotification, nil)
	}
}

// startBlink activates blink state
func (s *EnergySystem) startBlink(blinkType, blinkLevel int) {
	cursorEntity := s.world.Resources.Player.Entity
	energyComp, ok := s.world.Components.Energy.GetComponent(cursorEntity)
	if !ok {
		return
	}
	energyComp.BlinkActive = true
	energyComp.BlinkType = blinkType
	energyComp.BlinkLevel = blinkLevel
	energyComp.BlinkRemaining = parameter.EnergyBlinkTimeout
	s.world.Components.Energy.SetComponent(cursorEntity, energyComp)
}

// stopBlink clears blink state
func (s *EnergySystem) stopBlink() {
	cursorEntity := s.world.Resources.Player.Entity
	energyComp, ok := s.world.Components.Energy.GetComponent(cursorEntity)
	if !ok {
		return
	}
	energyComp.BlinkActive = false
	energyComp.BlinkRemaining = 0
	s.world.Components.Energy.SetComponent(cursorEntity, energyComp)
}