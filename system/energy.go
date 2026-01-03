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
			s.addEnergy(int64(payload.Delta))
		}

	case event.EventEnergySet:
		if payload, ok := ev.Payload.(*event.EnergySetPayload); ok {
			s.setEnergy(int64(payload.Value))
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

	dt := s.world.Resource.Time.DeltaTime
	cursorEntity := s.world.Resource.Cursor.Entity

	// Clear error flash after timeout
	cursor, ok := s.world.Component.Cursor.Get(cursorEntity)
	if ok && cursor.ErrorFlashRemaining > 0 {
		cursor.ErrorFlashRemaining -= dt
		if cursor.ErrorFlashRemaining <= 0 {
			cursor.ErrorFlashRemaining = 0
		}
		s.world.Component.Cursor.Set(cursorEntity, cursor)
	}

	// Clear energy blink after timeout
	energyComp, ok := s.world.Component.Energy.Get(cursorEntity)
	if ok && energyComp.BlinkActive.Load() {
		remaining := energyComp.BlinkRemaining.Load() - dt.Nanoseconds()
		if remaining <= 0 {
			remaining = 0
			energyComp.BlinkActive.Store(false)
		}
		energyComp.BlinkRemaining.Store(remaining)
		s.world.Component.Energy.Set(cursorEntity, energyComp)
	}

	// Evaluate shield activation state
	energy := energyComp.Current.Load()
	shield, shieldOk := s.world.Component.Shield.Get(cursorEntity)
	if shieldOk {
		shieldActive := shield.Active
		if energy != 0 && !shieldActive {
			s.world.PushEvent(event.EventShieldActivate, nil)
		} else if energy == 0 && shieldActive {
			s.world.PushEvent(event.EventShieldDeactivate, nil)
		}
	}
}

// addEnergy modifies energy on target entity
// Respects boost immunity: blocks changes that converge toward zero
func (s *EnergySystem) addEnergy(delta int64) {
	cursorEntity := s.world.Resource.Cursor.Entity
	energyComp, ok := s.world.Component.Energy.Get(cursorEntity)
	if !ok {
		return
	}

	// Boost immunity: block convergent drain
	if boost, ok := s.world.Component.Boost.Get(cursorEntity); ok && boost.Active {
		current := energyComp.Current.Load()
		switch {
		case current > 0 && delta < 0:
			return
		case current < 0 && delta > 0:
			return
		}
	}

	energyComp.Current.Add(delta)
	s.world.Component.Energy.Set(cursorEntity, energyComp)
}

// setEnergy sets energy value
func (s *EnergySystem) setEnergy(value int64) {
	cursorEntity := s.world.Resource.Cursor.Entity
	energyComp, ok := s.world.Component.Energy.Get(cursorEntity)
	if !ok {
		return
	}
	energyComp.Current.Store(value)
	s.world.Component.Energy.Set(cursorEntity, energyComp)
}

// startBlink activates blink state
func (s *EnergySystem) startBlink(blinkType, blinkLevel uint32) {
	cursorEntity := s.world.Resource.Cursor.Entity
	energyComp, ok := s.world.Component.Energy.Get(cursorEntity)
	if !ok {
		return
	}
	energyComp.BlinkActive.Store(true)
	energyComp.BlinkType.Store(blinkType)
	energyComp.BlinkLevel.Store(blinkLevel)
	energyComp.BlinkRemaining.Store(constant.EnergyBlinkTimeout.Nanoseconds())
	s.world.Component.Energy.Set(cursorEntity, energyComp)
}

// stopBlink clears blink state
func (s *EnergySystem) stopBlink() {
	cursorEntity := s.world.Resource.Cursor.Entity
	energyComp, ok := s.world.Component.Energy.Get(cursorEntity)
	if !ok {
		return
	}
	energyComp.BlinkActive.Store(false)
	energyComp.BlinkRemaining.Store(0)
	s.world.Component.Energy.Set(cursorEntity, energyComp)
}

// triggerEnergyBlink pushes blink event
func (s *EnergySystem) triggerEnergyBlink(blinkType, blinkLevel uint32) {
	s.world.PushEvent(event.EventEnergyBlinkStart, &event.EnergyBlinkPayload{
		Type:  blinkType,
		Level: blinkLevel,
	})
}

// TODO: move this to typing system
// handleDeleteRequest processes deletion of entities in a range
func (s *EnergySystem) handleDeleteRequest(payload *event.DeleteRequestPayload) {
	config := s.world.Resource.Config

	entitiesToDelete := make([]core.Entity, 0)

	// Helper to check and mark entity for deletion
	checkEntity := func(entity core.Entity) {
		if !s.world.Component.Glyph.Has(entity) {
			return
		}

		// Check protection
		if prot, ok := s.world.Component.Protection.Get(entity); ok {
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

		// Query all entities to find those in the row range
		entities := s.world.Query().With(s.world.Position).Execute()
		for _, entity := range entities {
			pos, _ := s.world.Position.Get(entity)
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
				cellEntities := s.world.Position.GetAllAt(x, y)
				for _, entity := range cellEntities {
					checkEntity(entity)
				}
			}
		}
	}

	// Batch deletion via DeathSystem (silent)
	if len(entitiesToDelete) > 0 {
		event.EmitDeathBatch(s.world.Resource.Event.Queue, 0, entitiesToDelete, s.world.Resource.Time.FrameNumber)
	}
}