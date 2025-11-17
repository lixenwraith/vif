package engine

import (
	"testing"
	"time"
)

func TestMonotonicTimeProvider(t *testing.T) {
	provider := NewMonotonicTimeProvider()

	t1 := provider.Now()
	time.Sleep(10 * time.Millisecond)
	t2 := provider.Now()

	if !t2.After(t1) {
		t.Errorf("Expected t2 to be after t1, but got t1=%v, t2=%v", t1, t2)
	}

	// Check that the time has a monotonic component
	// In Go, time.Now() includes a monotonic clock reading by default
	diff := t2.Sub(t1)
	if diff < 10*time.Millisecond {
		t.Errorf("Expected at least 10ms difference, got %v", diff)
	}
}

func TestMockTimeProvider(t *testing.T) {
	startTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	mock := NewMockTimeProvider(startTime)

	// Test initial time
	now := mock.Now()
	if !now.Equal(startTime) {
		t.Errorf("Expected initial time to be %v, got %v", startTime, now)
	}

	// Test SetTime
	newTime := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	mock.SetTime(newTime)
	now = mock.Now()
	if !now.Equal(newTime) {
		t.Errorf("Expected time to be %v after SetTime, got %v", newTime, now)
	}

	// Test Advance
	mock.Advance(1 * time.Hour)
	now = mock.Now()
	expected := newTime.Add(1 * time.Hour)
	if !now.Equal(expected) {
		t.Errorf("Expected time to be %v after Advance, got %v", expected, now)
	}

	// Test multiple advances
	mock.Advance(30 * time.Minute)
	mock.Advance(15 * time.Minute)
	now = mock.Now()
	expected = newTime.Add(1*time.Hour + 30*time.Minute + 15*time.Minute)
	if !now.Equal(expected) {
		t.Errorf("Expected time to be %v after multiple advances, got %v", expected, now)
	}
}

func TestMockTimeProviderConcurrency(t *testing.T) {
	startTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	mock := NewMockTimeProvider(startTime)

	// Test concurrent reads and writes
	done := make(chan bool)

	// Multiple readers
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = mock.Now()
			}
			done <- true
		}()
	}

	// Multiple writers
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				mock.Advance(1 * time.Millisecond)
			}
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 15; i++ {
		<-done
	}

	// Verify the time advanced by 5 * 50 * 1ms = 250ms
	expected := startTime.Add(250 * time.Millisecond)
	now := mock.Now()
	if !now.Equal(expected) {
		t.Errorf("Expected time to be %v after concurrent operations, got %v", expected, now)
	}
}

func TestTimeProviderInterface(t *testing.T) {
	// Test that both implementations satisfy the TimeProvider interface
	var _ TimeProvider = &MonotonicTimeProvider{}
	var _ TimeProvider = &MockTimeProvider{}
}
