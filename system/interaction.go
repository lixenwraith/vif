package system

import (
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// CursorOverlap holds results of cursor/shield spatial query for an entity
type CursorOverlap struct {
	OnCursor      bool          // Any part occupies cursor cell
	ShieldActive  bool          // Cursor shield is currently active
	ShieldMembers []core.Entity // Members (or self) inside shield ellipse
	CursorMembers []core.Entity // Members (or self) on exact cursor cell
}

// CheckCursorOverlap queries cursor cell and shield ellipse overlap for an entity.
// For composite headers: iterates MemberEntries.
// For simple entities: checks entity position directly.
// Returns zero-value CursorOverlap on missing cursor/position data.
func CheckCursorOverlap(w *engine.World, entity core.Entity) CursorOverlap {
	cursorEntity := w.Resources.Player.Entity
	cursorPos, ok := w.Positions.GetPosition(cursorEntity)
	if !ok {
		return CursorOverlap{}
	}

	shieldComp, shieldOK := w.Components.Shield.GetComponent(cursorEntity)
	shieldActive := shieldOK && shieldComp.Active

	result := CursorOverlap{ShieldActive: shieldActive}

	// Composite: iterate members
	if headerComp, ok := w.Components.Header.GetComponent(entity); ok {
		for _, member := range headerComp.MemberEntries {
			if member.Entity == 0 {
				continue
			}
			memberPos, ok := w.Positions.GetPosition(member.Entity)
			if !ok {
				continue
			}

			if memberPos.X == cursorPos.X && memberPos.Y == cursorPos.Y {
				result.OnCursor = true
				result.CursorMembers = append(result.CursorMembers, member.Entity)
			}

			if shieldActive && vmath.EllipseContainsPoint(memberPos.X, memberPos.Y, cursorPos.X, cursorPos.Y, shieldComp.InvRxSq, shieldComp.InvRySq) {
				result.ShieldMembers = append(result.ShieldMembers, member.Entity)
			}
		}
		return result
	}

	// Simple entity: check own position
	pos, ok := w.Positions.GetPosition(entity)
	if !ok {
		return CursorOverlap{}
	}

	if pos.X == cursorPos.X && pos.Y == cursorPos.Y {
		result.OnCursor = true
		result.CursorMembers = append(result.CursorMembers, entity)
	}

	if shieldActive && vmath.EllipseContainsPoint(pos.X, pos.Y, cursorPos.X, cursorPos.Y, shieldComp.InvRxSq, shieldComp.InvRySq) {
		result.ShieldMembers = append(result.ShieldMembers, entity)
	}

	return result
}