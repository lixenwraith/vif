package engine

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/components"
)

func TestCursorGhostingCharacter(t *testing.T) {
	// 1. Setup World and Store
	ps := NewPositionStore()

	// 2. Create a "Content" Entity (e.g., a character 'a') at (5, 5)
	charEntity := Entity(100)
	charPos := components.PositionComponent{X: 5, Y: 5}
	ps.Add(charEntity, charPos)

	// Verify Character is spatially indexed
	if found := ps.GetEntityAt(5, 5); found != charEntity {
		t.Fatalf("Setup failed: Expected character at (5,5), found %d", found)
	}

	// 3. Create a "Cursor" Entity at (0, 0)
	cursorEntity := Entity(999)
	cursorPos := components.PositionComponent{X: 0, Y: 0}
	ps.Add(cursorEntity, cursorPos)

	// 4. Move Cursor ON TOP of the Character
	// This simulates InputHandler calling world.Positions.Add
	cursorPos.X = 5
	cursorPos.Y = 5
	ps.Add(cursorEntity, cursorPos)

	// VERIFICATION 1: The Spatial Index now points to Cursor, not Character
	if found := ps.GetEntityAt(5, 5); found != cursorEntity {
		t.Errorf("Collision state unexpected: Expected Cursor at (5,5), found %d", found)
	}

	// 5. Move Cursor AWAY from the Character
	cursorPos.X = 6
	cursorPos.Y = 5
	ps.Add(cursorEntity, cursorPos)

	// VERIFICATION 2 (The Bug):
	// The Spatial Index at (5,5) should ideally point back to Character,
	// or at least not be empty. In the current bug, it will be 0.
	foundAtOldPos := ps.GetEntityAt(5, 5)

	if foundAtOldPos == 0 {
		t.Fatalf("BUG REPRODUCED: Character at (5,5) was 'ghosted'. Spatial index is empty after cursor left.")
	}

	if foundAtOldPos != charEntity {
		t.Errorf("Expected Character (%d) at (5,5), but found %d", charEntity, foundAtOldPos)
	}
}
