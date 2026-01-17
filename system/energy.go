package system

import (
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
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
	return constant.PriorityEnergy
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
			s.addEnergy(int64(payload.Delta), payload.Percentage, payload.Spend, payload.Reward, payload.Convergent)
		}

	case event.EventEnergySetRequest:
		if payload, ok := ev.Payload.(*event.EnergySetPayload); ok {
			s.setEnergy(int64(payload.Value), false)
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
	cursorEntity := s.world.Resources.Cursor.Entity

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
// Spend: bypasses boost protection
// Convergent: clamps at zero, cannot cross
func (s *EnergySystem) addEnergy(delta int64, percentage, spend, reward, convergent bool) {
	cursorEntity := s.world.Resources.Cursor.Entity
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
	// Fast path for typing (Direct modification, no clamps, raw delta)
	if !spend && !convergent && !reward {
		newEnergy = currentEnergy + delta
		goto finalize
	}

	// Early exit for convergent logic on empty energy
	if convergent && currentEnergy == 0 {
		return
	}

	// Boost protection, only applies when converging (draining) without spending (passive drain)
	if convergent && !spend {
		if boost, ok := s.world.Components.Boost.GetComponent(cursorEntity); ok && boost.Active {
			return
		}
	}

	// Check reward first
	if reward {
		if negativeEnergy {
			newEnergy = currentEnergy - absDelta
		} else {
			newEnergy = currentEnergy + absDelta
		}
		goto finalize
	}

	// Apply magnitude reduction based on current sign (Spend and Converge both reduce magnitude)
	if currentEnergy < 0 {
		newEnergy = currentEnergy + absDelta
		// Clamp to 0 if crossed over (convergent only)
		if convergent && newEnergy > 0 {
			newEnergy = 0
		}
	} else {
		newEnergy = currentEnergy - absDelta
		// Clamp to 0 if crossed over (convergent only)
		if convergent && newEnergy < 0 {
			newEnergy = 0
		}
	}

finalize:
	var crossedZero bool
	if newEnergy == 0 || (newEnergy > 0 && negativeEnergy) || (newEnergy < 0 && !negativeEnergy) {
		crossedZero = true
	}

	s.setEnergy(newEnergy, crossedZero)
}

// setEnergy sets energy value
func (s *EnergySystem) setEnergy(value int64, crossedZero bool) {
	cursorEntity := s.world.Resources.Cursor.Entity
	energyComp, ok := s.world.Components.Energy.GetComponent(cursorEntity)
	if !ok {
		return
	}
	energyComp.Current = value
	s.world.Components.Energy.SetComponent(cursorEntity, energyComp)

	// Preventing one frame flickering of shield at zero energy
	if value == 0 {
		s.world.PushEvent(event.EventShieldDeactivate, nil)
	} else if crossedZero {
		s.world.PushEvent(event.EventEnergyCrossedZeroNotification, nil)
	}
}

// handleGlyphConsumed calculates and applies energy from glyph destruction
func (s *EnergySystem) handleGlyphConsumed(glyphType component.GlyphType, _ component.GlyphLevel) {
	cursorEntity := s.world.Resources.Cursor.Entity

	// Fetch current heat
	var heat int
	if heatComp, ok := s.world.Components.Heat.GetComponent(cursorEntity); ok {
		heat = heatComp.Current
	}

	var delta int
	switch glyphType {
	case component.GlyphBlue:
		delta = constant.EnergyBaseBlue * heat
	case component.GlyphGreen:
		delta = constant.EnergyBaseGreen * heat
	case component.GlyphRed:
		delta = constant.EnergyBaseRed * heat
	default:
		return
	}

	s.addEnergy(int64(delta), false, false, false, false)
}

// startBlink activates blink state
func (s *EnergySystem) startBlink(blinkType, blinkLevel int) {
	cursorEntity := s.world.Resources.Cursor.Entity
	energyComp, ok := s.world.Components.Energy.GetComponent(cursorEntity)
	if !ok {
		return
	}
	energyComp.BlinkActive = true
	energyComp.BlinkType = blinkType
	energyComp.BlinkLevel = blinkLevel
	energyComp.BlinkRemaining = constant.EnergyBlinkTimeout
	s.world.Components.Energy.SetComponent(cursorEntity, energyComp)
}

// stopBlink clears blink state
func (s *EnergySystem) stopBlink() {
	cursorEntity := s.world.Resources.Cursor.Entity
	energyComp, ok := s.world.Components.Energy.GetComponent(cursorEntity)
	if !ok {
		return
	}
	energyComp.BlinkActive = false
	energyComp.BlinkRemaining = 0
	s.world.Components.Energy.SetComponent(cursorEntity, energyComp)
}