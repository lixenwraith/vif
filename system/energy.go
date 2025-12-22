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
	res   engine.Resources

	energyStore *engine.Store[component.EnergyComponent]
	cursorStore *engine.Store[component.CursorComponent]
	protStore   *engine.Store[component.ProtectionComponent]
	shieldStore *engine.Store[component.ShieldComponent]
	heatStore   *engine.Store[component.HeatComponent]
	boostStore  *engine.Store[component.BoostComponent]
	charStore   *engine.Store[component.CharacterComponent]
	nuggetStore *engine.Store[component.NuggetComponent]

	lastCorrect    time.Time
	errorCursorSet bool
}

// NewEnergySystem creates a new energy system
func NewEnergySystem(world *engine.World) engine.System {
	s := &EnergySystem{
		world: world,
		res:   engine.GetResources(world),

		energyStore: engine.GetStore[component.EnergyComponent](world),
		cursorStore: engine.GetStore[component.CursorComponent](world),
		protStore:   engine.GetStore[component.ProtectionComponent](world),
		shieldStore: engine.GetStore[component.ShieldComponent](world),
		heatStore:   engine.GetStore[component.HeatComponent](world),
		boostStore:  engine.GetStore[component.BoostComponent](world),
		charStore:   engine.GetStore[component.CharacterComponent](world),
		nuggetStore: engine.GetStore[component.NuggetComponent](world),
	}
	s.initLocked()
	return s
}

// Init resets session state for new game
func (s *EnergySystem) Init() {
	s.initLocked()
}

// initLocked performs session state reset
func (s *EnergySystem) initLocked() {
	s.lastCorrect = time.Time{}
	s.errorCursorSet = false
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

	case event.EventGameReset:
		s.Init()
	}
}

// Update manages blink timeout and shield activation state
func (s *EnergySystem) Update() {
	dt := s.res.Time.DeltaTime
	cursorEntity := s.res.Cursor.Entity

	// Clear error flash after timeout
	cursor, ok := s.cursorStore.Get(cursorEntity)
	if ok && cursor.ErrorFlashRemaining > 0 {
		cursor.ErrorFlashRemaining -= dt
		if cursor.ErrorFlashRemaining <= 0 {
			cursor.ErrorFlashRemaining = 0
		}
		s.cursorStore.Add(cursorEntity, cursor)
	}

	// Clear energy blink after timeout
	energyComp, ok := s.energyStore.Get(cursorEntity)
	if ok && energyComp.BlinkActive.Load() {
		remaining := energyComp.BlinkRemaining.Load() - dt.Nanoseconds()
		if remaining <= 0 {
			remaining = 0
			energyComp.BlinkActive.Store(false)
		}
		energyComp.BlinkRemaining.Store(remaining)
		s.energyStore.Add(cursorEntity, energyComp)
	}

	// Evaluate shield activation state
	energy := energyComp.Current.Load()
	shield, shieldOk := s.shieldStore.Get(cursorEntity)
	if shieldOk {
		shieldActive := shield.Active
		if energy > 0 && !shieldActive {
			s.world.PushEvent(event.EventShieldActivate, nil)
		} else if energy <= 0 && shieldActive {
			s.world.PushEvent(event.EventShieldDeactivate, nil)
		}
	}
}

// addEnergy modifies energy on target entity
func (s *EnergySystem) addEnergy(delta int64) {
	cursorEntity := s.res.Cursor.Entity
	energyComp, ok := s.energyStore.Get(cursorEntity)
	if !ok {
		return
	}
	energyComp.Current.Add(delta)
	s.energyStore.Add(cursorEntity, energyComp)
}

// setEnergy sets energy value
func (s *EnergySystem) setEnergy(value int64) {
	cursorEntity := s.res.Cursor.Entity
	energyComp, ok := s.energyStore.Get(cursorEntity)
	if !ok {
		return
	}
	energyComp.Current.Store(value)
	s.energyStore.Add(cursorEntity, energyComp)
}

// startBlink activates blink state
func (s *EnergySystem) startBlink(blinkType, blinkLevel uint32) {
	cursorEntity := s.res.Cursor.Entity
	energyComp, ok := s.energyStore.Get(cursorEntity)
	if !ok {
		return
	}
	energyComp.BlinkActive.Store(true)
	energyComp.BlinkType.Store(blinkType)
	energyComp.BlinkLevel.Store(blinkLevel)
	energyComp.BlinkRemaining.Store(constant.EnergyBlinkTimeout.Nanoseconds())
	s.energyStore.Add(cursorEntity, energyComp)
}

// stopBlink clears blink state
func (s *EnergySystem) stopBlink() {
	cursorEntity := s.res.Cursor.Entity
	energyComp, ok := s.energyStore.Get(cursorEntity)
	if !ok {
		return
	}
	energyComp.BlinkActive.Store(false)
	energyComp.BlinkRemaining.Store(0)
	s.energyStore.Add(cursorEntity, energyComp)
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
	config := s.res.Config

	entitiesToDelete := make([]core.Entity, 0)

	// Use cached ZIndexResolver
	resolver := s.res.ZIndex

	// Helper to check and mark entity for deletion
	checkEntity := func(entity core.Entity) {
		if resolver == nil || !resolver.IsTypeable(entity) {
			return
		}

		// Check protection
		if prot, ok := s.protStore.Get(entity); ok {
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
		entities := s.world.Query().With(s.world.Positions).Execute()
		for _, entity := range entities {
			pos, _ := s.world.Positions.Get(entity)
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
				cellEntities := s.world.Positions.GetAllAt(x, y)
				for _, entity := range cellEntities {
					checkEntity(entity)
				}
			}
		}
	}

	// Batch deletion via DeathSystem (silent)
	if len(entitiesToDelete) > 0 {
		event.EmitDeathBatch(s.res.Events.Queue, 0, entitiesToDelete, s.res.Time.FrameNumber)
	}
}