package system

import (
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// CursorOverlap describes one cursor's spatial contact with an entity.
type CursorOverlap struct {
	Cursor        core.Entity   // Cursor identifies the contacted player.
	Slot          uint8         // Slot identifies Cursor's roster slot.
	OnCursor      bool          // OnCursor reports whether any part occupies the cursor cell.
	ShieldActive  bool          // ShieldActive reports whether the cursor shield is active.
	ShieldMembers []core.Entity // ShieldMembers lists parts inside the shield ellipse.
	CursorMembers []core.Entity // CursorMembers lists parts on the exact cursor cell.
}

// CursorOverlaps holds every cursor contact in roster order.
type CursorOverlaps struct {
	Entries [parameter.MaxPlayers]CursorOverlap
	Count   int
}

// ClosestCursor returns the nearest rostered cursor in deterministic slot order.
func ClosestCursor(w *engine.World, fromX, fromY int) (core.Entity, int, int, bool) {
	var best core.Entity
	bestX, bestY, bestDist := 0, 0, -1
	for i := range parameter.MaxPlayers {
		e := w.Resources.Player.Slot(uint8(i))
		pos, ok := w.Positions.GetPosition(e)
		if !ok {
			continue
		}
		dx, dy := pos.X-fromX, pos.Y-fromY
		dist := dx*dx + dy*dy
		if bestDist < 0 || dist < bestDist {
			best = e
			bestX, bestY = pos.X, pos.Y
			bestDist = dist
		}
	}
	return best, bestX, bestY, best != 0
}

// CheckCursorOverlaps queries every cursor that touches an entity or its shield.
func CheckCursorOverlaps(w *engine.World, entity core.Entity) CursorOverlaps {
	var result CursorOverlaps
	for i := range parameter.MaxPlayers {
		cursor := w.Resources.Player.Slot(uint8(i))
		if cursor == 0 {
			continue
		}
		overlap := checkCursorOverlap(w, cursor, entity)
		if !overlap.OnCursor && len(overlap.ShieldMembers) == 0 {
			continue
		}
		overlap.Cursor = cursor
		overlap.Slot = uint8(i)
		result.Entries[result.Count] = overlap
		result.Count++
	}
	return result
}

// checkCursorOverlap evaluates one cursor against a simple or composite entity.
func checkCursorOverlap(w *engine.World, cursorEntity, entity core.Entity) CursorOverlap {
	cursorPos, ok := w.Positions.GetPosition(cursorEntity)
	if !ok {
		return CursorOverlap{}
	}

	shieldComp, shieldOK := w.Components.Shield.GetPtr(cursorEntity)
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

			if shieldActive && vmath.EllipseContainsPointF(memberPos.X, memberPos.Y, cursorPos.X, cursorPos.Y, shieldComp.InvRxSq, shieldComp.InvRySq) {
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

	if shieldActive && vmath.EllipseContainsPointF(pos.X, pos.Y, cursorPos.X, cursorPos.Y, shieldComp.InvRxSq, shieldComp.InvRySq) {
		result.ShieldMembers = append(result.ShieldMembers, entity)
	}

	return result
}
