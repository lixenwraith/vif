package engine

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/components"
)

// TestQueryBuilder verifies that the QueryBuilder compiles and works correctly
func TestQueryBuilder(t *testing.T) {
	w := NewWorldGeneric()

	// Create some test entities
	e1 := w.CreateEntity()
	w.Positions.Add(e1, components.PositionComponent{X: 1, Y: 1})
	w.Characters.Add(e1, components.CharacterComponent{Rune: 'A'})

	e2 := w.CreateEntity()
	w.Positions.Add(e2, components.PositionComponent{X: 2, Y: 2})

	e3 := w.CreateEntity()
	w.Characters.Add(e3, components.CharacterComponent{Rune: 'B'})

	// Test query with both components
	results := w.Query().
		With(w.Positions).
		With(w.Characters).
		Execute()

	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}

	if len(results) > 0 && results[0] != e1 {
		t.Errorf("Expected entity %d, got %d", e1, results[0])
	}

	// Test query with single component
	posResults := w.Query().
		With(w.Positions).
		Execute()

	if len(posResults) != 2 {
		t.Errorf("Expected 2 position results, got %d", len(posResults))
	}

	// Test empty query
	emptyResults := w.Query().Execute()
	if len(emptyResults) != 0 {
		t.Errorf("Expected 0 empty results, got %d", len(emptyResults))
	}

	// Test query reexecution
	cached := w.Query().With(w.Positions).Execute()
	// Second call should return cached results
	_ = cached
}

// TestQueryBuilder_Panic verifies panic behavior
func TestQueryBuilder_Panic(t *testing.T) {
	w := NewWorldGeneric()

	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic when modifying executed query")
		}
	}()

	q := w.Query()
	q.Execute()
	q.With(w.Positions) // Should panic
}
