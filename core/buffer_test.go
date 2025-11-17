package core

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestNewBuffer(t *testing.T) {
	width, height := 80, 24
	buf := NewBuffer(width, height)

	if buf.Width() != width {
		t.Errorf("Expected width %d, got %d", width, buf.Width())
	}
	if buf.Height() != height {
		t.Errorf("Expected height %d, got %d", height, buf.Height())
	}

	// Verify all cells are initialized to space with default style
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			cell, ok := buf.GetCell(x, y)
			if !ok {
				t.Errorf("Expected cell at (%d, %d) to exist", x, y)
			}
			if cell.Rune != ' ' {
				t.Errorf("Expected cell at (%d, %d) to be space, got %v", x, y, cell.Rune)
			}
			if cell.Entity != 0 {
				t.Errorf("Expected cell at (%d, %d) to have no entity, got %d", x, y, cell.Entity)
			}
		}
	}
}

func TestGetSetCell(t *testing.T) {
	buf := NewBuffer(10, 10)

	// Test setting a cell
	cell := Cell{
		Rune:   'A',
		Style:  tcell.StyleDefault.Foreground(tcell.ColorRed),
		Entity: 123,
	}

	if !buf.SetCell(5, 5, cell) {
		t.Error("Expected SetCell to succeed")
	}

	// Test getting the cell
	retrieved, ok := buf.GetCell(5, 5)
	if !ok {
		t.Error("Expected GetCell to succeed")
	}
	if retrieved.Rune != 'A' {
		t.Errorf("Expected Rune 'A', got %v", retrieved.Rune)
	}
	if retrieved.Entity != 123 {
		t.Errorf("Expected Entity 123, got %d", retrieved.Entity)
	}

	// Test out of bounds
	if buf.SetCell(-1, 5, cell) {
		t.Error("Expected SetCell to fail for negative x")
	}
	if buf.SetCell(5, 100, cell) {
		t.Error("Expected SetCell to fail for y out of bounds")
	}

	if _, ok := buf.GetCell(-1, 5); ok {
		t.Error("Expected GetCell to fail for negative x")
	}
	if _, ok := buf.GetCell(5, 100); ok {
		t.Error("Expected GetCell to fail for y out of bounds")
	}
}

func TestSetContent(t *testing.T) {
	buf := NewBuffer(10, 10)

	if !buf.SetContent(3, 4, 'X', tcell.StyleDefault.Bold(true), 456) {
		t.Error("Expected SetContent to succeed")
	}

	cell, ok := buf.GetCell(3, 4)
	if !ok {
		t.Error("Expected GetCell to succeed")
	}
	if cell.Rune != 'X' {
		t.Errorf("Expected Rune 'X', got %v", cell.Rune)
	}
	if cell.Entity != 456 {
		t.Errorf("Expected Entity 456, got %d", cell.Entity)
	}
}

func TestSpatialIndex(t *testing.T) {
	buf := NewBuffer(10, 10)

	// Set cell with entity
	buf.SetContent(2, 3, 'E', tcell.StyleDefault, 789)

	// Test GetEntityAt
	entity := buf.GetEntityAt(2, 3)
	if entity != 789 {
		t.Errorf("Expected entity 789, got %d", entity)
	}

	// Test HasEntityAt
	if !buf.HasEntityAt(2, 3) {
		t.Error("Expected entity at (2, 3)")
	}
	if buf.HasEntityAt(5, 5) {
		t.Error("Expected no entity at (5, 5)")
	}

	// Test removing entity
	buf.SetContent(2, 3, 'E', tcell.StyleDefault, 0)
	if buf.HasEntityAt(2, 3) {
		t.Error("Expected no entity after setting to 0")
	}

	// Test out of bounds
	entity = buf.GetEntityAt(-1, 5)
	if entity != 0 {
		t.Errorf("Expected 0 for out of bounds, got %d", entity)
	}
}

func TestClear(t *testing.T) {
	buf := NewBuffer(10, 10)

	// Set some cells
	buf.SetContent(1, 1, 'A', tcell.StyleDefault, 111)
	buf.SetContent(5, 5, 'B', tcell.StyleDefault, 222)

	// Clear with custom style
	clearStyle := tcell.StyleDefault.Background(tcell.ColorBlue)
	buf.Clear(clearStyle)

	// Verify all cells are cleared
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			cell, _ := buf.GetCell(x, y)
			if cell.Rune != ' ' {
				t.Errorf("Expected space at (%d, %d), got %v", x, y, cell.Rune)
			}
			if cell.Entity != 0 {
				t.Errorf("Expected no entity at (%d, %d), got %d", x, y, cell.Entity)
			}
		}
	}

	// Verify spatial index is cleared
	if buf.HasEntityAt(1, 1) || buf.HasEntityAt(5, 5) {
		t.Error("Expected spatial index to be cleared")
	}

	// Verify dirty is cleared
	dirty := buf.DirtyRegions()
	if len(dirty) != 0 {
		t.Errorf("Expected dirty regions to be cleared, got %d regions", len(dirty))
	}
}

func TestDirtyTracking(t *testing.T) {
	buf := NewBuffer(10, 10)

	// Set some cells (should mark as dirty)
	buf.SetCell(1, 1, Cell{Rune: 'A'})
	buf.SetCell(2, 2, Cell{Rune: 'B'})

	// Get dirty regions
	dirty := buf.DirtyRegions()
	if len(dirty) != 2 {
		t.Errorf("Expected 2 dirty regions, got %d", len(dirty))
	}

	// Verify dirty regions contain our points
	dirtyMap := make(map[Point]bool)
	for _, p := range dirty {
		dirtyMap[p] = true
	}
	if !dirtyMap[Point{1, 1}] || !dirtyMap[Point{2, 2}] {
		t.Error("Expected (1,1) and (2,2) to be dirty")
	}

	// Clear dirty
	buf.ClearDirty()
	dirty = buf.DirtyRegions()
	if len(dirty) != 0 {
		t.Errorf("Expected 0 dirty regions after clear, got %d", len(dirty))
	}

	// Mark dirty explicitly
	buf.MarkDirty(5, 5)
	dirty = buf.DirtyRegions()
	if len(dirty) != 1 {
		t.Errorf("Expected 1 dirty region after MarkDirty, got %d", len(dirty))
	}

	// MarkDirty out of bounds should not crash
	buf.MarkDirty(-1, 5)
	buf.MarkDirty(100, 5)
}

func TestResize(t *testing.T) {
	buf := NewBuffer(10, 10)

	// Set some content
	buf.SetContent(2, 2, 'A', tcell.StyleDefault, 100)
	buf.SetContent(8, 8, 'B', tcell.StyleDefault, 200)

	// Resize larger
	buf.Resize(15, 15)
	if buf.Width() != 15 || buf.Height() != 15 {
		t.Errorf("Expected size (15, 15), got (%d, %d)", buf.Width(), buf.Height())
	}

	// Verify preserved content
	cell, _ := buf.GetCell(2, 2)
	if cell.Rune != 'A' || cell.Entity != 100 {
		t.Error("Expected content at (2, 2) to be preserved")
	}

	// Verify new cells are initialized
	cell, _ = buf.GetCell(12, 12)
	if cell.Rune != ' ' {
		t.Error("Expected new cells to be initialized to space")
	}

	// Resize smaller (should clip content)
	buf.Resize(5, 5)
	if buf.Width() != 5 || buf.Height() != 5 {
		t.Errorf("Expected size (5, 5), got (%d, %d)", buf.Width(), buf.Height())
	}

	// Verify (2, 2) is still there
	cell, _ = buf.GetCell(2, 2)
	if cell.Rune != 'A' {
		t.Error("Expected content at (2, 2) to still exist")
	}

	// Verify (8, 8) is gone (out of bounds)
	if _, ok := buf.GetCell(8, 8); ok {
		t.Error("Expected (8, 8) to be out of bounds")
	}

	// Verify spatial index was updated
	if buf.HasEntityAt(8, 8) {
		t.Error("Expected spatial index to be updated after resize")
	}
}

func TestGetLine(t *testing.T) {
	buf := NewBuffer(10, 10)

	// Set some content on line 3
	buf.SetContent(0, 3, 'A', tcell.StyleDefault, 0)
	buf.SetContent(5, 3, 'B', tcell.StyleDefault, 0)
	buf.SetContent(9, 3, 'C', tcell.StyleDefault, 0)

	// Get the line
	line := buf.GetLine(3)
	if line == nil {
		t.Fatal("Expected GetLine to return a line")
	}
	if len(line) != 10 {
		t.Errorf("Expected line length 10, got %d", len(line))
	}
	if line[0].Rune != 'A' || line[5].Rune != 'B' || line[9].Rune != 'C' {
		t.Error("Expected correct content in line")
	}

	// Verify it's a copy (modifying returned line shouldn't affect buffer)
	line[0].Rune = 'X'
	cell, _ := buf.GetCell(0, 3)
	if cell.Rune == 'X' {
		t.Error("Expected GetLine to return a copy, not reference")
	}

	// Test out of bounds
	if buf.GetLine(-1) != nil {
		t.Error("Expected GetLine to return nil for negative index")
	}
	if buf.GetLine(100) != nil {
		t.Error("Expected GetLine to return nil for out of bounds index")
	}
}
