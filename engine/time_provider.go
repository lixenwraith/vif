package engine

import "time"

// TimeProvider provides the real system time with monotonic clock readings
// Used for real-time operations (UI, network) that should not pause
type TimeProvider struct{}

// NewTimeProvider creates a new monotonic time provider
func NewTimeProvider() *TimeProvider {
	return &TimeProvider{}
}

// Now returns the current time with monotonic clock reading
func (p *TimeProvider) Now() time.Time {
	return time.Now()
}