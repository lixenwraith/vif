package engine

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/components"
)

// TestEntityBuilder verifies that the EntityBuilder compiles and works correctly
func TestEntityBuilder(t *testing.T) {
	w := NewWorldGeneric()

	// Test basic entity creation with generic With function
	char := components.CharacterComponent{Rune: 'A'}
	e1 := With(w.NewEntity(), w.Characters, char).Build()

	if e1 == 0 {
		t.Error("Expected non-zero entity ID")
	}

	// Verify component was added
	retrieved, ok := w.Characters.Get(e1)
	if !ok {
		t.Error("Expected character component to exist")
	}
	if retrieved.Rune != 'A' {
		t.Errorf("Expected rune 'A', got '%c'", retrieved.Rune)
	}

	// Test entity creation with position using WithPosition
	pos := components.PositionComponent{X: 5, Y: 10}
	e2 := WithPosition(w.NewEntity(), w.Positions, pos).Build()

	retrievedPos, ok := w.Positions.Get(e2)
	if !ok {
		t.Error("Expected position component to exist")
	}
	if retrievedPos.X != 5 || retrievedPos.Y != 10 {
		t.Errorf("Expected position (5, 10), got (%d, %d)", retrievedPos.X, retrievedPos.Y)
	}

	// Test chaining multiple With calls
	e3 := WithPosition(
		With(w.NewEntity(), w.Characters, components.CharacterComponent{Rune: 'B'}),
		w.Positions,
		components.PositionComponent{X: 3, Y: 7},
	).Build()

	if _, ok := w.Characters.Get(e3); !ok {
		t.Error("Expected character component on e3")
	}
	if _, ok := w.Positions.Get(e3); !ok {
		t.Error("Expected position component on e3")
	}

	// Test entity ID reservation
	builder := w.NewEntity()
	reservedID := builder.entity
	finalID := builder.Build()

	if reservedID != finalID {
		t.Errorf("Expected reserved ID %d to match final ID %d", reservedID, finalID)
	}
}

// TestEntityBuilder_Panic verifies panic behavior
func TestEntityBuilder_Panic_With(t *testing.T) {
	w := NewWorldGeneric()

	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic when adding component to built entity")
		}
	}()

	builder := w.NewEntity()
	builder.Build()
	With(builder, w.Characters, components.CharacterComponent{Rune: 'X'}) // Should panic
}

func TestEntityBuilder_Panic_WithPosition(t *testing.T) {
	w := NewWorldGeneric()

	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic when adding position to built entity")
		}
	}()

	builder := w.NewEntity()
	builder.Build()
	WithPosition(builder, w.Positions, components.PositionComponent{X: 1, Y: 1}) // Should panic
}

// TestEntityBuilder_ComplexScenario tests a real-world entity creation pattern
func TestEntityBuilder_ComplexScenario(t *testing.T) {
	w := NewWorldGeneric()

	// Create a complete game entity with multiple components
	entity := With(
		WithPosition(
			With(
				w.NewEntity(),
				w.Characters,
				components.CharacterComponent{Rune: 'X'},
			),
			w.Positions,
			components.PositionComponent{X: 10, Y: 20},
		),
		w.Sequences,
		components.SequenceComponent{Text: "test"},
	).Build()

	// Verify all components were added
	if _, ok := w.Characters.Get(entity); !ok {
		t.Error("Missing character component")
	}
	if _, ok := w.Positions.Get(entity); !ok {
		t.Error("Missing position component")
	}
	if seq, ok := w.Sequences.Get(entity); !ok || seq.Text != "test" {
		t.Error("Missing or incorrect sequence component")
	}

	// Verify spatial index was updated
	foundEntity := w.Positions.GetEntityAt(10, 20)
	if foundEntity != entity {
		t.Errorf("Expected entity %d at (10, 20), got %d", entity, foundEntity)
	}
}
