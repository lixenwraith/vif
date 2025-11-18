package engine

import (
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// MockScreen is a minimal mock for tcell.Screen used in tests
type MockScreen struct {
	tcell.Screen
	width, height int
}

func (m *MockScreen) Size() (int, int) {
	if m.width == 0 && m.height == 0 {
		return 80, 24 // Default size
	}
	return m.width, m.height
}

func (m *MockScreen) Init() error          { return nil }
func (m *MockScreen) Fini()                {}
func (m *MockScreen) Clear()               {}
func (m *MockScreen) Show()                {}
func (m *MockScreen) Sync()                {}
func (m *MockScreen) SetContent(x, y int, mainc rune, combc []rune, style tcell.Style) {}

// TestGamePhaseString tests the String() method for GamePhase
func TestGamePhaseString(t *testing.T) {
	tests := []struct {
		phase    GamePhase
		expected string
	}{
		{PhaseNormal, "Normal"},
		{PhaseGoldActive, "GoldActive"},
		{PhaseDecayWait, "DecayWait"},
		{PhaseDecayAnimation, "DecayAnimation"},
		{GamePhase(999), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.phase.String()
			if result != tt.expected {
				t.Errorf("GamePhase(%d).String() = %q, want %q", tt.phase, result, tt.expected)
			}
		})
	}
}

// TestPhaseStateInitialization verifies initial phase state
func TestPhaseStateInitialization(t *testing.T) {
	tp := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(80, 24, 100, tp)

	// Verify initial phase
	phase := gs.GetPhase()
	if phase != PhaseNormal {
		t.Errorf("Initial phase = %v, want %v", phase, PhaseNormal)
	}

	// Verify phase start time
	startTime := gs.GetPhaseStartTime()
	if startTime.IsZero() {
		t.Error("Phase start time should not be zero")
	}

	// Verify phase duration is near zero
	duration := gs.GetPhaseDuration()
	if duration > 10*time.Millisecond {
		t.Errorf("Initial phase duration = %v, expected near zero", duration)
	}
}

// TestPhaseTransitions tests phase state changes
func TestPhaseTransitions(t *testing.T) {
	tp := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(80, 24, 100, tp)

	// Start in Normal phase
	if gs.GetPhase() != PhaseNormal {
		t.Errorf("Expected initial phase PhaseNormal, got %v", gs.GetPhase())
	}

	// Transition to GoldActive
	tp.Advance(1 * time.Second)
	gs.SetPhase(PhaseGoldActive)
	if gs.GetPhase() != PhaseGoldActive {
		t.Errorf("Expected PhaseGoldActive, got %v", gs.GetPhase())
	}

	// Verify phase duration
	tp.Advance(2 * time.Second)
	duration := gs.GetPhaseDuration()
	if duration < 2*time.Second || duration > 2100*time.Millisecond {
		t.Errorf("Phase duration = %v, expected ~2s", duration)
	}

	// Transition to DecayWait
	tp.Advance(500 * time.Millisecond)
	gs.SetPhase(PhaseDecayWait)
	if gs.GetPhase() != PhaseDecayWait {
		t.Errorf("Expected PhaseDecayWait, got %v", gs.GetPhase())
	}

	// Verify duration reset on phase change
	duration = gs.GetPhaseDuration()
	if duration > 10*time.Millisecond {
		t.Errorf("Phase duration after transition = %v, expected near zero", duration)
	}

	// Transition to DecayAnimation
	tp.Advance(30 * time.Second)
	gs.SetPhase(PhaseDecayAnimation)
	if gs.GetPhase() != PhaseDecayAnimation {
		t.Errorf("Expected PhaseDecayAnimation, got %v", gs.GetPhase())
	}

	// Transition back to Normal (cycle complete)
	tp.Advance(5 * time.Second)
	gs.SetPhase(PhaseNormal)
	if gs.GetPhase() != PhaseNormal {
		t.Errorf("Expected PhaseNormal, got %v", gs.GetPhase())
	}
}

// TestPhaseSnapshot tests consistent phase state reads
func TestPhaseSnapshot(t *testing.T) {
	tp := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(80, 24, 100, tp)

	// Set to GoldActive
	gs.SetPhase(PhaseGoldActive)
	tp.Advance(3 * time.Second)

	// Read snapshot
	snapshot := gs.ReadPhaseState()

	if snapshot.Phase != PhaseGoldActive {
		t.Errorf("Snapshot phase = %v, want PhaseGoldActive", snapshot.Phase)
	}

	if snapshot.Duration < 3*time.Second {
		t.Errorf("Snapshot duration = %v, expected >= 3s", snapshot.Duration)
	}

	if snapshot.StartTime.IsZero() {
		t.Error("Snapshot start time should not be zero")
	}
}

// TestConcurrentPhaseReads tests thread-safe phase state access
func TestConcurrentPhaseReads(t *testing.T) {
	tp := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(80, 24, 100, tp)

	const numReaders = 20
	const numReads = 100

	var wg sync.WaitGroup
	wg.Add(numReaders)

	// Launch concurrent readers
	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numReads; j++ {
				_ = gs.GetPhase()
				_ = gs.GetPhaseStartTime()
				_ = gs.GetPhaseDuration()
				_ = gs.ReadPhaseState()
			}
		}()
	}

	// Concurrent writer
	wg.Add(1)
	go func() {
		defer wg.Done()
		phases := []GamePhase{PhaseNormal, PhaseGoldActive, PhaseDecayWait, PhaseDecayAnimation}
		for j := 0; j < 50; j++ {
			gs.SetPhase(phases[j%len(phases)])
			time.Sleep(1 * time.Millisecond)
		}
	}()

	wg.Wait()

	// If we reach here without deadlock or panic, test passes
}

// TestClockSchedulerCreation tests scheduler initialization
func TestClockSchedulerCreation(t *testing.T) {
	screen := &MockScreen{}
	ctx := NewGameContext(screen)
	scheduler := NewClockScheduler(ctx)

	if scheduler.ctx != ctx {
		t.Error("Scheduler context not set correctly")
	}

	if scheduler.ticker == nil {
		t.Error("Scheduler ticker not initialized")
	}

	if scheduler.GetTickCount() != 0 {
		t.Errorf("Initial tick count = %d, want 0", scheduler.GetTickCount())
	}

	tickRate := scheduler.GetTickRate()
	if tickRate != 50*time.Millisecond {
		t.Errorf("Tick rate = %v, want 50ms", tickRate)
	}

	// Cleanup
	scheduler.Stop()
}

// TestClockSchedulerTicking tests that scheduler ticks
func TestClockSchedulerTicking(t *testing.T) {
	screen := &MockScreen{}
	ctx := NewGameContext(screen)
	scheduler := NewClockScheduler(ctx)

	// Start scheduler
	scheduler.Start()
	defer scheduler.Stop()

	// Wait for multiple ticks (50ms × 10 = 500ms)
	time.Sleep(550 * time.Millisecond)

	tickCount := scheduler.GetTickCount()
	// Should have ticked at least 8 times (allowing for timing variance)
	if tickCount < 8 {
		t.Errorf("Tick count = %d after 550ms, expected at least 8", tickCount)
	}

	// Should not have ticked more than 12 times (allowing for timing variance)
	if tickCount > 12 {
		t.Errorf("Tick count = %d after 550ms, expected at most 12", tickCount)
	}
}

// TestClockSchedulerStopIdempotent tests that Stop() can be called multiple times
func TestClockSchedulerStopIdempotent(t *testing.T) {
	screen := &MockScreen{}
	ctx := NewGameContext(screen)
	scheduler := NewClockScheduler(ctx)

	scheduler.Start()
	time.Sleep(100 * time.Millisecond)

	// Stop multiple times - should not panic
	scheduler.Stop()
	scheduler.Stop()
	scheduler.Stop()

	initialCount := scheduler.GetTickCount()

	// Wait a bit more
	time.Sleep(100 * time.Millisecond)

	// Tick count should not increase after stop
	finalCount := scheduler.GetTickCount()
	if finalCount != initialCount {
		t.Errorf("Tick count increased after stop: %d -> %d", initialCount, finalCount)
	}
}

// TestClockSchedulerConcurrentAccess tests thread-safe tick count reads
func TestClockSchedulerConcurrentAccess(t *testing.T) {
	screen := &MockScreen{}
	ctx := NewGameContext(screen)
	scheduler := NewClockScheduler(ctx)

	scheduler.Start()
	defer scheduler.Stop()

	const numReaders = 10
	const numReads = 50

	var wg sync.WaitGroup
	wg.Add(numReaders)

	// Launch concurrent readers
	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numReads; j++ {
				_ = scheduler.GetTickCount()
				_ = scheduler.GetTickRate()
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	wg.Wait()

	// Verify tick count is reasonable
	tickCount := scheduler.GetTickCount()
	if tickCount == 0 {
		t.Error("Expected non-zero tick count after concurrent access")
	}
}

// TestPhaseAndClockIntegration tests phase state changes during clock ticks
func TestPhaseAndClockIntegration(t *testing.T) {
	screen := &MockScreen{}
	ctx := NewGameContext(screen)
	scheduler := NewClockScheduler(ctx)

	// Verify initial phase
	if ctx.State.GetPhase() != PhaseNormal {
		t.Errorf("Initial phase = %v, want PhaseNormal", ctx.State.GetPhase())
	}

	scheduler.Start()
	defer scheduler.Stop()

	// Let it tick for a bit
	time.Sleep(200 * time.Millisecond)

	// Change phase during ticking
	ctx.State.SetPhase(PhaseGoldActive)
	time.Sleep(100 * time.Millisecond)

	// Verify phase persisted
	if ctx.State.GetPhase() != PhaseGoldActive {
		t.Errorf("Phase = %v, want PhaseGoldActive", ctx.State.GetPhase())
	}

	// Change to DecayWait
	ctx.State.SetPhase(PhaseDecayWait)
	time.Sleep(100 * time.Millisecond)

	if ctx.State.GetPhase() != PhaseDecayWait {
		t.Errorf("Phase = %v, want PhaseDecayWait", ctx.State.GetPhase())
	}

	// Verify ticks continued during phase changes
	tickCount := scheduler.GetTickCount()
	if tickCount < 6 { // 400ms / 50ms = 8 ticks expected, allow variance
		t.Errorf("Tick count = %d after 400ms, expected at least 6", tickCount)
	}
}

// TestClockSchedulerMemoryLeak tests for goroutine leaks
func TestClockSchedulerMemoryLeak(t *testing.T) {
	screen := &MockScreen{}
	ctx := NewGameContext(screen)

	// Create and destroy multiple schedulers
	for i := 0; i < 10; i++ {
		scheduler := NewClockScheduler(ctx)
		scheduler.Start()
		time.Sleep(20 * time.Millisecond)
		scheduler.Stop()
	}

	// If we reach here without hanging, test passes
	// (Goroutine leak would cause test timeout)
}
