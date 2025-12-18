package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// EnergySystem handles character typing and energy calculation
type EnergySystem struct {
	world *engine.World
	res   engine.CoreResources

	energyStore *engine.Store[components.EnergyComponent]
	cursorStore *engine.Store[components.CursorComponent]
	protStore   *engine.Store[components.ProtectionComponent]
	shieldStore *engine.Store[components.ShieldComponent]
	heatStore   *engine.Store[components.HeatComponent]
	boostStore  *engine.Store[components.BoostComponent]
	seqStore    *engine.Store[components.SequenceComponent]
	charStore   *engine.Store[components.CharacterComponent]
	nuggetStore *engine.Store[components.NuggetComponent]

	lastCorrect    time.Time
	errorCursorSet bool
}

// NewEnergySystem creates a new energy system
func NewEnergySystem(world *engine.World) engine.System {
	return &EnergySystem{
		world: world,
		res:   engine.GetCoreResources(world),

		energyStore: engine.GetStore[components.EnergyComponent](world),
		cursorStore: engine.GetStore[components.CursorComponent](world),
		protStore:   engine.GetStore[components.ProtectionComponent](world),
		shieldStore: engine.GetStore[components.ShieldComponent](world),
		heatStore:   engine.GetStore[components.HeatComponent](world),
		boostStore:  engine.GetStore[components.BoostComponent](world),
		seqStore:    engine.GetStore[components.SequenceComponent](world),
		charStore:   engine.GetStore[components.CharacterComponent](world),
		nuggetStore: engine.GetStore[components.NuggetComponent](world),

		lastCorrect: time.Time{},
	}
}

// Init
func (s *EnergySystem) Init() {}

// Priority returns the system's priority
func (s *EnergySystem) Priority() int {
	return constants.PriorityEnergy
}

// EventTypes returns the event types EnergySystem handles
func (s *EnergySystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventCharacterTyped,
		events.EventEnergyAdd,
		events.EventEnergySet,
		events.EventEnergyBlinkStart,
		events.EventEnergyBlinkStop,
		events.EventDeleteRequest,
	}
}

// HandleEvent processes input-related events from the router
func (s *EnergySystem) HandleEvent(event events.GameEvent) {
	switch event.Type {
	case events.EventCharacterTyped:
		if payload, ok := event.Payload.(*events.CharacterTypedPayload); ok {
			s.handleCharacterTyping(payload.X, payload.Y, payload.Char)
			events.CharacterTypedPayloadPool.Put(payload)
		}

	case events.EventDeleteRequest:
		if payload, ok := event.Payload.(*events.DeleteRequestPayload); ok {
			s.handleDeleteRequest(payload)
		}

	case events.EventEnergyAdd:
		if payload, ok := event.Payload.(*events.EnergyAddPayload); ok {
			s.addEnergy(int64(payload.Delta))
		}

	case events.EventEnergySet:
		if payload, ok := event.Payload.(*events.EnergySetPayload); ok {
			s.setEnergy(int64(payload.Value))
		}

	case events.EventEnergyBlinkStart:
		if payload, ok := event.Payload.(*events.EnergyBlinkPayload); ok {
			s.startBlink(payload.Type, payload.Level)
		}

	case events.EventEnergyBlinkStop:
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
			s.world.PushEvent(events.EventShieldActivate, nil)
		} else if energy <= 0 && shieldActive {
			s.world.PushEvent(events.EventShieldDeactivate, nil)
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
	if seq.Type == components.SequenceGold {
		s.handleGoldSequenceTyping(entity, char, seq, typedRune)
		return
	}

	// Check if typed character matches
	if char.Rune == typedRune {
		// RED characters reset heat and disable boost
		if seq.Type == components.SequenceRed {
			s.world.PushEvent(events.EventHeatSet, &events.HeatSetPayload{Value: 0})
			s.world.PushEvent(events.EventBoostDeactivate, nil)
		} else {
			// Blue/Green: Apply heat gain with boost multiplier
			heatGain := 1

			// TODO: this logic seems wrong now, check
			// Check boost state from component
			boost, ok := s.boostStore.Get(cursorEntity)
			if ok && boost.Active {
				heatGain = 2
			}
			s.world.PushEvent(events.EventHeatAdd, &events.HeatAddPayload{Delta: heatGain})

			// Handle boost activation and maintenance
			currentHeat := s.getHeat()

			if currentHeat >= constants.MaxHeat {
				if ok && !boost.Active {
					// Activate boost
					s.world.PushEvent(events.EventBoostActivate, &events.BoostActivatePayload{
						Duration: constants.BoostBaseDuration,
					})
				} else if ok && boost.Active {
					// Extend boost
					s.world.PushEvent(events.EventBoostExtend, &events.BoostExtendPayload{
						Duration: constants.BoostExtensionDuration,
					})
				}
			}
		}
		s.lastCorrect = now

		// Calculate points: heat value only (no level multiplier)
		points := s.getHeat()

		// Red characters give negative points
		if seq.Type == components.SequenceRed {
			points = -points
		}

		// Trigger energy blink with character type and level
		s.world.PushEvent(events.EventEnergyAdd, &events.EnergyAddPayload{Delta: points})

		// Trigger blink via event
		var typeCode uint32
		switch seq.Type {
		case components.SequenceBlue:
			typeCode = 1
		case components.SequenceGreen:
			typeCode = 2
		case components.SequenceRed:
			typeCode = 3
		case components.SequenceGold:
			typeCode = 4
		default:
			typeCode = 0
		}
		var levelCode uint32
		switch seq.Level {
		case components.LevelDark:
			levelCode = 0
		case components.LevelNormal:
			levelCode = 1
		case components.LevelBright:
			levelCode = 2
		}
		s.triggerEnergyBlink(typeCode, levelCode)

		// Request death (silent)
		s.world.PushEvent(events.EventRequestDeath, &events.DeathRequestPayload{
			Entities:    []core.Entity{entity},
			EffectEvent: 0,
		})

		// Trigger splash for successful typing via Event
		splashColor := s.getSplashColorForSequence(seq)
		s.world.PushEvent(events.EventSplashRequest, &events.SplashRequestPayload{
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
	cursor.ErrorFlashRemaining = constants.ErrorBlinkTimeout
	s.cursorStore.Add(cursorEntity, cursor)

	s.world.PushEvent(events.EventHeatSet, &events.HeatSetPayload{Value: 0})
	s.world.PushEvent(events.EventBoostDeactivate, nil)

	s.triggerEnergyBlink(0, 0)

	s.world.PushEvent(events.EventSoundRequest, &events.SoundRequestPayload{
		SoundType: audio.SoundError,
	})
}

// handleNuggetCollection processes nugget collection (requires typing matching character)
func (s *EnergySystem) handleNuggetCollection(entity core.Entity, char components.CharacterComponent, typedRune rune) {
	cursorEntity := s.res.Cursor.Entity
	config := s.res.Config

	// Check if typed character matches the nugget character
	if char.Rune != typedRune {
		// Incorrect character - flash error cursor, reset heat, deactivate boost
		cursor, _ := s.cursorStore.Get(cursorEntity)
		cursor.ErrorFlashRemaining = constants.ErrorBlinkTimeout
		s.cursorStore.Add(cursorEntity, cursor)
		s.world.PushEvent(events.EventHeatSet, &events.HeatSetPayload{Value: 0})
		s.world.PushEvent(events.EventBoostDeactivate, nil)
		s.triggerEnergyBlink(0, 0)
		s.world.PushEvent(events.EventSoundRequest, &events.SoundRequestPayload{
			SoundType: audio.SoundError,
		})
		return
	}

	// Correct character - collect nugget and add nugget heat with cap
	currentHeat := s.getHeat()
	newHeat := currentHeat + constants.NuggetHeatIncrease
	if newHeat > constants.MaxHeat {
		newHeat = constants.MaxHeat
	}
	s.world.PushEvent(events.EventHeatSet, &events.HeatSetPayload{Value: newHeat})

	// Spawn directional cleaners if we just hit max heat
	if newHeat >= constants.MaxHeat {
		cursorPos, ok := s.world.Positions.Get(cursorEntity)
		if ok {
			s.world.PushEvent(events.EventDirectionalCleanerRequest, &events.DirectionalCleanerPayload{
				OriginX: cursorPos.X,
				OriginY: cursorPos.Y,
			})
		}
	}

	// Request death (silent)
	s.world.PushEvent(events.EventRequestDeath, &events.DeathRequestPayload{
		Entities:    []core.Entity{entity},
		EffectEvent: 0,
	})

	// Trigger splash for nugget collection via Event
	s.world.PushEvent(events.EventSplashRequest, &events.SplashRequestPayload{
		Text:    string(typedRune),
		Color:   components.SplashColorNugget,
		OriginX: config.GameWidth / 2,
		OriginY: config.GameHeight / 2,
	})

	// Signal nugget collection to NuggetSystem
	s.world.PushEvent(events.EventNuggetCollected, &events.NuggetCollectedPayload{Entity: entity})

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
func (s *EnergySystem) handleGoldSequenceTyping(entity core.Entity, char components.CharacterComponent, seq components.SequenceComponent, typedRune rune) {
	cursorEntity := s.res.Cursor.Entity
	config := s.res.Config

	// Check if typed character matches
	if char.Rune != typedRune {
		// Incorrect character - flash error cursor but DON'T reset heat for gold sequence
		cursor, _ := s.cursorStore.Get(cursorEntity)
		cursor.ErrorFlashRemaining = constants.ErrorBlinkTimeout
		s.cursorStore.Add(cursorEntity, cursor)
		s.triggerEnergyBlink(0, 0)
		s.world.PushEvent(events.EventSoundRequest, &events.SoundRequestPayload{
			SoundType: audio.SoundError,
		})
		return
	}

	s.triggerEnergyBlink(4, 2)

	// Request death (silent - gold typing has splash feedback)
	s.world.PushEvent(events.EventRequestDeath, &events.DeathRequestPayload{
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
	s.world.PushEvent(events.EventSplashRequest, &events.SplashRequestPayload{
		Text:    string(typedRune),
		Color:   components.SplashColorGold,
		OriginX: cursorPos.X,
		OriginY: cursorPos.Y,
	})

	// Check if this was the last character of the gold sequence
	isLastChar := seq.Index == constants.GoldSequenceLength-1

	if isLastChar {
		// Gold sequence completed! Check if we should trigger cleaners
		currentHeat := s.getHeat()

		// Request cleaners if heat is already at max
		if currentHeat >= constants.MaxHeat {
			s.world.PushEvent(events.EventCleanerRequest, nil)
		}

		// Fill heat to max (if not already higher)
		if currentHeat < constants.MaxHeat {
			s.world.PushEvent(events.EventHeatSet, &events.HeatSetPayload{Value: constants.MaxHeat})
		}

		// Emit Completion Event (GoldSystem handles cleanup, SplashSystem removes timer)
		s.world.PushEvent(events.EventGoldComplete, &events.GoldCompletionPayload{
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
	energyComp.BlinkRemaining.Store(constants.EnergyBlinkTimeout.Nanoseconds())
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
	s.world.PushEvent(events.EventEnergyBlinkStart, &events.EnergyBlinkPayload{
		Type:  blinkType,
		Level: blinkLevel,
	})
}

// handleDeleteRequest processes deletion of entities in a range
func (s *EnergySystem) handleDeleteRequest(payload *events.DeleteRequestPayload) {
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
			if prot.Mask.Has(components.ProtectFromDelete) || prot.Mask == components.ProtectAll {
				return
			}
		}

		// Check sequence type for penalty
		// Red has no penalty, Gold cannot be deleted (via protection), Blue/Green resets heat
		if seq, ok := s.seqStore.Get(entity); ok {
			if seq.Type == components.SequenceGreen || seq.Type == components.SequenceBlue {
				resetHeat = true
			}
		}

		entitiesToDelete = append(entitiesToDelete, entity)
	}

	if payload.RangeType == events.DeleteRangeLine {
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
		s.world.PushEvent(events.EventRequestDeath, &events.DeathRequestPayload{
			Entities:    entitiesToDelete,
			EffectEvent: 0,
		})
	}

	// TODO: should only reset if non-red deletec
	if resetHeat {
		s.world.PushEvent(events.EventHeatSet, &events.HeatSetPayload{Value: 0})
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
func (s *EnergySystem) getSplashColorForSequence(seq components.SequenceComponent) components.SplashColor {
	switch seq.Type {
	case components.SequenceGreen:
		return components.SplashColorGreen
	case components.SequenceBlue:
		return components.SplashColorBlue
	case components.SequenceRed:
		return components.SplashColorRed
	case components.SequenceGold:
		return components.SplashColorGold
	default:
		return components.SplashColorInsert
	}
}