package engine

import "time"

// TimeProvider is an interface for getting the current time.
// This abstraction allows for easier testing and ensures monotonic time usage.
type TimeProvider interface {
	// Now returns the current time using a monotonic clock source.
	// The returned time includes both wall clock and monotonic clock readings.
	Now() time.Time
}

// MonotonicTimeProvider provides the real system time with monotonic clock readings.
// Go's time.Now() includes a monotonic clock reading by default, which is
// unaffected by changes to the system wall clock.
type MonotonicTimeProvider struct{}

// NewMonotonicTimeProvider creates a new monotonic time provider
func NewMonotonicTimeProvider() *MonotonicTimeProvider {
	return &MonotonicTimeProvider{}
}

// Now returns the current time with monotonic clock reading
func (p *MonotonicTimeProvider) Now() time.Time {
	return time.Now()
}
