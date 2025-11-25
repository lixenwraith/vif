package modes

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/systems"
)

// MockScreen is a minimal mock for tcell.Screen
type MockScreen struct {
	tcell.Screen
	width, height int
}

func (m *MockScreen) Size() (int, int) {
	return m.width, m.height
}

func (m *MockScreen) Init() error {
	return nil
}

func (m *MockScreen) Fini() {
}

func (m *MockScreen) Clear() {
}

func (m *MockScreen) Show() {
}

func (m *MockScreen) SetContent(x, y int, mainc rune, combc []rune, style tcell.Style) {
}

func (m *MockScreen) Sync() {
}

// TestDeleteOperatorResetOnModeTransitions tests that DeleteOperator flag is reset appropriately
func TestDeleteOperatorResetOnModeTransitions(t *testing.T) {
	mockScreen := &MockScreen{width: 80, height: 24}
	ctx := engine.NewGameContext(mockScreen)
	energySystem := systems.NewEnergySystem(ctx)
	handler := NewInputHandler(ctx, energySystem)

	// Test 1: DeleteOperator reset when pressing 'i' (treated as invalid motion, doesn't enter insert mode)
	ctx.Mode = engine.ModeNormal
	ctx.DeleteOperator = true
	evI := tcell.NewEventKey(tcell.KeyRune, 'i', tcell.ModNone)
	handler.HandleEvent(evI)

	if ctx.DeleteOperator {
		t.Error("Expected DeleteOperator to be reset when pressing 'i'")
	}
	if ctx.Mode != engine.ModeNormal {
		t.Error("Expected to stay in normal mode (i is treated as motion when DeleteOperator is set)")
	}

	// Test 2: DeleteOperator reset when pressing '/' (treated as invalid motion, doesn't enter search mode)
	ctx.Mode = engine.ModeNormal
	ctx.DeleteOperator = true
	evSlash := tcell.NewEventKey(tcell.KeyRune, '/', tcell.ModNone)
	handler.HandleEvent(evSlash)

	if ctx.DeleteOperator {
		t.Error("Expected DeleteOperator to be reset when pressing '/'")
	}
	if ctx.Mode != engine.ModeNormal {
		t.Error("Expected to stay in normal mode (/ is treated as motion when DeleteOperator is set)")
	}

	// Test 3: DeleteOperator reset when pressing Escape from insert mode
	ctx.Mode = engine.ModeInsert
	ctx.DeleteOperator = true
	evEsc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	handler.HandleEvent(evEsc)

	if ctx.DeleteOperator {
		t.Error("Expected DeleteOperator to be reset when escaping from insert mode")
	}
	if ctx.Mode != engine.ModeNormal {
		t.Error("Expected to be in normal mode")
	}

	// Test 4: DeleteOperator reset when pressing Escape from search mode
	ctx.Mode = engine.ModeSearch
	ctx.DeleteOperator = true
	handler.HandleEvent(evEsc)

	if ctx.DeleteOperator {
		t.Error("Expected DeleteOperator to be reset when escaping from search mode")
	}
	if ctx.Mode != engine.ModeNormal {
		t.Error("Expected to be in normal mode")
	}
}

// TestDeleteOperatorPersistenceWithinNormalMode tests that DeleteOperator persists in normal mode until motion
func TestDeleteOperatorPersistenceWithinNormalMode(t *testing.T) {
	mockScreen := &MockScreen{width: 80, height: 24}
	ctx := engine.NewGameContext(mockScreen)
	energySystem := systems.NewEnergySystem(ctx)
	handler := NewInputHandler(ctx, energySystem)

	// Start in normal mode
	ctx.Mode = engine.ModeNormal

	// Press 'd' to enter delete operator mode
	evD := tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone)
	handler.HandleEvent(evD)

	if !ctx.DeleteOperator {
		t.Error("Expected DeleteOperator to be set after pressing 'd'")
	}

	// Verify that pressing a number doesn't reset it (count accumulation)
	ev2 := tcell.NewEventKey(tcell.KeyRune, '2', tcell.ModNone)
	handler.HandleEvent(ev2)

	if !ctx.DeleteOperator {
		t.Error("Expected DeleteOperator to persist when entering count")
	}
}

// TestDeleteOperatorResetSequence tests complete sequence: d -> various keys -> verify reset
func TestDeleteOperatorResetSequence(t *testing.T) {
	mockScreen := &MockScreen{width: 80, height: 24}
	ctx := engine.NewGameContext(mockScreen)
	energySystem := systems.NewEnergySystem(ctx)
	handler := NewInputHandler(ctx, energySystem)

	tests := []struct {
		name         string
		setupMode    engine.GameMode
		triggerEvent *tcell.EventKey
		expectedMode engine.GameMode
	}{
		{
			name:         "Normal -> 'i' with DeleteOperator (stays Normal)",
			setupMode:    engine.ModeNormal,
			triggerEvent: tcell.NewEventKey(tcell.KeyRune, 'i', tcell.ModNone),
			expectedMode: engine.ModeNormal, // i is treated as motion when DeleteOperator is set
		},
		{
			name:         "Normal -> '/' with DeleteOperator (stays Normal)",
			setupMode:    engine.ModeNormal,
			triggerEvent: tcell.NewEventKey(tcell.KeyRune, '/', tcell.ModNone),
			expectedMode: engine.ModeNormal, // / is treated as motion when DeleteOperator is set
		},
		{
			name:         "Insert -> Normal via Escape",
			setupMode:    engine.ModeInsert,
			triggerEvent: tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone),
			expectedMode: engine.ModeNormal,
		},
		{
			name:         "Search -> Normal via Escape",
			setupMode:    engine.ModeSearch,
			triggerEvent: tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone),
			expectedMode: engine.ModeNormal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup: set mode and DeleteOperator flag
			ctx.Mode = tt.setupMode
			ctx.DeleteOperator = true

			// Trigger the event
			handler.HandleEvent(tt.triggerEvent)

			// Verify DeleteOperator is reset
			if ctx.DeleteOperator {
				t.Errorf("Expected DeleteOperator to be reset after %s", tt.name)
			}

			// Verify mode changed as expected
			if ctx.Mode != tt.expectedMode {
				t.Errorf("Expected mode %v, got %v", tt.expectedMode, ctx.Mode)
			}
		})
	}
}