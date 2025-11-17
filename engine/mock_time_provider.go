package engine

import (
	"sync"
	"time"
)

// MockTimeProvider provides a controllable time source for testing
type MockTimeProvider struct {
	mu          sync.RWMutex
	currentTime time.Time
}

// NewMockTimeProvider creates a new mock time provider with the given start time
func NewMockTimeProvider(startTime time.Time) *MockTimeProvider {
	return &MockTimeProvider{
		currentTime: startTime,
	}
}

// Now returns the current mocked time
func (m *MockTimeProvider) Now() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentTime
}

// SetTime sets the current time for the mock
func (m *MockTimeProvider) SetTime(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentTime = t
}

// Advance advances the current time by the given duration
func (m *MockTimeProvider) Advance(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentTime = m.currentTime.Add(d)
}
