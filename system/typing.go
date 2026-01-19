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

	statCorrect   *atomic.Int64
	statErrors    *atomic.Int64
	statMaxStreak *atomic.Int64

	currentStreak int64

	enabled bool
}

// NewTypingSystem creates a new typing system
func NewTypingSystem(world *engine.World) engine.System {
	s := &TypingSystem{
		world: world,
	}

	s.statCorrect = world.Resources.Status.Ints.Get("typing.correct")
	s.statErrors = world.Resources.Status.Ints.Get("typing.errors")
	s.statMaxStreak = world.Resources.Status.Ints.Get("typing.max_streak")

	s.Init()
	return s
}

func (s *TypingSystem) Init() {
	s.currentStreak = 0
	s.statCorrect.Store(0)
	s.statErrors.Store(0)
	s.statMaxStreak.Store(0)
	s.enabled = true
}

// Name returns system's name
func (s *TypingSystem) Name() string {
	return "typing"
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
		event.EventDeleteRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *TypingSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
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

	case event.EventDeleteRequest:
		if payload, ok := ev.Payload.(*event.DeleteRequestPayload); ok {
			s.handleDeleteRequest(payload)
		}
	}
}

// handleTyping processes a typed character at cursor position
func (s *TypingSystem) handleTyping(cursorX, cursorY int, typedRune rune) {
	// Stack-allocated buffer for zero-allocation lookup
	var buf [constant.MaxEntitiesPerCell]core.Entity
	count := s.world.Positions.GetAllEntitiesAtInto(cursorX, cursorY, buf[:])

	var entity core.Entity

	// Iterate to find typeable entity (Glyph)
	// Break on first match for O(1) best case in crowded cells
	for i := 0; i < count; i++ {
		if s.world.Components.Glyph.HasEntity(buf[i]) {
			entity = buf[i]
			break
		}
	}

	if entity == 0 {
		s.emitTypingError()
		return
	}

	// Check if this is a composite member
	if member, ok := s.world.Components.Member.GetComponent(entity); ok {
		s.handleCompositeMember(entity, member.HeaderEntity, typedRune)
		return
	}

	// Check for standalone GlyphComponent
	if glyph, ok := s.world.Components.Glyph.GetComponent(entity); ok {
		s.handleGlyph(entity, glyph, typedRune)
		return
	}

	s.emitTypingError()
}

// === UNIFIED REWARD HELPERS ===

// applyUniversalRewards handles boost activation/extension and heat gain for any correct typing
func (s *TypingSystem) applyUniversalRewards() {
	cursorEntity := s.world.Resources.Cursor.Entity

	// Check current boost state BEFORE pushing events
	boost, ok := s.world.Components.Boost.GetComponent(cursorEntity)
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
	s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{Delta: heatGain})

	s.statCorrect.Add(1)
	s.currentStreak++
	maxStreak := s.statMaxStreak.Load()
	if maxStreak < s.currentStreak {
		s.statMaxStreak.Store(s.currentStreak)
	}
}

// emitTypingFeedback sends visual feedback (splash + blink)
func (s *TypingSystem) emitTypingFeedback(glyphType component.GlyphType, char rune) {
	cursorPos, _ := s.world.Positions.GetPosition(s.world.Resources.Cursor.Entity)

	var splashColor component.SplashColor
	var blinkType int

	switch glyphType {
	case component.GlyphBlue:
		splashColor = component.SplashColorBlue
		blinkType = 1
	case component.GlyphGreen:
		splashColor = component.SplashColorGreen
		blinkType = 2
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

	s.world.PushEvent(event.EventEnergyBlinkStart, &event.EnergyBlinkPayload{
		Type: blinkType,
	})

	s.world.PushEvent(event.EventSplashRequest, &event.SplashRequestPayload{
		Text:    string(char),
		Color:   splashColor,
		OriginX: cursorPos.X,
		OriginY: cursorPos.Y,
	})
}

// emitTypingError emits events corresponding to typing error
func (s *TypingSystem) emitTypingError() {
	cursorEntity := s.world.Resources.Cursor.Entity

	// Set cursor error flash
	if cursor, ok := s.world.Components.Cursor.GetComponent(cursorEntity); ok {
		cursor.ErrorFlashRemaining = constant.ErrorBlinkTimeout
		s.world.Components.Cursor.SetComponent(cursorEntity, cursor)
	}

	// Reset boost and apply heat penalty
	s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{Delta: -constant.HeatTypingErrorPenalty})
	s.world.PushEvent(event.EventBoostDeactivate, nil)
	s.world.PushEvent(event.EventEnergyBlinkStart, &event.EnergyBlinkPayload{Type: 0, Level: 0})

	s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
		SoundType: core.SoundError,
	})

	s.statErrors.Add(1)
	s.currentStreak = 0
}

func (s *TypingSystem) moveCursorRight() {
	cursorEntity := s.world.Resources.Cursor.Entity
	config := s.world.Resources.Config

	if cursorPos, ok := s.world.Positions.GetPosition(cursorEntity); ok && cursorPos.X < config.GameWidth-1 {
		cursorPos.X++
		s.world.Positions.SetPosition(cursorEntity, cursorPos)
	}
}

// === HANDLER PATHS ===

// handleCompositeMember processes typing for composite member entities
func (s *TypingSystem) handleCompositeMember(entity core.Entity, anchorID core.Entity, typedRune rune) {
	glyph, ok := s.world.Components.Glyph.GetComponent(entity)
	if !ok {
		s.emitTypingError()
		return
	}

	// Character match check
	if glyph.Rune != typedRune {
		s.emitTypingError()
		return
	}

	// Identify composite behavior for reward logic
	header, ok := s.world.Components.Header.GetComponent(anchorID)
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
	if header.Behavior != component.BehaviorGold {
		s.world.PushEvent(event.EventEnergyGlyphConsumed, &event.GlyphConsumedPayload{
			Type:  glyph.Type,
			Level: glyph.Level,
		})
	}

	// Visual feedback
	s.emitTypingFeedback(glyph.Type, typedRune)

	// Signal composite system
	remaining := 0
	for _, m := range header.MemberEntries {
		if m.Entity != 0 && m.Entity != entity {
			remaining++
		}
	}
	s.world.PushEvent(event.EventMemberTyped, &event.MemberTypedPayload{
		HeaderEntity:   anchorID,
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
		s.world.PushEvent(event.EventEnergyGlyphConsumed, &event.GlyphConsumedPayload{
			Type:  glyph.Type,
			Level: glyph.Level,
		})
	}

	// Silent Death
	event.EmitDeathOne(s.world.Resources.Event.Queue, entity, 0)

	// Splash typing feedback
	s.emitTypingFeedback(glyph.Type, typedRune)
	s.moveCursorRight()
}

// === TYPING ORDER VALIDATION ===

// validateTypingOrder checks if the entity is the next valid target based on Behavior heuristic
func (s *TypingSystem) validateTypingOrder(entity core.Entity, header *component.HeaderComponent) bool {
	switch header.Behavior {
	case component.BehaviorGold:
		// Gold: strict left-to-right ordering (X→Y→EntityID)
		return s.isLeftmostMember(entity, header)

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
func (s *TypingSystem) isLeftmostMember(entity core.Entity, header *component.HeaderComponent) bool {
	var leftmost core.Entity
	leftmostX := math.MaxInt
	leftmostY := math.MaxInt

	for _, m := range header.MemberEntries {
		if m.Entity == 0 {
			continue
		}
		pos, ok := s.world.Positions.GetPosition(m.Entity)
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
func (s *TypingSystem) validateBossOrder(entity core.Entity, header *component.HeaderComponent) bool {
	// Find entity's layer
	var entityLayer uint8
	for _, m := range header.MemberEntries {
		if m.Entity == entity {
			entityLayer = m.Layer
			break
		}
	}

	// Shield layer: any order
	if entityLayer > component.LayerGlyph {
		return true
	}

	// Core layer: find leftmost core member
	var leftmost core.Entity
	leftmostX := math.MaxInt
	leftmostY := math.MaxInt

	for _, m := range header.MemberEntries {
		if m.Entity == 0 || m.Layer != component.LayerGlyph {
			continue
		}
		pos, ok := s.world.Positions.GetPosition(m.Entity)
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

// handleDeleteRequest processes deletion of entities in a range
func (s *TypingSystem) handleDeleteRequest(payload *event.DeleteRequestPayload) {
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

	cellEntitiesBuf := make([]core.Entity, constant.MaxEntitiesPerCell)

	if payload.RangeType == event.DeleteRangeLine {
		// Line deletion (inclusive rows)
		startY, endY := payload.StartY, payload.EndY
		// Ensure normalized order
		if startY > endY {
			startY, endY = endY, startY
		}

		// Query all glyphs to find those in the row range
		entities := s.world.Components.Glyph.GetAllEntities()
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
				s.world.Positions.GetAllEntitiesAtInto(x, y, cellEntitiesBuf)
				for _, entity := range cellEntitiesBuf {
					checkEntity(entity)
				}
			}
		}
	}

	// Batch deletion via DeathSystem (silent)
	if len(entitiesToDelete) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, 0, entitiesToDelete)
	}
}