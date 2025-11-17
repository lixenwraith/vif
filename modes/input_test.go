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
