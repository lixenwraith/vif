package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// createAudioCommand creates an audio command for sound playback
func createAudioCommand(soundType audio.SoundType, timestamp time.Time, frameNumber uint64) audio.AudioCommand {
	return audio.AudioCommand{
		Type:       soundType,
		Priority:   1,
		Generation: frameNumber,
		Timestamp:  timestamp,
	}
}

// EnergySystem handles character typing and energy calculation
type EnergySystem struct {
	ctx            *engine.GameContext
	lastCorrect    time.Time
	errorCursorSet bool
}

// NewEnergySystem creates a new energy system
func NewEnergySystem(ctx *engine.GameContext) *EnergySystem {
	return &EnergySystem{
		ctx:         ctx,
		lastCorrect: time.Time{},
	}
}

// Priority returns the system's priority
func (s *EnergySystem) Priority() int {
	return constants.PriorityEnergy
}

// Update runs the energy system
func (s *EnergySystem) Update(world *engine.World, dt time.Duration) {
	// Fetch resources
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	// Clear error flash (red cursor) after timeout using Game Time
	// This ensures the red flash "freezes" if the game is paused
	cursor, ok := world.Cursors.Get(s.ctx.CursorEntity)
	if ok && cursor.ErrorFlashEnd > 0 && now.UnixNano() >= cursor.ErrorFlashEnd {
		cursor.ErrorFlashEnd = 0
		world.Cursors.Add(s.ctx.CursorEntity, cursor)
	}

	// Clear energy blink (background color flash) after timeout using Game Time
	// This ensures the success flash "freezes" if the game is paused
	if s.ctx.State.GetEnergyBlinkActive() && now.Sub(s.ctx.State.GetEnergyBlinkTime()) > constants.EnergyBlinkTimeout {
		s.ctx.State.SetEnergyBlinkActive(false)
	}

	energy := s.ctx.State.GetEnergy()
	shieldActive := s.ctx.State.GetShieldActive()
	// Evaluate and set shield activation state
	if energy > 0 && !shieldActive {
		s.ctx.PushEvent(events.EventShieldActivate, nil, now)
	} else if energy <= 0 && shieldActive {
		s.ctx.PushEvent(events.EventShieldDeactivate, nil, now)
	}
}

// handleCharacterTyping processes a character typed in insert mode
func (s *EnergySystem) handleCharacterTyping(world *engine.World, cursorX, cursorY int, typedRune rune) {
	// Fetch resources
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime
	frameNumber := uint64(s.ctx.State.GetFrameNumber())

	// Find interactable entity at cursor position using z-index filtered lookup
	entity := world.Positions.GetTopEntityFiltered(cursorX, cursorY, world, func(e engine.Entity) bool {
		return engine.IsInteractable(world, e)
	})

	if entity == 0 {
		// No character at cursor - flash error cursor and deactivate boost
		cursor, _ := world.Cursors.Get(s.ctx.CursorEntity)
		cursor.ErrorFlashEnd = now.Add(constants.ErrorBlinkTimeout).UnixNano()
		world.Cursors.Add(s.ctx.CursorEntity, cursor)
		s.ctx.State.SetHeat(0) // Reset heat
		s.ctx.State.SetBoostEnabled(false)
		s.ctx.State.SetBoostColor(0) // 0 = None
		// Set energy blink to error state (black background with bright red text)
		s.ctx.State.SetEnergyBlinkActive(true)
		s.ctx.State.SetEnergyBlinkType(0)  // 0 = error
		s.ctx.State.SetEnergyBlinkLevel(0) // 0 = dark
		s.ctx.State.SetEnergyBlinkTime(now)
		// Trigger error sound
		if s.ctx.AudioEngine != nil {
			cmd := createAudioCommand(audio.SoundError, now, frameNumber)
			s.ctx.AudioEngine.SendRealTime(cmd)
		}
		return
	}

	char, ok := world.Characters.Get(entity)
	if !ok {
		return // Entity has no character component (shouldn't happen for interactable)
	}

	// Check if this is a nugget - handle before sequence logic
	if world.Nuggets.Has(entity) {
		// Handle nugget collection (requires matching character)
		s.handleNuggetCollection(world, entity, char, typedRune)
		return
	}

	// Get sequence component
	seq, ok := world.Sequences.Get(entity)
	if !ok {
		cursor, _ := world.Cursors.Get(s.ctx.CursorEntity)
		cursor.ErrorFlashEnd = now.Add(constants.ErrorBlinkTimeout).UnixNano()
		world.Cursors.Add(s.ctx.CursorEntity, cursor)
		s.ctx.State.SetHeat(0)
		s.ctx.State.SetBoostEnabled(false)
		s.ctx.State.SetBoostColor(0)
		// Set energy blink to error state (black background with bright red text)
		s.ctx.State.SetEnergyBlinkActive(true)
		s.ctx.State.SetEnergyBlinkType(0)  // 0 = error
		s.ctx.State.SetEnergyBlinkLevel(0) // 0 = dark
		s.ctx.State.SetEnergyBlinkTime(now)
		// Trigger error sound
		if s.ctx.AudioEngine != nil {
			cmd := createAudioCommand(audio.SoundError, now, frameNumber)
			s.ctx.AudioEngine.SendRealTime(cmd)
		}
		return
	}

	// Check if this is a gold sequence character
	goldState := s.ctx.State.ReadGoldState(now)
	if seq.Type == components.SequenceGold && goldState.Active {
		// Handle gold sequence typing
		s.handleGoldSequenceTyping(world, entity, char, seq, typedRune)
		return
	}

	// Check if typed character matches
	if char.Rune == typedRune {
		// Track last typed sequence for shield color (Blue, Green, Red, Gold)
		var seqTypeCode int32
		switch seq.Type {
		case components.SequenceBlue:
			seqTypeCode = 1
		case components.SequenceGreen:
			seqTypeCode = 2
		case components.SequenceRed:
			seqTypeCode = 3
		case components.SequenceGold:
			seqTypeCode = 4
		}
		if seqTypeCode != 0 {
			s.ctx.State.SetLastTypedSeqType(seqTypeCode)
		}

		// RED characters reset heat instead of incrementing it
		if seq.Type == components.SequenceRed {
			s.ctx.State.SetHeat(0)
			s.ctx.State.SetBoostEnabled(false)
			s.ctx.State.SetBoostColor(0)
		} else {
			// Blue/Green: Apply heat gain with boost multiplier
			boostState := s.ctx.State.ReadBoostState(timeRes.GameTime)

			heatGain := 1
			if boostState.Enabled {
				heatGain = 2
			}
			s.ctx.State.AddHeat(heatGain)

			// Handle boost activation and maintenance
			currentHeat := s.ctx.State.GetHeat()

			// Convert sequence type to color code: 1=Blue, 2=Green
			var charColorCode int32
			if seq.Type == components.SequenceBlue {
				charColorCode = 1
			} else if seq.Type == components.SequenceGreen {
				charColorCode = 2
			}

			if currentHeat >= constants.MaxHeat {
				if !boostState.Enabled {
					s.ctx.State.SetBoostEnabled(true)
					s.ctx.State.SetBoostColor(charColorCode)
					s.ctx.State.SetBoostEndTime(now.Add(constants.BoostExtensionDuration))
				} else if boostState.Color == charColorCode {
					s.extendBoost(now, constants.BoostExtensionDuration)
				} else {
					s.ctx.State.SetBoostColor(charColorCode)
				}
			}
		}
		s.lastCorrect = now

		// Calculate points: heat value only (no level multiplier)
		points := s.ctx.State.GetHeat()

		// Red characters give negative points
		if seq.Type == components.SequenceRed {
			points = -points
		}

		s.ctx.State.AddEnergy(points)

		// Trigger energy blink with character type and level
		s.ctx.State.SetEnergyBlinkActive(true)
		// Map sequence types to uint32: 0=error, 1=blue, 2=green, 3=red, 4=gold
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
			typeCode = 0 // Error state
		}
		// Map levels to uint32: 0=dark, 1=normal, 2=bright
		var levelCode uint32
		switch seq.Level {
		case components.LevelDark:
			levelCode = 0
		case components.LevelNormal:
			levelCode = 1
		case components.LevelBright:
			levelCode = 2
		}
		s.ctx.State.SetEnergyBlinkType(typeCode)
		s.ctx.State.SetEnergyBlinkLevel(levelCode)
		s.ctx.State.SetEnergyBlinkTime(now)

		// Safely destroy the character entity
		world.DestroyEntity(entity)

		// Trigger splash for successful typing via Event
		splashColor := s.getSplashColorForSequence(seq)
		s.ctx.PushEvent(events.EventSplashRequest, &events.SplashRequestPayload{
			Text:    string(typedRune),
			Color:   splashColor,
			OriginX: cursorX,
			OriginY: cursorY,
		}, now)

		// Move cursor right in ECS
		cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
		if ok {
			if cursorPos.X < config.GameWidth-1 {
				cursorPos.X++
			}
			world.Positions.Add(s.ctx.CursorEntity, cursorPos)
		}

	} else {
		// Incorrect character - flash error cursor, reset heat, and deactivate boost
		cursor, _ := world.Cursors.Get(s.ctx.CursorEntity)
		cursor.ErrorFlashEnd = now.Add(constants.ErrorBlinkTimeout).UnixNano()
		world.Cursors.Add(s.ctx.CursorEntity, cursor)
		s.ctx.State.SetHeat(0)
		s.ctx.State.SetBoostEnabled(false)
		s.ctx.State.SetBoostColor(0) // 0 = None
		// Set energy blink to error state (black background with bright red text)
		s.ctx.State.SetEnergyBlinkActive(true)
		s.ctx.State.SetEnergyBlinkType(0)  // 0 = error
		s.ctx.State.SetEnergyBlinkLevel(0) // 0 = dark
		s.ctx.State.SetEnergyBlinkTime(now)
		// Trigger error sound
		if s.ctx.AudioEngine != nil {
			cmd := createAudioCommand(audio.SoundError, now, frameNumber)
			s.ctx.AudioEngine.SendRealTime(cmd)
		}
	}
}

// extendBoost extends the boost timer by the given duration
func (s *EnergySystem) extendBoost(now time.Time, duration time.Duration) {
	// If boost is already active, add to existing end time; otherwise start fresh
	currentEndTime := s.ctx.State.GetBoostEndTime()
	wasActive := s.ctx.State.GetBoostEnabled() && currentEndTime.After(now)

	if wasActive {
		s.ctx.State.SetBoostEndTime(currentEndTime.Add(duration))
	} else {
		s.ctx.State.SetBoostEndTime(now.Add(duration))
	}

	s.ctx.State.SetBoostEnabled(true)
}

// handleNuggetCollection processes nugget collection (requires typing matching character)
func (s *EnergySystem) handleNuggetCollection(world *engine.World, entity engine.Entity, char components.CharacterComponent, typedRune rune) {
	// Fetch resources
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime
	frameNumber := uint64(s.ctx.State.GetFrameNumber())

	// Check if typed character matches the nugget character
	if char.Rune != typedRune {
		// Incorrect character - flash error cursor and reset heat
		cursor, _ := world.Cursors.Get(s.ctx.CursorEntity)
		cursor.ErrorFlashEnd = now.Add(constants.ErrorBlinkTimeout).UnixNano()
		world.Cursors.Add(s.ctx.CursorEntity, cursor)
		s.ctx.State.SetHeat(0) // Reset heat on incorrect nugget typing
		s.ctx.State.SetBoostEnabled(false)
		s.ctx.State.SetBoostColor(0)
		s.ctx.State.SetEnergyBlinkActive(true)
		s.ctx.State.SetEnergyBlinkType(0)  // 0 = error
		s.ctx.State.SetEnergyBlinkLevel(0) // 0 = dark
		s.ctx.State.SetEnergyBlinkTime(now)
		// Trigger error sound
		if s.ctx.AudioEngine != nil {
			cmd := createAudioCommand(audio.SoundError, now, frameNumber)
			s.ctx.AudioEngine.SendRealTime(cmd)
		}
		return
	}

	// Correct character - collect nugget and add nugget heat with cap
	currentHeat := s.ctx.State.GetHeat()
	newHeat := currentHeat + constants.NuggetHeatIncrease
	if newHeat > constants.MaxHeat {
		newHeat = constants.MaxHeat
	}
	s.ctx.State.SetHeat(newHeat)

	// Spawn directional cleaners if we just hit max heat
	if newHeat >= constants.MaxHeat {
		cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
		if ok {
			payload := &events.DirectionalCleanerPayload{
				OriginX: cursorPos.X,
				OriginY: cursorPos.Y,
			}
			s.ctx.PushEvent(events.EventDirectionalCleanerRequest, payload, now)
		}
	}

	// Destroy the nugget entity
	world.DestroyEntity(entity)

	// Trigger splash for nugget collection via Event
	s.ctx.PushEvent(events.EventSplashRequest, &events.SplashRequestPayload{
		Text:    string(typedRune),
		Color:   components.SplashColorNugget,
		OriginX: config.GameWidth / 2, // Nugget splash doesn't need strict repulsion, but we pass valid coords
		OriginY: config.GameHeight / 2,
	}, now)

	// Clear the active nugget reference to trigger respawn
	// Use CAS to ensure we only clear if this is still the active nugget
	s.ctx.State.ClearActiveNuggetID(uint64(entity))

	// Move cursor right in ECS
	cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
	if ok {
		if cursorPos.X < config.GameWidth-1 {
			cursorPos.X++
		}
		world.Positions.Add(s.ctx.CursorEntity, cursorPos)
	}

	// No energy effects on successful collection
}

// handleGoldSequenceTyping processes typing of gold sequence characters
func (s *EnergySystem) handleGoldSequenceTyping(world *engine.World, entity engine.Entity, char components.CharacterComponent, seq components.SequenceComponent, typedRune rune) {
	// Fetch resources
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	now := timeRes.GameTime
	frameNumber := uint64(s.ctx.State.GetFrameNumber())

	// Check if typed character matches
	if char.Rune != typedRune {
		// Incorrect character - flash error cursor but DON'T reset heat for gold sequence
		cursor, _ := world.Cursors.Get(s.ctx.CursorEntity)
		cursor.ErrorFlashEnd = now.Add(constants.ErrorBlinkTimeout).UnixNano()
		world.Cursors.Add(s.ctx.CursorEntity, cursor)
		s.ctx.State.SetEnergyBlinkActive(true)
		s.ctx.State.SetEnergyBlinkType(0)  // 0 = error
		s.ctx.State.SetEnergyBlinkLevel(0) // 0 = dark
		s.ctx.State.SetEnergyBlinkTime(now)
		// Trigger error sound
		if s.ctx.AudioEngine != nil {
			cmd := createAudioCommand(audio.SoundError, now, frameNumber)
			s.ctx.AudioEngine.SendRealTime(cmd)
		}
		return
	}

	// Correct character - remove entity and move cursor
	s.ctx.State.SetEnergyBlinkActive(true)
	s.ctx.State.SetEnergyBlinkType(4)  // 4 = gold
	s.ctx.State.SetEnergyBlinkLevel(2) // 2 = bright
	s.ctx.State.SetEnergyBlinkTime(now)

	// Safely destroy the character entity
	world.DestroyEntity(entity)

	// Move cursor right
	cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
	if ok {
		if cursorPos.X < config.GameWidth-1 {
			cursorPos.X++
		}
		world.Positions.Add(s.ctx.CursorEntity, cursorPos)
	}

	// Trigger splash for gold character via Event
	s.ctx.PushEvent(events.EventSplashRequest, &events.SplashRequestPayload{
		Text:    string(typedRune),
		Color:   components.SplashColorGold,
		OriginX: cursorPos.X,
		OriginY: cursorPos.Y,
	}, now)

	// Check if this was the last character of the gold sequence
	isLastChar := seq.Index == constants.GoldSequenceLength-1

	if isLastChar {
		// Gold sequence completed! Check if we should trigger cleaners
		currentHeat := s.ctx.State.GetHeat()

		// Request cleaners if heat is already at max
		if currentHeat >= constants.MaxHeat {
			s.ctx.PushEvent(events.EventCleanerRequest, nil, timeRes.GameTime)
		}

		// Fill heat to max (if not already higher)
		if currentHeat < constants.MaxHeat {
			s.ctx.State.SetHeat(constants.MaxHeat)
		}

		// Emit Completion Event (GoldSystem handles cleanup, SplashSystem removes timer)
		s.ctx.PushEvent(events.EventGoldComplete, &events.GoldCompletionPayload{
			SequenceID: seq.ID,
		}, now)
	}
}

// EventTypes returns the event types EnergySystem handles
func (s *EnergySystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventCharacterTyped,
		events.EventEnergyTransaction,
		events.EventDeleteRequest,
	}
}

// HandleEvent processes input-related events from the router
func (s *EnergySystem) HandleEvent(world *engine.World, event events.GameEvent) {
	switch event.Type {
	case events.EventCharacterTyped:
		if payload, ok := event.Payload.(*events.CharacterTypedPayload); ok {
			// Process the event
			s.handleCharacterTyping(world, payload.X, payload.Y, payload.Char)

			// Return payload to pool to reduce allocations
			events.CharacterTypedPayloadPool.Put(payload)
		}

	case events.EventDeleteRequest:
		if payload, ok := event.Payload.(*events.DeleteRequestPayload); ok {
			s.handleDeleteRequest(world, payload, event.Timestamp)
		}

	case events.EventEnergyTransaction:
		if payload, ok := event.Payload.(*events.EnergyTransactionPayload); ok {
			s.ctx.State.AddEnergy(payload.Amount)
		}
	}
}

// handleDeleteRequest processes deletion of entities in a range
func (s *EnergySystem) handleDeleteRequest(world *engine.World, payload *events.DeleteRequestPayload, now time.Time) {
	// Fetch resources
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)

	resetHeat := false
	entitiesToDelete := make([]engine.Entity, 0)

	// Helper to check and mark entity for deletion
	checkEntity := func(entity engine.Entity) {
		if !engine.IsInteractable(world, entity) {
			return
		}

		// Check protection
		if prot, ok := world.Protections.Get(entity); ok {
			if prot.Mask.Has(components.ProtectFromDelete) || prot.Mask == components.ProtectAll {
				return
			}
		}

		// Check sequence type for penalty
		// Red has no penalty, Gold cannot be deleted (via protection), Blue/Green resets heat
		if seq, ok := world.Sequences.Get(entity); ok {
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
		entities := world.Query().With(world.Positions).Execute()
		for _, entity := range entities {
			pos, _ := world.Positions.Get(entity)
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
				cellEntities := world.Positions.GetAllAt(x, y)
				for _, entity := range cellEntities {
					checkEntity(entity)
				}
			}
		}
	}

	// Execute deletion and side effects
	for _, entity := range entitiesToDelete {
		world.DestroyEntity(entity)
	}

	if resetHeat {
		s.ctx.State.SetHeat(0)
	}
}

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