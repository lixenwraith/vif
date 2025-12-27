package system

import (
	"math"
	"sync/atomic"

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
	boostStore    *engine.Store[component.BoostComponent]
	heatStore     *engine.Store[component.HeatComponent]

	statCorrect   *atomic.Int64
	statErrors    *atomic.Int64
	statMaxStreak *atomic.Int64

	currentStreak int64

	enabled bool
}

// NewTypingSystem creates a new typing system
func NewTypingSystem(world *engine.World) engine.System {
	res := engine.GetResources(world)
	s := &TypingSystem{
		world: world,
		res:   res,

		typeableStore: engine.GetStore[component.TypeableComponent](world),
		memberStore:   engine.GetStore[component.MemberComponent](world),
		headerStore:   engine.GetStore[component.CompositeHeaderComponent](world),
		cursorStore:   engine.GetStore[component.CursorComponent](world),
		charStore:     engine.GetStore[component.CharacterComponent](world),
		nuggetStore:   engine.GetStore[component.NuggetComponent](world),
		boostStore:    engine.GetStore[component.BoostComponent](world),
		heatStore:     engine.GetStore[component.HeatComponent](world),

		statCorrect:   res.Status.Ints.Get("typing.correct"),
		statErrors:    res.Status.Ints.Get("typing.errors"),
		statMaxStreak: res.Status.Ints.Get("typing.max_streak"),
	}
	s.initLocked()
	return s
}

func (s *TypingSystem) Init() {
	s.initLocked()
}

func (s *TypingSystem) initLocked() {
	s.currentStreak = 0
	s.statCorrect.Store(0)
	s.statErrors.Store(0)
	s.statMaxStreak.Store(0)
	s.enabled = true
}

func (s *TypingSystem) Priority() int {
	return constant.PriorityTyping
}

func (s *TypingSystem) Update() {
	if !s.enabled {
		return
	}
}

func (s *TypingSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventCharacterTyped,
		event.EventGameReset,
	}
}

func (s *TypingSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventCharacterTyped:
		payload, ok := ev.Payload.(*event.CharacterTypedPayload)
		if !ok {
			return
		}
		s.handleTyping(payload.X, payload.Y, payload.Char)
		event.CharacterTypedPayloadPool.Put(payload)
	}
}

// handleTyping processes a typed character at cursor position
func (s *TypingSystem) handleTyping(cursorX, cursorY int, typedRune rune) {
	// Find interactable entity at cursor position
	entity := s.world.Positions.GetTopEntityFiltered(cursorX, cursorY, func(e core.Entity) bool {
		return s.res.ZIndex.IsTypeable(e)
	})

	if entity == 0 {
		s.emitTypingError()
		return
	}

	// Check if this is a composite member
	if member, ok := s.memberStore.Get(entity); ok {
		s.handleCompositeMember(entity, member.AnchorID, typedRune)
		return
	}

	// Check for standalone TypeableComponent
	if typeable, ok := s.typeableStore.Get(entity); ok {
		s.handleTypeable(entity, typeable, typedRune)
		return
	}

	s.emitTypingError()
}

// === UNIFIED REWARD HELPERS ===

// applyUniversalRewards handles boost activation/extension and heat gain for any correct typing
func (s *TypingSystem) applyUniversalRewards() {
	cursorEntity := s.res.Cursor.Entity

	// Check current boost state BEFORE pushing events
	boost, ok := s.boostStore.Get(cursorEntity)
	isBoostActive := ok && boost.Active

	// Boost: activate or extend
	if isBoostActive {
		s.world.PushEvent(event.EventBoostExtend, &event.BoostExtendPayload{
			Duration: constant.BoostExtensionDuration,
		})
	} else {
		s.world.PushEvent(event.EventBoostActivate, &event.BoostActivatePayload{
			Duration: constant.BoostBaseDuration,
		})
	}

	// Heat: +2 with active boost, +1 without
	// TODO: const
	heatGain := 1
	if isBoostActive {
		heatGain = 2
	}
	s.world.PushEvent(event.EventHeatAdd, &event.HeatAddPayload{Delta: heatGain})

	s.statCorrect.Add(1)
	s.currentStreak++
	maxStreak := s.statMaxStreak.Load()
	if maxStreak < s.currentStreak {
		s.statMaxStreak.Store(s.currentStreak)
	}
}

// applyColorEnergy handles energy based on color type
// Green: +heat, Blue: +2*heat, Red: -heat (mirrors green)
func (s *TypingSystem) applyColorEnergy(colorType component.TypeableType) {
	heat := s.getHeat()
	switch colorType {
	case component.TypeGreen:
		s.world.PushEvent(event.EventEnergyAdd, &event.EnergyAddPayload{Delta: heat})
	case component.TypeBlue:
		s.world.PushEvent(event.EventEnergyAdd, &event.EnergyAddPayload{Delta: 2 * heat})
	case component.TypeRed:
		s.world.PushEvent(event.EventEnergyAdd, &event.EnergyAddPayload{Delta: -heat})
	}
}

// emitTypingFeedback sends visual feedback (splash + blink)
func (s *TypingSystem) emitTypingFeedback(typeableType component.TypeableType, level component.TypeableLevel, char rune) {
	cursorPos, _ := s.world.Positions.Get(s.res.Cursor.Entity)

	var splashColor component.SplashColor
	var blinkType uint32

	switch typeableType {
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

	s.world.PushEvent(event.EventSplashRequest, &event.SplashRequestPayload{
		Text:    string(char),
		Color:   splashColor,
		OriginX: cursorPos.X,
		OriginY: cursorPos.Y,
	})
}

func (s *TypingSystem) emitTypingError() {
	cursorEntity := s.res.Cursor.Entity

	// Set cursor error flash
	if cursor, ok := s.cursorStore.Get(cursorEntity); ok {
		cursor.ErrorFlashRemaining = constant.ErrorBlinkTimeout
		s.cursorStore.Set(cursorEntity, cursor)
	}

	// Reset heat and boost
	s.world.PushEvent(event.EventHeatSet, &event.HeatSetPayload{Value: 0})
	s.world.PushEvent(event.EventBoostDeactivate, nil)
	s.world.PushEvent(event.EventEnergyBlinkStart, &event.EnergyBlinkPayload{Type: 0, Level: 0})
	s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
		SoundType: core.SoundError,
	})

	s.statErrors.Add(1)
	s.currentStreak = 0
}

func (s *TypingSystem) moveCursorRight() {
	cursorEntity := s.res.Cursor.Entity
	config := s.res.Config

	if cursorPos, ok := s.world.Positions.Get(cursorEntity); ok && cursorPos.X < config.GameWidth-1 {
		cursorPos.X++
		s.world.Positions.Set(cursorEntity, cursorPos)
	}
}

// getHeat reads current heat value from cursor's HeatComponent
func (s *TypingSystem) getHeat() int {
	cursorEntity := s.res.Cursor.Entity
	if hc, ok := s.heatStore.Get(cursorEntity); ok {
		return int(hc.Current.Load())
	}
	return 0
}

// === HANDLER PATHS ===

// handleCompositeMember processes typing for composite member entities
func (s *TypingSystem) handleCompositeMember(entity core.Entity, anchorID core.Entity, typedRune rune) {
	// Get typeable or fall back to character component
	var targetChar rune
	var typeableType component.TypeableType
	var typeableLevel component.TypeableLevel

	if typeable, ok := s.typeableStore.Get(entity); ok {
		targetChar = typeable.Char
		typeableType = typeable.Type
		typeableLevel = typeable.Level
	} else if char, ok := s.charStore.Get(entity); ok {
		// Fallback to CharacterComponent for migration period
		targetChar = char.Rune
		switch char.Type {
		case component.CharacterGold:
			typeableType = component.TypeGold
		case component.CharacterBlue:
			typeableType = component.TypeBlue
		case component.CharacterGreen:
			typeableType = component.TypeGreen
		case component.CharacterRed:
			typeableType = component.TypeRed
		}
		typeableLevel = char.Level
	} else {
		s.emitTypingError()
		return
	}

	// Character match check
	if targetChar != typedRune {
		s.emitTypingError()
		return
	}

	// Identify composite behavior for reward logic
	header, ok := s.headerStore.Get(anchorID)
	if !ok {
		s.emitTypingError()
		return
	}

	// Validate composite typing order
	if !s.validateTypingOrder(entity, &header) {
		s.emitTypingError()
		return
	}

	// Universal rewards (boost + heat)
	s.applyUniversalRewards()

	// Color-based energy (only Blue/Green/Red for now)
	if header.BehaviorID != component.BehaviorGold {
		s.applyColorEnergy(typeableType)
	}

	// Visual feedback
	s.emitTypingFeedback(typeableType, typeableLevel, typedRune)

	// Signal composite system
	remaining := 0
	for _, m := range header.Members {
		if m.Entity != 0 && m.Entity != entity {
			remaining++
		}
	}
	s.world.PushEvent(event.EventMemberTyped, &event.MemberTypedPayload{
		AnchorID:       anchorID,
		MemberEntity:   entity,
		Char:           typedRune,
		RemainingCount: remaining,
	})

	s.moveCursorRight()
}

// handleTypeable processes standalone TypeableComponent entities (nuggets)
func (s *TypingSystem) handleTypeable(entity core.Entity, typeable component.TypeableComponent, typedRune rune) {
	// cursorEntity := s.res.Cursor.Entity

	if typeable.Char != typedRune {
		s.emitTypingError()
		return
	}

	// Universal rewards
	s.applyUniversalRewards()

	// Type-specific handling, placeholder for other type additions
	switch typeable.Type {
	case component.TypeBlue, component.TypeGreen, component.TypeRed:
		// Color-based energy
		s.applyColorEnergy(typeable.Type)
	}

	// Silent Death
	event.EmitDeathOne(s.res.Events.Queue, entity, 0, s.res.Time.FrameNumber)

	// Splash typing feedback
	s.emitTypingFeedback(typeable.Type, typeable.Level, typedRune)
	s.moveCursorRight()
}

// === TYPING ORDER VALIDATION ===

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