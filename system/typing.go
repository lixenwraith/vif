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
	engine.SystemBase

	statCorrect   *atomic.Int64
	statErrors    *atomic.Int64
	statMaxStreak *atomic.Int64

	currentStreak int64

	enabled bool
}

// NewTypingSystem creates a new typing system
func NewTypingSystem(world *engine.World) engine.System {
	res := engine.GetResourceStore(world)
	s := &TypingSystem{
		SystemBase: engine.NewSystemBase(world),
	}

	s.statCorrect = res.Status.Ints.Get("typing.correct")
	s.statErrors = res.Status.Ints.Get("typing.errors")
	s.statMaxStreak = res.Status.Ints.Get("typing.max_streak")

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
	// Stack-allocated buffer for zero-allocation lookup
	var buf [constant.MaxEntitiesPerCell]core.Entity
	count := s.World.Positions.GetAllAtInto(cursorX, cursorY, buf[:])

	var entity core.Entity

	// Iterate to find typeable entity (Glyph)
	// Break on first match for O(1) best case in crowded cells
	for i := 0; i < count; i++ {
		if s.Component.Glyph.Has(buf[i]) {
			entity = buf[i]
			break
		}
	}

	if entity == 0 {
		s.emitTypingError()
		return
	}

	// Check if this is a composite member
	if member, ok := s.Component.Member.Get(entity); ok {
		s.handleCompositeMember(entity, member.AnchorID, typedRune)
		return
	}

	// Check for standalone GlyphComponent
	if glyph, ok := s.Component.Glyph.Get(entity); ok {
		s.handleGlyph(entity, glyph, typedRune)
		return
	}

	s.emitTypingError()
}

// === UNIFIED REWARD HELPERS ===

// applyUniversalRewards handles boost activation/extension and heat gain for any correct typing
func (s *TypingSystem) applyUniversalRewards() {
	cursorEntity := s.Resource.Cursor.Entity

	// Check current boost state BEFORE pushing events
	boost, ok := s.Component.Boost.Get(cursorEntity)
	isBoostActive := ok && boost.Active

	// Boost: activate or extend
	if isBoostActive {
		s.World.PushEvent(event.EventBoostExtend, &event.BoostExtendPayload{
			Duration: constant.BoostExtensionDuration,
		})
	} else {
		s.World.PushEvent(event.EventBoostActivate, &event.BoostActivatePayload{
			Duration: constant.BoostBaseDuration,
		})
	}

	// Heat: +2 with active boost, +1 without
	// TODO: const
	heatGain := 1
	if isBoostActive {
		heatGain = 2
	}
	s.World.PushEvent(event.EventHeatAdd, &event.HeatAddPayload{Delta: heatGain})

	s.statCorrect.Add(1)
	s.currentStreak++
	maxStreak := s.statMaxStreak.Load()
	if maxStreak < s.currentStreak {
		s.statMaxStreak.Store(s.currentStreak)
	}
}

// applyColorEnergy handles energy based on color type
// Green: +heat, Blue: +2*heat, Red: -heat (mirrors green)
func (s *TypingSystem) applyColorEnergy(colorType component.GlyphType) {
	heat := s.getHeat()
	switch colorType {
	case component.GlyphGreen:
		s.World.PushEvent(event.EventEnergyAdd, &event.EnergyAddPayload{Delta: heat})
	case component.GlyphBlue:
		s.World.PushEvent(event.EventEnergyAdd, &event.EnergyAddPayload{Delta: 2 * heat})
	case component.GlyphRed:
		s.World.PushEvent(event.EventEnergyAdd, &event.EnergyAddPayload{Delta: -heat})
	}
}

// emitTypingFeedback sends visual feedback (splash + blink)
func (s *TypingSystem) emitTypingFeedback(glyphType component.GlyphType, char rune) {
	cursorPos, _ := s.World.Positions.Get(s.Resource.Cursor.Entity)

	var splashColor component.SplashColor
	var blinkType uint32

	switch glyphType {
	case component.GlyphGreen:
		splashColor = component.SplashColorGreen
		blinkType = 2
	case component.GlyphBlue:
		splashColor = component.SplashColorBlue
		blinkType = 1
	case component.GlyphRed:
		splashColor = component.SplashColorRed
		blinkType = 3
	case component.GlyphGold:
		splashColor = component.SplashColorGold
		blinkType = 4
	default:
		splashColor = component.SplashColorNormal
		blinkType = 0
	}

	s.World.PushEvent(event.EventEnergyBlinkStart, &event.EnergyBlinkPayload{
		Type: blinkType,
	})

	s.World.PushEvent(event.EventSplashRequest, &event.SplashRequestPayload{
		Text:    string(char),
		Color:   splashColor,
		OriginX: cursorPos.X,
		OriginY: cursorPos.Y,
	})
}

// emitTypingError emits events corresponding to typing error
func (s *TypingSystem) emitTypingError() {
	cursorEntity := s.Resource.Cursor.Entity

	// Set cursor error flash
	if cursor, ok := s.Component.Cursor.Get(cursorEntity); ok {
		cursor.ErrorFlashRemaining = constant.ErrorBlinkTimeout
		s.Component.Cursor.Set(cursorEntity, cursor)
	}

	// Reset heat and boost
	s.World.PushEvent(event.EventHeatAdd, &event.HeatAddPayload{Delta: -10})
	s.World.PushEvent(event.EventBoostDeactivate, nil)
	s.World.PushEvent(event.EventEnergyBlinkStart, &event.EnergyBlinkPayload{Type: 0, Level: 0})
	s.World.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
		SoundType: core.SoundError,
	})

	s.statErrors.Add(1)
	s.currentStreak = 0
}

func (s *TypingSystem) moveCursorRight() {
	cursorEntity := s.Resource.Cursor.Entity
	config := s.Resource.Config

	if cursorPos, ok := s.World.Positions.Get(cursorEntity); ok && cursorPos.X < config.GameWidth-1 {
		cursorPos.X++
		s.World.Positions.Set(cursorEntity, cursorPos)
	}
}

// getHeat reads current heat value from cursor's HeatComponent
func (s *TypingSystem) getHeat() int {
	cursorEntity := s.Resource.Cursor.Entity
	if hc, ok := s.Component.Heat.Get(cursorEntity); ok {
		return int(hc.Current.Load())
	}
	return 0
}

// === HANDLER PATHS ===

// handleCompositeMember processes typing for composite member entities
func (s *TypingSystem) handleCompositeMember(entity core.Entity, anchorID core.Entity, typedRune rune) {
	glyph, ok := s.Component.Glyph.Get(entity)
	if !ok {
		s.emitTypingError()
		return
	}

	targetChar := glyph.Rune
	glyphType := glyph.Type

	// Character match check
	if targetChar != typedRune {
		s.emitTypingError()
		return
	}

	// Identify composite behavior for reward logic
	header, ok := s.Component.Header.Get(anchorID)
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
		s.applyColorEnergy(glyphType)
	}

	// Visual feedback
	s.emitTypingFeedback(glyphType, typedRune)

	// Signal composite system
	remaining := 0
	for _, m := range header.Members {
		if m.Entity != 0 && m.Entity != entity {
			remaining++
		}
	}
	s.World.PushEvent(event.EventMemberTyped, &event.MemberTypedPayload{
		AnchorID:       anchorID,
		MemberEntity:   entity,
		Char:           typedRune,
		RemainingCount: remaining,
	})

	s.moveCursorRight()
}

// handleGlyph processes standalone GlyphComponent entities
func (s *TypingSystem) handleGlyph(entity core.Entity, glyph component.GlyphComponent, typedRune rune) {
	if glyph.Rune != typedRune {
		s.emitTypingError()
		return
	}

	// Universal rewards
	s.applyUniversalRewards()

	// Type-specific handling, placeholder for other type additions
	switch glyph.Type {
	case component.GlyphBlue, component.GlyphGreen, component.GlyphRed:
		// Color-based energy
		s.applyColorEnergy(glyph.Type)
	}

	// Silent Death
	event.EmitDeathOne(s.Resource.Events.Queue, entity, 0, s.Resource.Time.FrameNumber)

	// Splash typing feedback
	s.emitTypingFeedback(glyph.Type, typedRune)
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
		pos, ok := s.World.Positions.Get(m.Entity)
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
		pos, ok := s.World.Positions.Get(m.Entity)
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