package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
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
	ctx                *engine.GameContext
	lastCorrect        time.Time
	errorCursorSet     bool
	goldSequenceSystem *GoldSystem
	spawnSystem        *SpawnSystem
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

// SetGoldSystem sets the gold sequence system reference
func (s *EnergySystem) SetGoldSystem(goldSystem *GoldSystem) {
	s.goldSequenceSystem = goldSystem
}

// SetSpawnSystem sets the spawn system reference for color counter updates
func (s *EnergySystem) SetSpawnSystem(spawnSystem *SpawnSystem) {
	s.spawnSystem = spawnSystem
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
	if seq.Type == components.SequenceGold && s.goldSequenceSystem != nil && s.goldSequenceSystem.IsActive() {
		// Handle gold sequence typing
		s.handleGoldSequenceTyping(world, entity, char, seq, typedRune)
		return
	}

	// Check if typed character matches
	if char.Rune == typedRune {
		// Correct character
		// RED characters reset heat instead of incrementing it
		if seq.Type == components.SequenceRed {
			s.ctx.State.SetHeat(0)
			// Red character also deactivates boost
			s.ctx.State.SetBoostEnabled(false)
			s.ctx.State.SetBoostColor(0) // 0 = None
		} else {
			// Read boost state snapshot for consistent checks
			boostState := s.ctx.State.ReadBoostState(timeRes.GameTime)

			// Apply heat gain with boost multiplier
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
				// Heat is at max
				if !boostState.Enabled {
					// Activate boost for the first time
					s.ctx.State.SetBoostEnabled(true)
					s.ctx.State.SetBoostColor(charColorCode)
					s.ctx.State.SetBoostEndTime(now.Add(constants.BoostExtensionDuration))
				} else if boostState.Color == charColorCode {
					// Same color - extend boost timer
					s.extendBoost(now, constants.BoostExtensionDuration)
				} else {
					// Different color - reset boost timer but keep heat at max
					// Set timer to current time (effectively deactivates until rebuilt)
					s.ctx.State.SetBoostEndTime(now)
					s.ctx.State.SetBoostEnabled(false)
					// Update color for potential rebuild
					s.ctx.State.SetBoostColor(charColorCode)
				}
			}
		}
		s.lastCorrect = now

		// Calculate points: increment * level_multiplier * (red?-1:1)
		levelMultipliers := map[components.SequenceLevel]int{
			components.LevelDark:   1,
			components.LevelNormal: 2,
			components.LevelBright: 3,
		}
		levelMult := levelMultipliers[seq.Level]
		points := s.ctx.State.GetHeat() * levelMult

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
			payload := &engine.DirectionalCleanerPayload{
				OriginX: cursorPos.X,
				OriginY: cursorPos.Y,
			}
			s.ctx.PushEvent(engine.EventDirectionalCleanerRequest, payload, now)
		}
	}

	// Destroy the nugget entity
	world.DestroyEntity(entity)

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

	// Correct character - remove entity and move cursor
	// Trigger energy blink with Gold type and level
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

	// Move cursor right
	cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
	if ok {
		if cursorPos.X < config.GameWidth-1 {
			cursorPos.X++
		}
		world.Positions.Add(s.ctx.CursorEntity, cursorPos)
	}

	// Check if this was the last character of the gold sequence
	isLastChar := seq.Index == constants.GoldSequenceLength-1

	if isLastChar {
		// Gold sequence completed! Check if we should trigger cleaners
		currentHeat := s.ctx.State.GetHeat()

		// Request cleaners if heat is already at max
		// Push event to trigger cleaners on next update
		if currentHeat >= constants.MaxHeat {
			s.ctx.PushEvent(engine.EventCleanerRequest, nil, timeRes.GameTime)
		}

		// Fill heat to max (if not already higher)
		if currentHeat < constants.MaxHeat {
			s.ctx.State.SetHeat(constants.MaxHeat)
		}

		// Mark gold sequence as complete
		s.goldSequenceSystem.CompleteGold(world)
	}
}

// EventTypes returns the event types EnergySystem handles
func (s *EnergySystem) EventTypes() []engine.EventType {
	return []engine.EventType{
		engine.EventCharacterTyped,
		engine.EventEnergyTransaction,
	}
}

// HandleEvent processes input-related events from the router
func (s *EnergySystem) HandleEvent(world *engine.World, event engine.GameEvent) {
	switch event.Type {
	case engine.EventCharacterTyped:
		if payload, ok := event.Payload.(*engine.CharacterTypedPayload); ok {
			// Process the event
			s.handleCharacterTyping(world, payload.X, payload.Y, payload.Char)

			// Return payload to pool to reduce allocations
			engine.CharacterTypedPayloadPool.Put(payload)
		}

	case engine.EventEnergyTransaction:
		if payload, ok := event.Payload.(*engine.EnergyTransactionPayload); ok {
			s.ctx.State.AddEnergy(payload.Amount)
		}
	}
}