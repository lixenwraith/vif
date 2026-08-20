package system

import (
	"math"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/status"
)

// TypingSystem validates typed characters and composite member ordering.
// Every path is scoped to the cursor that produced the keystroke.
type TypingSystem struct {
	world *engine.World

	// Reusable delete collection scratch
	deleteBuf []core.Entity

	// Roster-wide totals
	statCorrect *atomic.Int64
	statErrors  *atomic.Int64

	// Per-cursor records
	statMaxStreak *status.PlayerInt
	currentStreak [parameter.MaxPlayers]int64

	enabled bool
}

// NewTypingSystem creates a new typing system
func NewTypingSystem(world *engine.World) engine.System {
	s := &TypingSystem{world: world}

	reg := world.Resources.Status
	s.statCorrect = reg.Ints.Get("typing.correct")
	s.statErrors = reg.Ints.Get("typing.errors")
	s.statMaxStreak = status.NewPlayerInt(reg, parameter.MaxPlayers, "typing.max_streak", "typing.max_streak")

	s.Init()
	return s
}

// Init resets session state for a new game, including every slot's streak
func (s *TypingSystem) Init() {
	s.deleteBuf = s.deleteBuf[:0]
	s.currentStreak = [parameter.MaxPlayers]int64{}
	s.statCorrect.Store(0)
	s.statErrors.Store(0)
	s.statMaxStreak.Reset()
	s.enabled = true
}

// Name returns system's name
func (s *TypingSystem) Name() string { return "typing" }

// Priority returns the system's priority
func (s *TypingSystem) Priority() int { return parameter.PriorityTyping }

// Update is empty: typing is entirely event-driven
func (s *TypingSystem) Update() {}

// EventTypes returns the event types TypingSystem handles
func (s *TypingSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventCharacterTyped,
		event.EventDeleteRequest,
		event.EventCursorDespawned,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

// HandleEvent processes typing and deletion requests
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
	case event.EventCursorDespawned:
		if p, ok := ev.Payload.(*event.CursorDespawnedPayload); ok {
			s.clearSlot(p.Slot)
		}

	case event.EventCharacterTyped:
		payload, ok := ev.Payload.(*event.CharacterTypedPayload)
		if !ok {
			return
		}
		// Resolve before release: the pool reclaims the payload below
		if cursor := s.world.ResolveCursor(payload.Entity); cursor != 0 {
			s.handleTyping(cursor, payload.X, payload.Y, payload.Char)
		}
		event.CharacterTypedPayloadPool.Put(payload)

	case event.EventDeleteRequest:
		if payload, ok := ev.Payload.(*event.DeleteRequestPayload); ok {
			s.handleDeleteRequest(payload)
		}
	}
}

// handleTyping resolves the glyph under one cursor and dispatches the match
func (s *TypingSystem) handleTyping(cursor core.Entity, cursorX, cursorY int, typedRune rune) {
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
		s.emitTypingError(cursor)
		return
	}

	// Check if this is a composite member
	if member, ok := s.world.Components.Member.GetComponent(entity); ok {
		s.handleCompositeMember(cursor, entity, member.HeaderEntity, typedRune)
		return
	}

	// Check for standalone GlyphComponent
	if glyph, ok := s.world.Components.Glyph.GetComponent(entity); ok {
		s.handleGlyph(cursor, entity, glyph, typedRune)
		return
	}

	s.emitTypingError(cursor)
}

// === UNIFIED REWARD HELPERS ===

// applyUniversalRewards grants boost and heat to the cursor that typed correctly
func (s *TypingSystem) applyUniversalRewards(cursor core.Entity) {
	// Check current boost state BEFORE pushing events
	boost, hasBoost := s.world.Components.Boost.GetComponent(cursor)
	isBoostActive := hasBoost && boost.Active

	// Boost: activate or extend
	if isBoostActive {
		s.world.PushEvent(event.EventBoostExtend, &event.BoostExtendPayload{
			Entity:   cursor,
			Duration: parameter.BoostExtensionDuration,
		})
	} else {
		s.world.PushEvent(event.EventBoostActivate, &event.BoostActivatePayload{
			Entity:   cursor,
			Duration: parameter.BoostBaseDuration,
		})
	}

	// Heat: +2 with active boost, +1 without
	// TODO: const
	heatGain := 1
	if isBoostActive {
		heatGain = 2
	}
	s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
		Entity: cursor, Delta: heatGain,
	})

	s.statCorrect.Add(1)

	// Streak is per player, so it advances in the acting cursor's slot
	slot, ok := s.world.CursorSlot(cursor)
	if !ok {
		return
	}
	s.currentStreak[slot]++
	if s.statMaxStreak.Load(slot) < s.currentStreak[slot] {
		s.statMaxStreak.Store(slot, s.currentStreak[slot])
	}
}

// emitTypingFeedback sends visual feedback to the acting cursor
func (s *TypingSystem) emitTypingFeedback(cursor core.Entity, glyphType component.GlyphType) {
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
		Entity: cursor,
		Type:   blinkType,
	})
}

// emitTypingError penalizes the acting cursor and breaks its streak
func (s *TypingSystem) emitTypingError(cursor core.Entity) {
	// Set cursor error flash
	if cursorComp, ok := s.world.Components.Cursor.GetPtr(cursor); ok {
		cursorComp.ErrorFlashRemaining = parameter.ErrorBlinkTimeout
	}

	// Reset boost and apply heat penalty
	s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
		Entity: cursor, Delta: -parameter.HeatTypingErrorPenalty,
	})
	s.world.PushEvent(event.EventBoostDeactivate, &event.BoostDeactivatePayload{Entity: cursor})
	s.world.PushEvent(event.EventEnergyBlinkStart, &event.EnergyBlinkPayload{
		Entity: cursor, Type: 0, Level: 0,
	})

	s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
		ID: parameter.Sfx.Error,
	})

	s.statErrors.Add(1)
	if slot, ok := s.world.CursorSlot(cursor); ok {
		s.currentStreak[slot] = 0
	}
}

// moveCursorRight requests the post-typing advance; CursorSystem applies and announces it
func (s *TypingSystem) moveCursorRight(cursor core.Entity) {
	config := s.world.Resources.Config

	if pos, ok := s.world.Positions.GetPosition(cursor); ok && pos.X < config.MapWidth-1 {
		s.world.PushEvent(event.EventCursorMoveRequest, &event.CursorMoveRequestPayload{
			Entity: cursor, X: pos.X + 1, Y: pos.Y,
		})
	}
}

// === HANDLER PATHS ===

// handleCompositeMember validates a composite member typed by one cursor
func (s *TypingSystem) handleCompositeMember(cursor, entity, anchorID core.Entity, typedRune rune) {
	glyph, ok := s.world.Components.Glyph.GetComponent(entity)
	if !ok {
		s.emitTypingError(cursor)
		return
	}

	// Character match check
	if glyph.Rune != typedRune {
		s.emitTypingError(cursor)
		return
	}

	// Identify composite behavior for reward logic
	header, ok := s.world.Components.Header.GetComponent(anchorID)
	if !ok {
		s.emitTypingError(cursor)
		return
	}

	// Validate composite typing order
	if !s.isLeftmostMember(entity, &header) {
		s.emitTypingError(cursor)
		return
	}

	// Universal rewards (boost + heat)
	s.applyUniversalRewards(cursor)

	// Color-based energy (only Blue/Green/Red for now)
	if header.Behavior != component.BehaviorGold {
		s.world.PushEvent(event.EventEnergyGlyphConsumed, &event.EnergyGlyphConsumedPayload{
			Entity: cursor,
			Type:   glyph.Type,
			Level:  glyph.Level,
		})
	}

	// Visual feedback
	s.emitTypingFeedback(cursor, glyph.Type)

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

	s.moveCursorRight(cursor)
}

// handleGlyph validates a standalone glyph typed by one cursor
func (s *TypingSystem) handleGlyph(cursor, entity core.Entity, glyph component.GlyphComponent, typedRune rune) {
	if glyph.Rune != typedRune {
		s.emitTypingError(cursor)
		return
	}

	// Universal rewards
	s.applyUniversalRewards(cursor)

	// Type-specific handling, placeholder for other type additions
	switch glyph.Type {
	case component.GlyphBlue, component.GlyphGreen, component.GlyphRed:
		s.world.PushEvent(event.EventEnergyGlyphConsumed, &event.EnergyGlyphConsumedPayload{
			Entity: cursor,
			Type:   glyph.Type,
			Level:  glyph.Level,
		})
	}

	// Silent Death
	event.EmitDeathOne(s.world.Resources.Event.Queue, entity, 0)

	// Blink typing feedback
	s.emitTypingFeedback(cursor, glyph.Type)
	s.moveCursorRight(cursor)
}

// clearSlot drops a retired cursor's streak record
func (s *TypingSystem) clearSlot(slot uint8) {
	if int(slot) >= parameter.MaxPlayers {
		return
	}
	s.currentStreak[slot] = 0
	s.statMaxStreak.Store(slot, 0)
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
