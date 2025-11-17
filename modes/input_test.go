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

// TestCtrlSTogglesSoundEnabled tests that Ctrl+S toggles the SoundEnabled flag
func TestCtrlSTogglesSoundEnabled(t *testing.T) {
	// Create mock screen
	mockScreen := &MockScreen{width: 80, height: 24}

	// Create game context
	ctx := engine.NewGameContext(mockScreen)
	scoreSystem := systems.NewScoreSystem(ctx)
	handler := NewInputHandler(ctx, scoreSystem)

	// Verify initial state is false
	if ctx.SoundEnabled.Load() {
		t.Error("Expected initial SoundEnabled to be false")
	}

	// Create Ctrl+S event
	ev := tcell.NewEventKey(tcell.KeyCtrlS, 0, tcell.ModCtrl)

	// Handle the event
	result := handler.HandleEvent(ev)

	// Verify the event was handled (returned true)
	if !result {
		t.Error("Expected Ctrl+S to return true (continue game)")
	}

	// Verify SoundEnabled was toggled to true
	if !ctx.SoundEnabled.Load() {
		t.Error("Expected SoundEnabled to be true after first Ctrl+S")
	}

	// Handle Ctrl+S again
	handler.HandleEvent(ev)

	// Verify SoundEnabled was toggled back to false
	if ctx.SoundEnabled.Load() {
		t.Error("Expected SoundEnabled to be false after second Ctrl+S")
	}
}

// TestCtrlSWorksInAllModes tests that Ctrl+S works in Normal, Insert, and Search modes
func TestCtrlSWorksInAllModes(t *testing.T) {
	mockScreen := &MockScreen{width: 80, height: 24}
	ctx := engine.NewGameContext(mockScreen)
	scoreSystem := systems.NewScoreSystem(ctx)
	handler := NewInputHandler(ctx, scoreSystem)

	ev := tcell.NewEventKey(tcell.KeyCtrlS, 0, tcell.ModCtrl)

	// Test in Normal mode
	ctx.Mode = engine.ModeNormal
	ctx.SoundEnabled.Store(false)
	handler.HandleEvent(ev)
	if !ctx.SoundEnabled.Load() {
		t.Error("Expected Ctrl+S to work in Normal mode")
	}

	// Test in Insert mode
	ctx.Mode = engine.ModeInsert
	ctx.SoundEnabled.Store(false)
	handler.HandleEvent(ev)
	if !ctx.SoundEnabled.Load() {
		t.Error("Expected Ctrl+S to work in Insert mode")
	}

	// Test in Search mode
	ctx.Mode = engine.ModeSearch
	ctx.SoundEnabled.Store(false)
	handler.HandleEvent(ev)
	if !ctx.SoundEnabled.Load() {
		t.Error("Expected Ctrl+S to work in Search mode")
	}
}

// TestCtrlSDoesNotAffectOtherControls tests that other Ctrl keys still work
func TestCtrlSDoesNotAffectOtherControls(t *testing.T) {
	mockScreen := &MockScreen{width: 80, height: 24}
	ctx := engine.NewGameContext(mockScreen)
	scoreSystem := systems.NewScoreSystem(ctx)
	handler := NewInputHandler(ctx, scoreSystem)

	// Test Ctrl+Q (should exit)
	evQ := tcell.NewEventKey(tcell.KeyCtrlQ, 0, tcell.ModCtrl)
	result := handler.HandleEvent(evQ)
	if result {
		t.Error("Expected Ctrl+Q to return false (exit game)")
	}

	// Test Ctrl+C (should exit)
	evC := tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModCtrl)
	result = handler.HandleEvent(evC)
	if result {
		t.Error("Expected Ctrl+C to return false (exit game)")
	}
}

// TestCtrlSMultipleToggles tests multiple consecutive Ctrl+S presses
func TestCtrlSMultipleToggles(t *testing.T) {
	mockScreen := &MockScreen{width: 80, height: 24}
	ctx := engine.NewGameContext(mockScreen)
	scoreSystem := systems.NewScoreSystem(ctx)
	handler := NewInputHandler(ctx, scoreSystem)

	ev := tcell.NewEventKey(tcell.KeyCtrlS, 0, tcell.ModCtrl)

	// Toggle 6 times
	for i := 0; i < 6; i++ {
		handler.HandleEvent(ev)
		expected := (i % 2) == 0 // Should be true on even iterations (0, 2, 4)
		actual := ctx.SoundEnabled.Load()

		if actual != expected {
			t.Errorf("After %d Ctrl+S presses, expected SoundEnabled=%v, got %v", i+1, expected, actual)
		}
	}

	// After 6 toggles (even number), should be back to false
	if ctx.SoundEnabled.Load() {
		t.Error("Expected SoundEnabled to be false after even number of toggles")
	}
}

// TestDeleteOperatorResetOnModeTransitions tests that DeleteOperator flag is reset when changing modes
func TestDeleteOperatorResetOnModeTransitions(t *testing.T) {
	mockScreen := &MockScreen{width: 80, height: 24}
	ctx := engine.NewGameContext(mockScreen)
	scoreSystem := systems.NewScoreSystem(ctx)
	handler := NewInputHandler(ctx, scoreSystem)

	// Test 1: DeleteOperator reset when pressing 'i' to enter insert mode
	ctx.Mode = engine.ModeNormal
	ctx.DeleteOperator = true
	evI := tcell.NewEventKey(tcell.KeyRune, 'i', tcell.ModNone)
	handler.HandleEvent(evI)

	if ctx.DeleteOperator {
		t.Error("Expected DeleteOperator to be reset when entering insert mode with 'i'")
	}
	if ctx.Mode != engine.ModeInsert {
		t.Error("Expected to be in insert mode")
	}

	// Test 2: DeleteOperator reset when pressing '/' to enter search mode
	ctx.Mode = engine.ModeNormal
	ctx.DeleteOperator = true
	evSlash := tcell.NewEventKey(tcell.KeyRune, '/', tcell.ModNone)
	handler.HandleEvent(evSlash)

	if ctx.DeleteOperator {
		t.Error("Expected DeleteOperator to be reset when entering search mode with '/'")
	}
	if ctx.Mode != engine.ModeSearch {
		t.Error("Expected to be in search mode")
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
	scoreSystem := systems.NewScoreSystem(ctx)
	handler := NewInputHandler(ctx, scoreSystem)

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

// TestDeleteOperatorResetSequence tests complete sequence: d -> mode change -> verify reset
func TestDeleteOperatorResetSequence(t *testing.T) {
	mockScreen := &MockScreen{width: 80, height: 24}
	ctx := engine.NewGameContext(mockScreen)
	scoreSystem := systems.NewScoreSystem(ctx)
	handler := NewInputHandler(ctx, scoreSystem)

	tests := []struct {
		name         string
		setupMode    engine.GameMode
		triggerEvent *tcell.EventKey
		expectedMode engine.GameMode
	}{
		{
			name:         "Normal -> Insert via 'i'",
			setupMode:    engine.ModeNormal,
			triggerEvent: tcell.NewEventKey(tcell.KeyRune, 'i', tcell.ModNone),
			expectedMode: engine.ModeInsert,
		},
		{
			name:         "Normal -> Search via '/'",
			setupMode:    engine.ModeNormal,
			triggerEvent: tcell.NewEventKey(tcell.KeyRune, '/', tcell.ModNone),
			expectedMode: engine.ModeSearch,
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

			// Trigger the mode transition
			handler.HandleEvent(tt.triggerEvent)

			// Verify DeleteOperator is reset
			if ctx.DeleteOperator {
				t.Errorf("Expected DeleteOperator to be reset after %s transition", tt.name)
			}

			// Verify mode changed as expected
			if ctx.Mode != tt.expectedMode {
				t.Errorf("Expected mode %v, got %v", tt.expectedMode, ctx.Mode)
			}
		})
	}
}
