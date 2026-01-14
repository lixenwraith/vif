package system

import (
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
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

// Priority returns the system's priority
func (s *EnergySystem) Priority() int {
	return constant.PriorityEnergy
}

// EventTypes returns the event types EnergySystem handles
func (s *EnergySystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventEnergyAdd,
		event.EventEnergySet,
		event.EventEnergyGlyphConsumed,
		event.EventEnergyBlinkStart,
		event.EventEnergyBlinkStop,
		event.EventDeleteRequest,
		event.EventGameReset,
	}
}

// HandleEvent processes input-related events from the router
func (s *EnergySystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventDeleteRequest:
		if payload, ok := ev.Payload.(*event.DeleteRequestPayload); ok {
			s.handleDeleteRequest(payload)
		}

	case event.EventEnergyAdd:
		if payload, ok := ev.Payload.(*event.EnergyAddPayload); ok {
			s.addEnergy(int64(payload.Delta), payload.Spend, payload.Convergent)
		}

	case event.EventEnergySet:
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
	cursorEntity := s.world.Resources.Cursor.Entity

	// Clear error flash after timeout
	cursor, ok := s.world.Components.Cursor.GetComponent(cursorEntity)
	if ok && cursor.ErrorFlashRemaining > 0 {
		cursor.ErrorFlashRemaining -= dt
		if cursor.ErrorFlashRemaining <= 0 {
			cursor.ErrorFlashRemaining = 0
		}
		s.world.Components.Cursor.SetComponent(cursorEntity, cursor)
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
	shield, ok := s.world.Components.Shield.GetComponent(cursorEntity)
	if ok {
		shieldActive := shield.Active
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
func (s *EnergySystem) addEnergy(delta int64, spend bool, convergent bool) {
	cursorEntity := s.world.Resources.Cursor.Entity
	energyComp, ok := s.world.Components.Energy.GetComponent(cursorEntity)
	if !ok {
		return
	}

	currentEnergy := energyComp.Current

	// Fast path for typing (Direct modification, no clamps, raw delta)
	// This is the most frequent operation and requires no defensive overhead
	if !spend && !convergent {
		energyComp.Current = currentEnergy + delta
		s.world.Components.Energy.SetComponent(cursorEntity, energyComp)
		return
	}

	// Early exit for convergent logic on empty energy
	if convergent && currentEnergy == 0 {
		return
	}

	// Drain protection (Boost check)
	// Only applies when converging (draining) without spending (passive drain)
	if convergent && !spend {
		if boost, ok := s.world.Components.Boost.GetComponent(cursorEntity); !ok || boost.Active {
			return
		}
	}

	// Calculate defensive absolute magnitude
	absDelta := delta
	if absDelta < 0 {
		absDelta = -absDelta
	}

	var newEnergy int64

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

	energyComp.Current = newEnergy
	s.world.Components.Energy.SetComponent(cursorEntity, energyComp)
}

// setEnergy sets energy value
func (s *EnergySystem) setEnergy(value int64) {
	cursorEntity := s.world.Resources.Cursor.Entity
	energyComp, ok := s.world.Components.Energy.GetComponent(cursorEntity)
	if !ok {
		return
	}
	energyComp.Current = value
	s.world.Components.Energy.SetComponent(cursorEntity, energyComp)
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

	s.addEnergy(int64(delta), false, false)
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

// TODO: move this to typing system
// handleDeleteRequest processes deletion of entities in a range
func (s *EnergySystem) handleDeleteRequest(payload *event.DeleteRequestPayload) {
	config := s.world.Resources.Config

	entitiesToDelete := make([]core.Entity, 0)

	// Helper to check and mark entity for deletion
	checkEntity := func(entity core.Entity) {
		if !s.world.Components.Glyph.HasEntity(entity) {
			return
		}

		// Check protection
		if prot, ok := s.world.Components.Protection.GetComponent(entity); ok {
			if prot.Mask.Has(component.ProtectFromDelete) || prot.Mask == component.ProtectAll {
				return
			}
		}

		entitiesToDelete = append(entitiesToDelete, entity)
	}

	if payload.RangeType == event.DeleteRangeLine {
		// Line deletion (inclusive rows)
		startY, endY := payload.StartY, payload.EndY
		// Ensure normalized order
		if startY > endY {
			startY, endY = endY, startY
		}

		// Query all glyphs to find those in the row range
		entities := s.world.Components.Glyph.AllEntities()
		for _, entity := range entities {
			pos, _ := s.world.Positions.GetPosition(entity)
			if pos.Y >= startY && pos.Y <= endY {
				checkEntity(entity)
			}
		}

	} else {
		// Char deletion (can span multiple lines)
		p1x, p1y := payload.StartX, payload.StartY
		p2x, p2y := payload.EndX, payload.EndY

		// Normalize: P1 should be textually before P2
		if p1y > p2y || (p1y == p2y && p1x > p2x) {
			p1x, p1y, p2x, p2y = p2x, p2y, p1x, p1y
		}

		// Iterate through all rows involved
		for y := p1y; y <= p2y; y++ {
			// Determine X bounds for this row
			minX := 0
			maxX := config.GameWidth - 1

			if y == p1y {
				minX = p1x
			}
			if y == p2y {
				maxX = p2x
			}

			// Optimization: Get entities by cell for the range on this row
			for x := minX; x <= maxX; x++ {
				cellEntities := s.world.Positions.GetAllEntityAt(x, y)
				for _, entity := range cellEntities {
					checkEntity(entity)
				}
			}
		}
	}

	// Batch deletion via DeathSystem (silent)
	if len(entitiesToDelete) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, 0, entitiesToDelete, s.world.Resources.Time.FrameNumber)
	}
}