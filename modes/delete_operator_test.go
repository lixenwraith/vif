package modes

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// Helper function to place a character at a position with spatial index update
func placeCharWithSpatialIndex(ctx *engine.GameContext, x, y int, r rune, seqType components.SequenceType) engine.Entity {
	entity := ctx.World.CreateEntity()
	ctx.World.Positions.Add(entity, components.PositionComponent{X: x, Y: y})
	ctx.World.Characters.Add(entity, components.CharacterComponent{Rune: r, Style: tcell.StyleDefault})
	ctx.World.Sequences.Add(entity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  seqType,
		Level: components.LevelBright,
	})
	// Update spatial index

	tx := ctx.World.BeginSpatialTransaction()
	tx.Spawn(entity, x, y)
	tx.Commit()
	return entity
}

// Helper to count entities at specific positions
func countEntitiesInRange(ctx *engine.GameContext, startX, endX, y int) int {
	count := 0
	for x := startX; x <= endX; x++ {
		entity := ctx.World.GetEntityAtPosition(x, y)
		if entity != 0 {
			count++
		}
	}
	return count
}

// TestDeleteRangeWithNoGaps tests deleting a continuous range of characters
func TestDeleteRangeWithNoGaps(t *testing.T) {
	ctx := createTestContext()

	// Place "hello" at positions 0-4 on line 0
	placeCharWithSpatialIndex(ctx, 0, 0, 'h', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 1, 0, 'e', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 2, 0, 'l', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 3, 0, 'l', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 4, 0, 'o', components.SequenceGreen)

	// Delete range 1-3 (should delete "ell")
	deletedGreenOrBlue := deleteRange(ctx, 1, 3, 0)

	if !deletedGreenOrBlue {
		t.Error("Expected deletedGreenOrBlue to be true when deleting green characters")
	}

	// Check that positions 1-3 are now empty
	if ctx.World.GetEntityAtPosition(1, 0) != 0 {
		t.Error("Position 1 should be empty after deletion")
	}
	if ctx.World.GetEntityAtPosition(2, 0) != 0 {
		t.Error("Position 2 should be empty after deletion")
	}
	if ctx.World.GetEntityAtPosition(3, 0) != 0 {
		t.Error("Position 3 should be empty after deletion")
	}

	// Check that positions 0 and 4 still have entities
	if ctx.World.GetEntityAtPosition(0, 0) == 0 {
		t.Error("Position 0 should still have entity 'h'")
	}
	if ctx.World.GetEntityAtPosition(4, 0) == 0 {
		t.Error("Position 4 should still have entity 'o'")
	}

	// Verify total entity count
	entities := ctx.World.Positions.All()
	if len(entities) != 2 {
		t.Errorf("Expected 2 entities remaining, got %d", len(entities))
	}
}

// TestDeleteRangeWithGaps tests deleting a range that includes gaps (spaces)
func TestDeleteRangeWithGaps(t *testing.T) {
	ctx := createTestContext()

	// Place "foo bar" with gaps at positions 3-4 (spaces)
	// "foo" at 0-2, gap at 3-4, "bar" at 5-7
	placeCharWithSpatialIndex(ctx, 0, 0, 'f', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 1, 0, 'o', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 2, 0, 'o', components.SequenceGreen)
	// Gap at positions 3-4 (no entities)
	placeCharWithSpatialIndex(ctx, 5, 0, 'b', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 6, 0, 'a', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 7, 0, 'r', components.SequenceGreen)

	// Delete range 1-6 (should delete "oo", skip gaps at 3-4, delete "ba")
	deletedGreenOrBlue := deleteRange(ctx, 1, 6, 0)

	if !deletedGreenOrBlue {
		t.Error("Expected deletedGreenOrBlue to be true when deleting green characters")
	}

	// Check that only entities at positions 0 and 7 remain
	if ctx.World.GetEntityAtPosition(0, 0) == 0 {
		t.Error("Position 0 should still have entity 'f'")
	}
	if ctx.World.GetEntityAtPosition(7, 0) == 0 {
		t.Error("Position 7 should still have entity 'r'")
	}

	// Check that deleted positions are empty
	if ctx.World.GetEntityAtPosition(1, 0) != 0 {
		t.Error("Position 1 should be empty after deletion")
	}
	if ctx.World.GetEntityAtPosition(2, 0) != 0 {
		t.Error("Position 2 should be empty after deletion")
	}
	if ctx.World.GetEntityAtPosition(5, 0) != 0 {
		t.Error("Position 5 should be empty after deletion")
	}
	if ctx.World.GetEntityAtPosition(6, 0) != 0 {
		t.Error("Position 6 should be empty after deletion")
	}

	// Verify total entity count
	entities := ctx.World.Positions.All()
	if len(entities) != 2 {
		t.Errorf("Expected 2 entities remaining, got %d", len(entities))
	}
}

// TestDeleteRangeEntirelyGaps tests deleting a range that contains only gaps
func TestDeleteRangeEntirelyGaps(t *testing.T) {
	ctx := createTestContext()

	// Place characters at positions 0 and 10, with gaps in between
	placeCharWithSpatialIndex(ctx, 0, 0, 'a', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 10, 0, 'b', components.SequenceGreen)

	// Delete range 2-8 (all gaps, no entities)
	deletedGreenOrBlue := deleteRange(ctx, 2, 8, 0)

	if deletedGreenOrBlue {
		t.Error("Expected deletedGreenOrBlue to be false when deleting only gaps")
	}

	// Check that original entities still exist
	if ctx.World.GetEntityAtPosition(0, 0) == 0 {
		t.Error("Position 0 should still have entity 'a'")
	}
	if ctx.World.GetEntityAtPosition(10, 0) == 0 {
		t.Error("Position 10 should still have entity 'b'")
	}

	// Verify total entity count unchanged
	entities := ctx.World.Positions.All()
	if len(entities) != 2 {
		t.Errorf("Expected 2 entities remaining, got %d", len(entities))
	}
}

// TestDeleteRangeRedCharacters tests that red characters don't trigger heat reset
func TestDeleteRangeRedCharacters(t *testing.T) {
	ctx := createTestContext()

	// Place red characters
	placeCharWithSpatialIndex(ctx, 0, 0, 'a', components.SequenceRed)
	placeCharWithSpatialIndex(ctx, 1, 0, 'b', components.SequenceRed)
	placeCharWithSpatialIndex(ctx, 2, 0, 'c', components.SequenceRed)

	// Delete range
	deletedGreenOrBlue := deleteRange(ctx, 0, 2, 0)

	if deletedGreenOrBlue {
		t.Error("Expected deletedGreenOrBlue to be false when deleting only red characters")
	}

	// Verify entities were deleted
	entities := ctx.World.Positions.All()
	if len(entities) != 0 {
		t.Errorf("Expected 0 entities remaining, got %d", len(entities))
	}
}

// TestDeleteRangeMixedColors tests deleting a mix of colors
func TestDeleteRangeMixedColors(t *testing.T) {
	ctx := createTestContext()

	// Place mixed colored characters
	placeCharWithSpatialIndex(ctx, 0, 0, 'a', components.SequenceRed)
	placeCharWithSpatialIndex(ctx, 1, 0, 'b', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 2, 0, 'c', components.SequenceBlue)
	placeCharWithSpatialIndex(ctx, 3, 0, 'd', components.SequenceRed)

	// Delete range
	deletedGreenOrBlue := deleteRange(ctx, 0, 3, 0)

	if !deletedGreenOrBlue {
		t.Error("Expected deletedGreenOrBlue to be true when deleting green/blue characters")
	}

	// Verify all entities were deleted
	entities := ctx.World.Positions.All()
	if len(entities) != 0 {
		t.Errorf("Expected 0 entities remaining, got %d", len(entities))
	}
}

// TestDeleteRangeSwappedBounds tests that deleteRange handles swapped bounds correctly
func TestDeleteRangeSwappedBounds(t *testing.T) {
	ctx := createTestContext()

	// Place characters
	placeCharWithSpatialIndex(ctx, 0, 0, 'a', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 1, 0, 'b', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 2, 0, 'c', components.SequenceGreen)

	// Delete range with swapped bounds (endX < startX)
	deletedGreenOrBlue := deleteRange(ctx, 2, 0, 0)

	if !deletedGreenOrBlue {
		t.Error("Expected deletedGreenOrBlue to be true")
	}

	// Verify all entities were deleted
	entities := ctx.World.Positions.All()
	if len(entities) != 0 {
		t.Errorf("Expected 0 entities remaining, got %d", len(entities))
	}
}

// TestExecuteDeleteMotionDW tests dw (delete word) with gaps
func TestExecuteDeleteMotionDW(t *testing.T) {
	ctx := createTestContext()
	ctx.CursorX = 0
	ctx.CursorY = 0

	// Place "foo bar" with gap at positions 3-4
	placeCharWithSpatialIndex(ctx, 0, 0, 'f', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 1, 0, 'o', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 2, 0, 'o', components.SequenceGreen)
	// Gap at 3-4
	placeCharWithSpatialIndex(ctx, 5, 0, 'b', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 6, 0, 'a', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 7, 0, 'r', components.SequenceGreen)

	// Execute dw from position 0
	ExecuteDeleteMotion(ctx, 'w', 1)

	// "foo" and the gap should be deleted, "bar" should remain
	// Check that "bar" at positions 5-7 still exists
	if ctx.World.GetEntityAtPosition(5, 0) == 0 {
		t.Error("Position 5 should still have entity 'b'")
	}
	if ctx.World.GetEntityAtPosition(6, 0) == 0 {
		t.Error("Position 6 should still have entity 'a'")
	}
	if ctx.World.GetEntityAtPosition(7, 0) == 0 {
		t.Error("Position 7 should still have entity 'r'")
	}

	// Check that "foo" was deleted
	if ctx.World.GetEntityAtPosition(0, 0) != 0 {
		t.Error("Position 0 should be empty after dw")
	}
	if ctx.World.GetEntityAtPosition(1, 0) != 0 {
		t.Error("Position 1 should be empty after dw")
	}
	if ctx.World.GetEntityAtPosition(2, 0) != 0 {
		t.Error("Position 2 should be empty after dw")
	}
}

// TestExecuteDeleteMotionDE tests de (delete to end of word) with gaps
func TestExecuteDeleteMotionDE(t *testing.T) {
	ctx := createTestContext()
	ctx.CursorX = 5
	ctx.CursorY = 0

	// Place "foo bar" with gap at positions 3-4
	placeCharWithSpatialIndex(ctx, 0, 0, 'f', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 1, 0, 'o', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 2, 0, 'o', components.SequenceGreen)
	// Gap at 3-4
	placeCharWithSpatialIndex(ctx, 5, 0, 'b', components.SequenceBlue)
	placeCharWithSpatialIndex(ctx, 6, 0, 'a', components.SequenceBlue)
	placeCharWithSpatialIndex(ctx, 7, 0, 'r', components.SequenceBlue)

	// Execute de from position 5 (on 'b')
	ExecuteDeleteMotion(ctx, 'e', 1)

	// "bar" should be deleted, "foo" should remain
	if ctx.World.GetEntityAtPosition(0, 0) == 0 {
		t.Error("Position 0 should still have entity 'f'")
	}
	if ctx.World.GetEntityAtPosition(1, 0) == 0 {
		t.Error("Position 1 should still have entity 'o'")
	}
	if ctx.World.GetEntityAtPosition(2, 0) == 0 {
		t.Error("Position 2 should still have entity 'o'")
	}

	// Check that "bar" was deleted
	if ctx.World.GetEntityAtPosition(5, 0) != 0 {
		t.Error("Position 5 should be empty after de")
	}
	if ctx.World.GetEntityAtPosition(6, 0) != 0 {
		t.Error("Position 6 should be empty after de")
	}
	if ctx.World.GetEntityAtPosition(7, 0) != 0 {
		t.Error("Position 7 should be empty after de")
	}
}

// TestExecuteDeleteMotionDollarSign tests d$ with gaps
func TestExecuteDeleteMotionDollarSign(t *testing.T) {
	ctx := createTestContext()
	ctx.CursorX = 1
	ctx.CursorY = 0

	// Place "a bc d" with gaps
	placeCharWithSpatialIndex(ctx, 0, 0, 'a', components.SequenceGreen)
	// Gap at 1
	placeCharWithSpatialIndex(ctx, 2, 0, 'b', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 3, 0, 'c', components.SequenceGreen)
	// Gap at 4
	placeCharWithSpatialIndex(ctx, 5, 0, 'd', components.SequenceGreen)

	// Execute d$ from position 1
	ExecuteDeleteMotion(ctx, '$', 1)

	// Only 'a' at position 0 should remain
	if ctx.World.GetEntityAtPosition(0, 0) == 0 {
		t.Error("Position 0 should still have entity 'a'")
	}

	// Everything else should be deleted
	count := countEntitiesInRange(ctx, 1, ctx.GameWidth-1, 0)
	if count != 0 {
		t.Errorf("Expected 0 entities from position 1 onwards, got %d", count)
	}
}

// TestDeleteAllOnLineWithGaps tests deleting all characters on a line with gaps
func TestDeleteAllOnLineWithGaps(t *testing.T) {
	ctx := createTestContext()

	// Place characters with gaps on line 0
	placeCharWithSpatialIndex(ctx, 0, 0, 'a', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 2, 0, 'b', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 5, 0, 'c', components.SequenceGreen)

	// Place characters on line 1 to verify they're not affected
	placeCharWithSpatialIndex(ctx, 0, 1, 'x', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 1, 1, 'y', components.SequenceGreen)

	// Delete all on line 0
	deletedGreenOrBlue := deleteAllOnLine(ctx, 0)

	if !deletedGreenOrBlue {
		t.Error("Expected deletedGreenOrBlue to be true")
	}

	// Verify line 0 is empty
	count := countEntitiesInRange(ctx, 0, ctx.GameWidth-1, 0)
	if count != 0 {
		t.Errorf("Expected 0 entities on line 0, got %d", count)
	}

	// Verify line 1 still has entities
	if ctx.World.GetEntityAtPosition(0, 1) == 0 {
		t.Error("Position (0,1) should still have entity 'x'")
	}
	if ctx.World.GetEntityAtPosition(1, 1) == 0 {
		t.Error("Position (1,1) should still have entity 'y'")
	}
}

// TestDeleteRangeDoesNotDestroyZeroEntity verifies entity ID 0 is never destroyed
func TestDeleteRangeDoesNotDestroyZeroEntity(t *testing.T) {
	ctx := createTestContext()

	// Place some characters with gaps
	placeCharWithSpatialIndex(ctx, 0, 0, 'a', components.SequenceGreen)
	placeCharWithSpatialIndex(ctx, 5, 0, 'b', components.SequenceGreen)

	// Count entities before
	entitiesBefore := len(ctx.World.Positions.All())

	// Delete range that includes gaps (positions 1-4 have no entities)
	deleteRange(ctx, 0, 5, 0)

	// All entities should be deleted
	entitiesAfter := len(ctx.World.Positions.All())
	if entitiesAfter != 0 {
		t.Errorf("Expected 0 entities after deletion, got %d", entitiesAfter)
	}

	// Verify we deleted the expected number (2 entities)
	if entitiesBefore != 2 {
		t.Errorf("Test setup error: expected 2 entities before deletion, got %d", entitiesBefore)
	}
}
