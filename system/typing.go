package system

import (
	"math"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// TypingSystem handles character typing validation and composite member ordering
// Extracted from EnergySystem to support composite entity mechanics
type TypingSystem struct {
	world *engine.World
	res   engine.Resources

	typeableStore *engine.Store[component.TypeableComponent]
	memberStore   *engine.Store[component.MemberComponent]
	headerStore   *engine.Store[component.CompositeHeaderComponent]
	cursorStore   *engine.Store[component.CursorComponent]
	charStore     *engine.Store[component.CharacterComponent]
	nuggetStore   *engine.Store[component.NuggetComponent]
	seqStore      *engine.Store[component.SequenceComponent]
	boostStore    *engine.Store[component.BoostComponent]
	heatStore     *engine.Store[component.HeatComponent]
}

// NewTypingSystem creates a new typing system
func NewTypingSystem(world *engine.World) engine.System {
	return &TypingSystem{
		world: world,
		res:   engine.GetResources(world),

		typeableStore: engine.GetStore[component.TypeableComponent](world),
		memberStore:   engine.GetStore[component.MemberComponent](world),
		headerStore:   engine.GetStore[component.CompositeHeaderComponent](world),
		cursorStore:   engine.GetStore[component.CursorComponent](world),
		charStore:     engine.GetStore[component.CharacterComponent](world),
		nuggetStore:   engine.GetStore[component.NuggetComponent](world),
		seqStore:      engine.GetStore[component.SequenceComponent](world),
		boostStore:    engine.GetStore[component.BoostComponent](world),
		heatStore:     engine.GetStore[component.HeatComponent](world),
	}
}

func (s *TypingSystem) Init() {}

func (s *TypingSystem) Priority() int {
	return constant.PriorityTyping
}

func (s *TypingSystem) Update() {}

func (s *TypingSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventCharacterTyped,
	}
}

func (s *TypingSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type != event.EventCharacterTyped {
		return
	}

	payload, ok := ev.Payload.(*event.CharacterTypedPayload)
	if !ok {
		return
	}

	s.handleTyping(payload.X, payload.Y, payload.Char)
	event.CharacterTypedPayloadPool.Put(payload)
}

// handleTyping processes a typed character at cursor position
func (s *TypingSystem) handleTyping(cursorX, cursorY int, typedRune rune) {
	// Find interactable entity at cursor position
	entity := s.world.Positions.GetTopEntityFiltered(cursorX, cursorY, func(e core.Entity) bool {
		return s.res.ZIndex.IsInteractable(e)
	})

	if entity == 0 {
		s.emitTypingError()
		return
	}

	// 1. Check if this is a composite member
	if member, ok := s.memberStore.Get(entity); ok {
		s.handleCompositeMember(entity, member.AnchorID, typedRune)
		return
	}

	// 2. Check for standalone TypeableComponent
	if typeable, ok := s.typeableStore.Get(entity); ok {
		s.handleTypeable(entity, typeable, typedRune)
		return
	}

	// 3. Legacy SequenceComponent path (SpawnSystem-created blocks)
	// TODO: to be migrated/deprecated
	if s.seqStore.Has(entity) {
		s.handleLegacySequence(entity, typedRune)
		return
	}

	// // EnergySystem still handles SequenceComponent entities during migration
	// s.world.PushEvent(event.EventCharacterTyped, &event.CharacterTypedPayload{
	// 	Char: typedRune,
	// 	X:    cursorX,
	// 	Y:    cursorY,
	// })

	s.emitTypingError()
}

// handleLegacySequence processes legacy SequenceComponent entities
// Migrated from EnergySystem.handleCharacterTyping
func (s *TypingSystem) handleLegacySequence(entity core.Entity, typedRune rune) {
	cursorEntity := s.res.Cursor.Entity

	char, ok := s.charStore.Get(entity)
	if !ok {
		s.emitTypingError()
		return
	}

	seq, ok := s.seqStore.Get(entity)
	if !ok {
		s.emitTypingError()
		return
	}

	// Character match check
	if char.Rune != typedRune {
		s.emitTypingError()
		return
	}

	// Universal: ALL correct typing adds heat
	heatGain := 1

	// Check boost state for double heat gain
	boost, ok := s.boostStore.Get(cursorEntity)
	if ok && boost.Active {
		heatGain = 2 // TODO: const
		s.world.PushEvent(event.EventBoostExtend, &event.BoostExtendPayload{
			Duration: constant.BoostExtensionDuration,
		})
	} else {
		s.world.PushEvent(event.EventBoostActivate, &event.BoostActivatePayload{
			Duration: constant.BoostBaseDuration,
		})
	}
	s.world.PushEvent(event.EventHeatAdd, &event.HeatAddPayload{Delta: heatGain})

	// Energy: positive for Blue/Green, negative for Red
	points := s.getHeat()
	if seq.Type == component.SequenceRed {
		points = -points
	}
	s.world.PushEvent(event.EventEnergyAdd, &event.EnergyAddPayload{Delta: points})

	// Energy blink based on sequence type and level
	var typeCode uint32
	switch seq.Type {
	case component.SequenceBlue:
		typeCode = 1
	case component.SequenceGreen:
		typeCode = 2
	case component.SequenceRed:
		typeCode = 3
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
	s.world.PushEvent(event.EventEnergyBlinkStart, &event.EnergyBlinkPayload{
		Type:  typeCode,
		Level: levelCode,
	})

	p := event.AcquireDeathRequest(0)
	p.Entities = append(p.Entities, entity)
	s.world.PushEvent(event.EventRequestDeath, p)

	// Splash feedback
	var splashColor component.SplashColor
	switch seq.Type {
	case component.SequenceGreen:
		splashColor = component.SplashColorGreen
	case component.SequenceBlue:
		splashColor = component.SplashColorBlue
	case component.SequenceRed:
		splashColor = component.SplashColorRed
	default:
		splashColor = component.SplashColorNormal
	}
	cursorPos, ok := s.world.Positions.Get(cursorEntity)
	if ok {
		s.world.PushEvent(event.EventSplashRequest, &event.SplashRequestPayload{
			Text:    string(typedRune),
			Color:   splashColor,
			OriginX: cursorPos.X,
			OriginY: cursorPos.Y,
		})
	}

	// Move cursor right
	s.moveCursorRight()
}

// getHeat reads current heat value from cursor's HeatComponent
func (s *TypingSystem) getHeat() int {
	cursorEntity := s.res.Cursor.Entity
	if hc, ok := s.heatStore.Get(cursorEntity); ok {
		return int(hc.Current.Load())
	}
	return 0
}

// handleCompositeMemberTyping processes typing for composite member entities
// TODO: are we under lock and can we not pass cursor location? any inaccuracy risk? we have cursor cached and pass it around...
func (s *TypingSystem) handleCompositeMember(entity core.Entity, anchorID core.Entity, typedRune rune) {
	cursorEntity := s.res.Cursor.Entity

	// Get typeable or fall back to character component
	var targetChar rune
	var typeableType component.TypeableType
	var typeableLevel component.TypeableLevel

	if typeable, ok := s.typeableStore.Get(entity); ok {
		targetChar = typeable.Char
		typeableType = typeable.Type
		typeableLevel = typeable.Level
	} else {
		// Fallback to CharacterComponent for migration period
		char, ok := s.charStore.Get(entity)
		if !ok {
			s.emitTypingError()
			return
		}
		targetChar = char.Rune
		// Derive type from CharacterComponent.SeqType
		switch char.SeqType {
		case component.SequenceGold:
			typeableType = component.TypeGold
		case component.SequenceBlue:
			typeableType = component.TypeBlue
		case component.SequenceGreen:
			typeableType = component.TypeGreen
		case component.SequenceRed:
			typeableType = component.TypeRed
		}
		typeableLevel = char.SeqLevel
	}

	// Character match check
	if targetChar != typedRune {
		s.emitTypingError()
		return
	}

	// 1. Identify behavior for reward logic
	header, ok := s.headerStore.Get(anchorID)
	if !ok {
		s.emitTypingError()
		return
	}

	// 2. Validate typing order
	if !s.validateTypingOrder(entity, &header) {
		s.emitTypingError()
		return
	}

	// 3. Visual Feedback (Shield Pulse / Blink)
	// Even Gold gives a pulse via BlinkType, but no heat/energy delta here
	s.emitTypingSuccess(typeableType, typeableLevel, typedRune)

	// 4. Rewards: Non-Gold members give immediate reward

	// Universal: ALL correct typing adds heat
	heatGain := 1

	// Boost and heat logic
	boost, ok := s.boostStore.Get(cursorEntity)
	if ok && boost.Active {
		heatGain = 2 // TODO: const
		s.world.PushEvent(event.EventBoostExtend, &event.BoostExtendPayload{
			Duration: constant.BoostExtensionDuration,
		})
	} else {
		s.world.PushEvent(event.EventBoostActivate, &event.BoostActivatePayload{
			Duration: constant.BoostBaseDuration,
		})
	}
	s.world.PushEvent(event.EventHeatAdd, &event.HeatAddPayload{Delta: heatGain})

	// Energy and heat logic for non-Gold composites
	if header.BehaviorID != component.BehaviorGold {
		points := s.getHeat()
		if typeableType == component.TypeRed {
			points = -points
		}
		s.world.PushEvent(event.EventEnergyAdd, &event.EnergyAddPayload{Delta: points})
	}

	// 5. Signal Composite Hit
	remaining := 0
	for _, m := range header.Members {
		if m.Entity != 0 && m.Entity != entity {
			remaining++
		}
	}

	// Emit member typed event for CompositeSystem routing
	s.world.PushEvent(event.EventMemberTyped, &event.MemberTypedPayload{
		AnchorID:       anchorID,
		MemberEntity:   entity,
		Char:           typedRune,
		RemainingCount: remaining,
	})

	// Move cursor right
	s.moveCursorRight()
}

// validateTypingOrder checks if the entity is the next valid target based on BehaviorID heuristic
func (s *TypingSystem) validateTypingOrder(entity core.Entity, header *component.CompositeHeaderComponent) bool {
	switch header.BehaviorID {
	case component.BehaviorGold:
		// Gold: strict left-to-right ordering (X→Y→EntityID)
		return s.isLeftmostMember(entity, header)

	case component.BehaviorBubble:
		// Bubble: any order allowed
		return true

	case component.BehaviorBoss:
		// Boss: Layer 0 (Core) must be typed in order, Layer 1+ (Shield) any order
		return s.validateBossOrder(entity, header)

	default:
		// Default: spatial order (left-to-right)
		return s.isLeftmostMember(entity, header)
	}
}

// isLeftmostMember returns true if entity is the leftmost living member
// Ordering: X ascending → Y ascending → EntityID ascending
// O(n) single pass, zero allocation
func (s *TypingSystem) isLeftmostMember(entity core.Entity, header *component.CompositeHeaderComponent) bool {
	var leftmost core.Entity
	leftmostX := math.MaxInt
	leftmostY := math.MaxInt

	for _, m := range header.Members {
		if m.Entity == 0 {
			continue
		}
		pos, ok := s.world.Positions.Get(m.Entity)
		if !ok {
			continue
		}

		better := false
		if pos.X < leftmostX {
			better = true
		} else if pos.X == leftmostX {
			if pos.Y < leftmostY {
				better = true
			} else if pos.Y == leftmostY && m.Entity < leftmost {
				better = true
			}
		}

		if better {
			leftmost = m.Entity
			leftmostX = pos.X
			leftmostY = pos.Y
		}
	}

	return leftmost == entity
}

// validateBossOrder checks boss-specific typing rules
// Shield layer (1+): any order allowed
// Core layer (0): must be leftmost core member
// O(n) single pass, zero allocation
func (s *TypingSystem) validateBossOrder(entity core.Entity, header *component.CompositeHeaderComponent) bool {
	// Find entity's layer
	var entityLayer uint8
	for _, m := range header.Members {
		if m.Entity == entity {
			entityLayer = m.Layer
			break
		}
	}

	// Shield layer: any order
	if entityLayer > component.LayerCore {
		return true
	}

	// Core layer: find leftmost core member
	var leftmost core.Entity
	leftmostX := math.MaxInt
	leftmostY := math.MaxInt

	for _, m := range header.Members {
		if m.Entity == 0 || m.Layer != component.LayerCore {
			continue
		}
		pos, ok := s.world.Positions.Get(m.Entity)
		if !ok {
			continue
		}

		better := false
		if pos.X < leftmostX {
			better = true
		} else if pos.X == leftmostX {
			if pos.Y < leftmostY {
				better = true
			} else if pos.Y == leftmostY && m.Entity < leftmost {
				better = true
			}
		}

		if better {
			leftmost = m.Entity
			leftmostX = pos.X
			leftmostY = pos.Y
		}
	}

	return leftmost == entity
}

// handleTypeableTyping processes standalone TypeableComponent entities (nuggets migrated)
func (s *TypingSystem) handleTypeable(entity core.Entity, typeable component.TypeableComponent, typedRune rune) {
	cursorEntity := s.res.Cursor.Entity

	if typeable.Char != typedRune {
		s.emitTypingError()
		return
	}

	// Emit appropriate event based on type
	switch typeable.Type {
	case component.TypeNugget:
		s.world.PushEvent(event.EventNuggetCollected, &event.NuggetCollectedPayload{
			Entity: entity,
		})

		// Universal: ALL correct typing adds heat
		heatGain := 1

		// Check boost state for double heat gain
		boost, ok := s.boostStore.Get(cursorEntity)
		if ok && boost.Active {
			heatGain = 2 // TODO: const
			s.world.PushEvent(event.EventBoostExtend, &event.BoostExtendPayload{
				Duration: constant.BoostExtensionDuration,
			})
		} else {
			s.world.PushEvent(event.EventBoostActivate, &event.BoostActivatePayload{
				Duration: constant.BoostBaseDuration,
			})
		}
		s.world.PushEvent(event.EventHeatAdd, &event.HeatAddPayload{Delta: heatGain})

		// Branching reward logic based on current heat
		// TODO: this is Nugget; tighten after the legacy migration
		currentHeat := s.getHeat()
		if currentHeat >= constant.MaxHeat {
			cursorPos, ok := s.world.Positions.Get(cursorEntity)
			if ok {
				// Trigger directional cleaners ONLY if heat is already at maximum
				s.world.PushEvent(event.EventDirectionalCleanerRequest, &event.DirectionalCleanerPayload{
					OriginX: cursorPos.X,
					OriginY: cursorPos.Y,
				})
			}
		} else {
			// Provide heat reward only when below maximum
			s.world.PushEvent(event.EventHeatAdd, &event.HeatAddPayload{Delta: constant.NuggetHeatIncrease})
		}
		p := event.AcquireDeathRequest(0)
		p.Entities = append(p.Entities, entity)
		s.world.PushEvent(event.EventRequestDeath, p)

	default:
		p := event.AcquireDeathRequest(0)
		p.Entities = append(p.Entities, entity)
		s.world.PushEvent(event.EventRequestDeath, p)
	}

	s.moveCursorRight()
	s.emitTypingSuccess(typeable.Type, typeable.Level, typedRune)
}

// handleNuggetTyping processes legacy nugget typing
func (s *TypingSystem) handleNuggetTyping(entity core.Entity, entityChar rune, typedRune rune, cursorX, cursorY int) {
	if entityChar != typedRune {
		s.emitTypingError()
		return
	}

	s.world.PushEvent(event.EventNuggetCollected, &event.NuggetCollectedPayload{
		Entity: entity,
	})
	// TODO: doing this seems inefficient, why not pool them at target, or let the pool manage append centrally
	p := event.AcquireDeathRequest(0)
	p.Entities = append(p.Entities, entity)
	s.world.PushEvent(event.EventRequestDeath, p)

	s.moveCursorRight()

	// Nugget splash
	s.world.PushEvent(event.EventSplashRequest, &event.SplashRequestPayload{
		Text:    string(typedRune),
		Color:   component.SplashColorNugget,
		OriginX: cursorX,
		OriginY: cursorY,
	})
}

func (s *TypingSystem) emitTypingError() {
	cursorEntity := s.res.Cursor.Entity

	// Set cursor error flash
	cursor, ok := s.cursorStore.Get(cursorEntity)
	if ok {
		cursor.ErrorFlashRemaining = constant.ErrorBlinkTimeout
		s.cursorStore.Add(cursorEntity, cursor)
	}

	// Reset heat and boost
	s.world.PushEvent(event.EventHeatSet, &event.HeatSetPayload{Value: 0})
	s.world.PushEvent(event.EventBoostDeactivate, nil)
	s.world.PushEvent(event.EventEnergyBlinkStart, &event.EnergyBlinkPayload{Type: 0, Level: 0})
	s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
		SoundType: core.SoundError,
	})
}

func (s *TypingSystem) emitTypingSuccess(seqType component.TypeableType, level component.TypeableLevel, char rune) {
	cursorEntity := s.res.Cursor.Entity
	// Map TypeableType to splash color
	var splashColor component.SplashColor
	var blinkType uint32

	switch seqType {
	case component.TypeGreen:
		splashColor = component.SplashColorGreen
		blinkType = 2
	case component.TypeBlue:
		splashColor = component.SplashColorBlue
		blinkType = 1
	case component.TypeRed:
		splashColor = component.SplashColorRed
		blinkType = 3
	case component.TypeGold:
		splashColor = component.SplashColorGold
		blinkType = 4
	case component.TypeNugget:
		splashColor = component.SplashColorNugget
		blinkType = 0
	default:
		splashColor = component.SplashColorNormal
		blinkType = 0
	}

	var blinkLevel uint32
	switch level {
	case component.LevelDark:
		blinkLevel = 0
	case component.LevelNormal:
		blinkLevel = 1
	case component.LevelBright:
		blinkLevel = 2
	}

	s.world.PushEvent(event.EventEnergyBlinkStart, &event.EnergyBlinkPayload{
		Type:  blinkType,
		Level: blinkLevel,
	})

	cursorPos, ok := s.world.Positions.Get(cursorEntity)
	if ok {
		s.world.PushEvent(event.EventSplashRequest, &event.SplashRequestPayload{
			Text:    string(char),
			Color:   splashColor,
			OriginX: cursorPos.X,
			OriginY: cursorPos.Y,
		})
	}
}

func (s *TypingSystem) moveCursorRight() {
	cursorEntity := s.res.Cursor.Entity
	config := s.res.Config

	cursorPos, ok := s.world.Positions.Get(cursorEntity)
	if ok && cursorPos.X < config.GameWidth-1 {
		cursorPos.X++
		s.world.Positions.Add(cursorEntity, cursorPos)
	}
}