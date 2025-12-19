package system

import (
	"time"

	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// EnergySystem handles character typing and energy calculation
type EnergySystem struct {
	world *engine.World
	res   engine.CoreResources

	energyStore *engine.Store[component.EnergyComponent]
	cursorStore *engine.Store[component.CursorComponent]
	protStore   *engine.Store[component.ProtectionComponent]
	shieldStore *engine.Store[component.ShieldComponent]
	heatStore   *engine.Store[component.HeatComponent]
	boostStore  *engine.Store[component.BoostComponent]
	seqStore    *engine.Store[component.SequenceComponent]
	charStore   *engine.Store[component.CharacterComponent]
	nuggetStore *engine.Store[component.NuggetComponent]

	lastCorrect    time.Time
	errorCursorSet bool
}

// NewEnergySystem creates a new energy system
func NewEnergySystem(world *engine.World) engine.System {
	return &EnergySystem{
		world: world,
		res:   engine.GetCoreResources(world),

		energyStore: engine.GetStore[component.EnergyComponent](world),
		cursorStore: engine.GetStore[component.CursorComponent](world),
		protStore:   engine.GetStore[component.ProtectionComponent](world),
		shieldStore: engine.GetStore[component.ShieldComponent](world),
		heatStore:   engine.GetStore[component.HeatComponent](world),
		boostStore:  engine.GetStore[component.BoostComponent](world),
		seqStore:    engine.GetStore[component.SequenceComponent](world),
		charStore:   engine.GetStore[component.CharacterComponent](world),
		nuggetStore: engine.GetStore[component.NuggetComponent](world),

		lastCorrect: time.Time{},
	}
}

// Init
func (s *EnergySystem) Init() {}

// Priority returns the system's priority
func (s *EnergySystem) Priority() int {
	return constant.PriorityEnergy
}

// EventTypes returns the event types EnergySystem handles
func (s *EnergySystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventCharacterTyped,
		event.EventEnergyAdd,
		event.EventEnergySet,
		event.EventEnergyBlinkStart,
		event.EventEnergyBlinkStop,
		event.EventDeleteRequest,
	}
}

// HandleEvent processes input-related events from the router
func (s *EnergySystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventCharacterTyped:
		if payload, ok := ev.Payload.(*event.CharacterTypedPayload); ok {
			s.handleCharacterTyping(payload.X, payload.Y, payload.Char)
			event.CharacterTypedPayloadPool.Put(payload)
		}

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

// Update runs the energy system
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

// handleCharacterTyping processes a character typed in insert mode
func (s *EnergySystem) handleCharacterTyping(cursorX, cursorY int, typedRune rune) {
	cursorEntity := s.res.Cursor.Entity
	config := s.res.Config
	now := s.res.Time.GameTime

	// Find interactable entity at cursor position using z-index filtered lookup
	entity := s.world.Positions.GetTopEntityFiltered(cursorX, cursorY, func(e core.Entity) bool {
		return s.res.ZIndex.IsInteractable(e)
	})

	if entity == 0 {
		// No character at cursor - flash error cursor and deactivate boost
		s.handleTypingError()
		return
	}

	char, ok := s.charStore.Get(entity)
	if !ok {
		return // Entity has no character component (shouldn't happen for interactable)
	}

	// Check if this is a nugget - handle before sequence logic
	if s.nuggetStore.Has(entity) {
		// Handle nugget collection (requires matching character)
		s.handleNuggetCollection(entity, char, typedRune)
		return
	}

	// Get sequence component
	seq, ok := s.seqStore.Get(entity)
	if !ok {
		s.handleTypingError()
		return
	}

	// Check if this is a gold sequence character
	if seq.Type == component.SequenceGold {
		s.handleGoldSequenceTyping(entity, char, seq, typedRune)
		return
	}

	// Check if typed character matches
	if char.Rune == typedRune {
		// RED characters reset heat and disable boost
		if seq.Type == component.SequenceRed {
			s.world.PushEvent(event.EventHeatSet, &event.HeatSetPayload{Value: 0})
			s.world.PushEvent(event.EventBoostDeactivate, nil)
		} else {
			// Blue/Green: Apply heat gain with boost multiplier
			heatGain := 1

			// TODO: this logic seems wrong now, check
			// Check boost state from component
			boost, ok := s.boostStore.Get(cursorEntity)
			if ok && boost.Active {
				heatGain = 2
			}
			s.world.PushEvent(event.EventHeatAdd, &event.HeatAddPayload{Delta: heatGain})

			// Handle boost activation and maintenance
			currentHeat := s.getHeat()

			if currentHeat >= constant.MaxHeat {
				if ok && !boost.Active {
					// Activate boost
					s.world.PushEvent(event.EventBoostActivate, &event.BoostActivatePayload{
						Duration: constant.BoostBaseDuration,
					})
				} else if ok && boost.Active {
					// Extend boost
					s.world.PushEvent(event.EventBoostExtend, &event.BoostExtendPayload{
						Duration: constant.BoostExtensionDuration,
					})
				}
			}
		}
		s.lastCorrect = now

		// Calculate points: heat value only (no level multiplier)
		points := s.getHeat()

		// Red characters give negative points
		if seq.Type == component.SequenceRed {
			points = -points
		}

		// Trigger energy blink with character type and level
		s.world.PushEvent(event.EventEnergyAdd, &event.EnergyAddPayload{Delta: points})

		// Trigger blink via event
		var typeCode uint32
		switch seq.Type {
		case component.SequenceBlue:
			typeCode = 1
		case component.SequenceGreen:
			typeCode = 2
		case component.SequenceRed:
			typeCode = 3
		case component.SequenceGold:
			typeCode = 4
		default:
			typeCode = 0
		}
		var levelCode uint32
		switch seq.Level {
		case component.LevelDark:
			levelCode = 0
		case component.LevelNormal:
			levelCode = 1
		case component.LevelBright:
			levelCode = 2
		}
		s.triggerEnergyBlink(typeCode, levelCode)

		// Request death (silent)
		s.world.PushEvent(event.EventRequestDeath, &event.DeathRequestPayload{
			Entities:    []core.Entity{entity},
			EffectEvent: 0,
		})

		// Trigger splash for successful typing via Event
		splashColor := s.getSplashColorForSequence(seq)
		s.world.PushEvent(event.EventSplashRequest, &event.SplashRequestPayload{
			Text:    string(typedRune),
			Color:   splashColor,
			OriginX: cursorX,
			OriginY: cursorY,
		})

		// Move cursor right in ECS
		cursorPos, ok := s.world.Positions.Get(cursorEntity)
		if ok {
			if cursorPos.X < config.GameWidth-1 {
				cursorPos.X++
			}
			s.world.Positions.Add(cursorEntity, cursorPos)
		}
	} else {
		// Incorrect character
		s.handleTypingError()
	}
}

// handleTypingError resets heat and boost on typing error
func (s *EnergySystem) handleTypingError() {
	cursorEntity := s.res.Cursor.Entity

	cursor, _ := s.cursorStore.Get(cursorEntity)
	cursor.ErrorFlashRemaining = constant.ErrorBlinkTimeout
	s.cursorStore.Add(cursorEntity, cursor)

	s.world.PushEvent(event.EventHeatSet, &event.HeatSetPayload{Value: 0})
	s.world.PushEvent(event.EventBoostDeactivate, nil)

	s.triggerEnergyBlink(0, 0)

	s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
		SoundType: audio.SoundError,
	})
}

// handleNuggetCollection processes nugget collection (requires typing matching character)
func (s *EnergySystem) handleNuggetCollection(entity core.Entity, char component.CharacterComponent, typedRune rune) {
	cursorEntity := s.res.Cursor.Entity
	config := s.res.Config

	// Check if typed character matches the nugget character
	if char.Rune != typedRune {
		// Incorrect character - flash error cursor, reset heat, deactivate boost
		cursor, _ := s.cursorStore.Get(cursorEntity)
		cursor.ErrorFlashRemaining = constant.ErrorBlinkTimeout
		s.cursorStore.Add(cursorEntity, cursor)
		s.world.PushEvent(event.EventHeatSet, &event.HeatSetPayload{Value: 0})
		s.world.PushEvent(event.EventBoostDeactivate, nil)
		s.triggerEnergyBlink(0, 0)
		s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
			SoundType: audio.SoundError,
		})
		return
	}

	// Correct character - collect nugget and add nugget heat with cap
	currentHeat := s.getHeat()
	newHeat := currentHeat + constant.NuggetHeatIncrease
	if newHeat > constant.MaxHeat {
		newHeat = constant.MaxHeat
	}
	s.world.PushEvent(event.EventHeatSet, &event.HeatSetPayload{Value: newHeat})

	// Spawn directional cleaners if we just hit max heat
	if newHeat >= constant.MaxHeat {
		cursorPos, ok := s.world.Positions.Get(cursorEntity)
		if ok {
			s.world.PushEvent(event.EventDirectionalCleanerRequest, &event.DirectionalCleanerPayload{
				OriginX: cursorPos.X,
				OriginY: cursorPos.Y,
			})
		}
	}

	// Request death (silent)
	s.world.PushEvent(event.EventRequestDeath, &event.DeathRequestPayload{
		Entities:    []core.Entity{entity},
		EffectEvent: 0,
	})

	// Trigger splash for nugget collection via Event
	s.world.PushEvent(event.EventSplashRequest, &event.SplashRequestPayload{
		Text:    string(typedRune),
		Color:   component.SplashColorNugget,
		OriginX: config.GameWidth / 2,
		OriginY: config.GameHeight / 2,
	})

	// Signal nugget collection to NuggetSystem
	s.world.PushEvent(event.EventNuggetCollected, &event.NuggetCollectedPayload{Entity: entity})

	// Move cursor right in ECS
	cursorPos, ok := s.world.Positions.Get(cursorEntity)
	if ok {
		if cursorPos.X < config.GameWidth-1 {
			cursorPos.X++
		}
		s.world.Positions.Add(cursorEntity, cursorPos)
	}

	// No energy effects on successful collection
}

// handleGoldSequenceTyping processes typing of gold sequence characters
func (s *EnergySystem) handleGoldSequenceTyping(entity core.Entity, char component.CharacterComponent, seq component.SequenceComponent, typedRune rune) {
	cursorEntity := s.res.Cursor.Entity
	config := s.res.Config

	// Check if typed character matches
	if char.Rune != typedRune {
		// Incorrect character - flash error cursor but DON'T reset heat for gold sequence
		cursor, _ := s.cursorStore.Get(cursorEntity)
		cursor.ErrorFlashRemaining = constant.ErrorBlinkTimeout
		s.cursorStore.Add(cursorEntity, cursor)
		s.triggerEnergyBlink(0, 0)
		s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
			SoundType: audio.SoundError,
		})
		return
	}

	s.triggerEnergyBlink(4, 2)

	// Request death (silent - gold typing has splash feedback)
	s.world.PushEvent(event.EventRequestDeath, &event.DeathRequestPayload{
		Entities:    []core.Entity{entity},
		EffectEvent: 0,
	})

	// Move cursor right
	cursorPos, ok := s.world.Positions.Get(cursorEntity)
	if ok {
		if cursorPos.X < config.GameWidth-1 {
			cursorPos.X++
		}
		s.world.Positions.Add(cursorEntity, cursorPos)
	}

	// Trigger splash for gold character via Event
	s.world.PushEvent(event.EventSplashRequest, &event.SplashRequestPayload{
		Text:    string(typedRune),
		Color:   component.SplashColorGold,
		OriginX: cursorPos.X,
		OriginY: cursorPos.Y,
	})

	// Check if this was the last character of the gold sequence
	isLastChar := seq.Index == constant.GoldSequenceLength-1

	if isLastChar {
		// Gold sequence completed! Check if we should trigger cleaners
		currentHeat := s.getHeat()

		// Request cleaners if heat is already at max
		if currentHeat >= constant.MaxHeat {
			s.world.PushEvent(event.EventCleanerRequest, nil)
		}

		// Fill heat to max (if not already higher)
		if currentHeat < constant.MaxHeat {
			s.world.PushEvent(event.EventHeatSet, &event.HeatSetPayload{Value: constant.MaxHeat})
		}

		// Emit Completion Event (GoldSystem handles cleanup, SplashSystem removes timer)
		s.world.PushEvent(event.EventGoldComplete, &event.GoldCompletionPayload{
			SequenceID: seq.ID,
		})
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

// handleDeleteRequest processes deletion of entities in a range
func (s *EnergySystem) handleDeleteRequest(payload *event.DeleteRequestPayload) {
	config := s.res.Config

	resetHeat := false
	entitiesToDelete := make([]core.Entity, 0)

	// Use cached ZIndexResolver
	resolver := s.res.ZIndex

	// Helper to check and mark entity for deletion
	checkEntity := func(entity core.Entity) {
		// Use resolver method instead of package function
		if resolver == nil || !resolver.IsInteractable(entity) {
			return
		}

		// Check protection
		if prot, ok := s.protStore.Get(entity); ok {
			if prot.Mask.Has(component.ProtectFromDelete) || prot.Mask == component.ProtectAll {
				return
			}
		}

		// Check sequence type for penalty
		// Red has no penalty, Gold cannot be deleted (via protection), Blue/Green resets heat
		if seq, ok := s.seqStore.Get(entity); ok {
			if seq.Type == component.SequenceGreen || seq.Type == component.SequenceBlue {
				resetHeat = true
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

	// Execute deletion via DeathSystem (silent - delete operator has no flash)
	if len(entitiesToDelete) > 0 {
		s.world.PushEvent(event.EventRequestDeath, &event.DeathRequestPayload{
			Entities:    entitiesToDelete,
			EffectEvent: 0,
		})
	}

	// TODO: should only reset if non-red deletec
	if resetHeat {
		s.world.PushEvent(event.EventHeatSet, &event.HeatSetPayload{Value: 0})
	}
}

// getHeat reads heat value from HeatComponent
func (s *EnergySystem) getHeat() int {
	cursorEntity := s.res.Cursor.Entity
	if hc, ok := s.heatStore.Get(cursorEntity); ok {
		return int(hc.Current.Load())
	}
	return 0
}

// TODO: revisit this mapping
// getSplashColorForSequence returns splash color based on sequence type
func (s *EnergySystem) getSplashColorForSequence(seq component.SequenceComponent) component.SplashColor {
	switch seq.Type {
	case component.SequenceGreen:
		return component.SplashColorGreen
	case component.SequenceBlue:
		return component.SplashColorBlue
	case component.SequenceRed:
		return component.SplashColorRed
	case component.SequenceGold:
		return component.SplashColorGold
	default:
		return component.SplashColorInsert
	}
}