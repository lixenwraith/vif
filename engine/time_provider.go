package engine

import "time"

// This abstraction allows for easier testing and ensures monotonic time usage.
// When used with PausableClock, provides game time that can be paused/resumed
// independently of wall clock time.
type TimeProvider interface {
	// Now returns the current time using a monotonic clock source.
	// The returned time includes both wall clock and monotonic clock readings.
	// For game time, this may return pausable time that stops during pause.
	Now() time.Time
}

// MonotonicTimeProvider provides the real system time with monotonic clock readings.
// Go's time.Now() includes a monotonic clock reading by default, which is
// unaffected by changes to the system wall clock.
// Used for real-time operations (UI, network) that should not pause.
type MonotonicTimeProvider struct{}

// NewMonotonicTimeProvider creates a new monotonic time provider
func NewMonotonicTimeProvider() *MonotonicTimeProvider {
	return &MonotonicTimeProvider{}
}

// Now returns the current time with monotonic clock reading
func (p *MonotonicTimeProvider) Now() time.Time {
	return time.Now()
}
