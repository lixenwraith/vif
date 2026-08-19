package system

import (
	"math"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// TypingSystem handles character typing validation and composite member ordering
// Extracted from EnergySystem to support composite entity mechanics
type TypingSystem struct {
	world *engine.World

	// Reusable delete collection scratch
	deleteBuf []core.Entity

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
	s.deleteBuf = s.deleteBuf[:0]
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
	return parameter.PriorityTyping
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
		event.EventGameResetRequest,
	}
}

func (s *TypingSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
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
	var buf [parameter.MaxEntitiesPerCell]core.Entity
	count := s.world.Positions.GetAllEntitiesAtInto(cursorX, cursorY, buf[:])

	var entity core.Entity

	// Iterate to find typeable entity (Glyph)
	// Break on first match for O(1) best case in crowded cells
	for i := range count {
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
	cursorEntity := s.world.Resources.Player.Entity

	// Check current boost state BEFORE pushing events
	boost, ok := s.world.Components.Boost.GetComponent(cursorEntity)
	isBoostActive := ok && boost.Active

	// Boost: activate or extend
	if isBoostActive {
		s.world.PushEvent(event.EventBoostExtend, &event.BoostExtendPayload{
			Duration: parameter.BoostExtensionDuration,
		})
	} else {
		s.world.PushEvent(event.EventBoostActivate, &event.BoostActivatePayload{
			Duration: parameter.BoostBaseDuration,
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

// emitTypingFeedback sends visual feedback
func (s *TypingSystem) emitTypingFeedback(glyphType component.GlyphType) {
	var blinkType int

	switch glyphType {
	case component.GlyphBlue:
		blinkType = 1
	case component.GlyphGreen:
		blinkType = 2
	case component.GlyphRed:
		blinkType = 3
	case component.GlyphGold:
		blinkType = 4
	default:
		blinkType = 0
	}

	s.world.PushEvent(event.EventEnergyBlinkStart, &event.EnergyBlinkPayload{
		Type: blinkType,
	})
}

// emitTypingError emits events corresponding to typing error
func (s *TypingSystem) emitTypingError() {
	cursorEntity := s.world.Resources.Player.Entity

	// Set cursor error flash
	if cursor, ok := s.world.Components.Cursor.GetComponent(cursorEntity); ok {
		cursor.ErrorFlashRemaining = parameter.ErrorBlinkTimeout
		s.world.Components.Cursor.SetComponent(cursorEntity, cursor)
	}

	// Reset boost and apply heat penalty
	s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{Delta: -parameter.HeatTypingErrorPenalty})
	s.world.PushEvent(event.EventBoostDeactivate, nil)
	s.world.PushEvent(event.EventEnergyBlinkStart, &event.EnergyBlinkPayload{Type: 0, Level: 0})

	s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
		ID: parameter.Sfx.Error,
	})

	s.statErrors.Add(1)
	s.currentStreak = 0
}

// moveCursorRight requests the post-typing advance; CursorSystem applies and announces it
func (s *TypingSystem) moveCursorRight() {
	cursorEntity := s.world.Resources.Player.Entity
	config := s.world.Resources.Config

	if cursorPos, ok := s.world.Positions.GetPosition(cursorEntity); ok && cursorPos.X < config.MapWidth-1 {
		s.world.PushEvent(event.EventCursorMoveRequest, &event.CursorMoveRequestPayload{
			Entity: cursorEntity, X: cursorPos.X + 1, Y: cursorPos.Y,
		})
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

	if !s.isLeftmostMember(entity, &header) {
		s.emitTypingError()
		return
	}

	// Universal rewards (boost + heat)
	s.applyUniversalRewards()

	// Color-based energy (only Blue/Green/Red for now)
	if header.Behavior != component.BehaviorGold {
		s.world.PushEvent(event.EventEnergyGlyphConsumed, &event.EnergyGlyphConsumedPayload{
			Type:  glyph.Type,
			Level: glyph.Level,
		})
	}

	// Visual feedback
	s.emitTypingFeedback(glyph.Type)

	// Signal composite system
	remaining := 0
	for _, m := range header.MemberEntries {
		if m.Entity != 0 && m.Entity != entity {
			remaining++
		}
	}
	s.world.PushEvent(event.EventCompositeMemberDestroyed, &event.CompositeMemberDestroyedPayload{
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
		s.world.PushEvent(event.EventEnergyGlyphConsumed, &event.EnergyGlyphConsumedPayload{
			Type:  glyph.Type,
			Level: glyph.Level,
		})
	}

	// Silent Death
	event.EmitDeathOne(s.world.Resources.Event.Queue, entity, 0)

	// Blink typing feedback
	s.emitTypingFeedback(glyph.Type)
	s.moveCursorRight()
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

// handleDeleteRequest destroys glyphs whose position falls inside the requested range
// Store-driven: Glyph+Position are authoritative, the spatial grid is not consulted
func (s *TypingSystem) handleDeleteRequest(payload *event.DeleteRequestPayload) {
	lineRange := payload.RangeType == event.DeleteRangeLine

	startX, startY := payload.StartX, payload.StartY
	endX, endY := payload.EndX, payload.EndY
	if startY > endY || (startY == endY && startX > endX) {
		startX, startY, endX, endY = endX, endY, startX, startY
	}

	s.deleteBuf = s.deleteBuf[:0]

	s.world.Components.Glyph.Each(func(e core.Entity, _ *component.GlyphComponent) bool {
		pos, ok := s.world.Positions.GetPosition(e)
		if !ok {
			return true // orphan glyph: no position, not a positional target
		}
		if pos.Y < startY || pos.Y > endY {
			return true
		}
		// Char ranges clamp X on the first and last rows only
		if !lineRange {
			if pos.Y == startY && pos.X < startX {
				return true
			}
			if pos.Y == endY && pos.X > endX {
				return true
			}
		}
		if prot, ok := s.world.Components.Protection.GetComponent(e); ok {
			if prot.Mask&component.ProtectFromDelete != 0 {
				return true
			}
		}
		s.deleteBuf = append(s.deleteBuf, e)
		return true
	})

	if len(s.deleteBuf) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, 0, s.deleteBuf)
	}
}
