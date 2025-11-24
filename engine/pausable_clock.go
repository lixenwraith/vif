package engine

import (
	"sync"
	"sync/atomic"
	"time"
)

// PausableClock provides pausable game time with pause duration tracking
type PausableClock struct {
	mu sync.RWMutex

	// Base time tracking
	realStartTime time.Time // When clock was created (real time)
	gameStartTime time.Time // Game time epoch (adjusted for pauses)

	// Pause state
	isPaused        atomic.Bool
	pauseStartTime  time.Time     // When current pause started (real time)
	totalPausedTime time.Duration // Cumulative pause duration

	// For external access to real time
	realTimeProvider TimeProvider
}

// NewPausableClock creates a new pausable clock
func NewPausableClock() *PausableClock {
	now := time.Now()
	return &PausableClock{
		realStartTime:    now,
		gameStartTime:    now,
		realTimeProvider: NewMonotonicTimeProvider(),
	}
}

// Now returns current game time (affected by pause)
func (pc *PausableClock) Now() time.Time {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	if pc.isPaused.Load() {
		// During pause: return frozen time at pause point
		return pc.gameStartTime.Add(pc.pauseStartTime.Sub(pc.realStartTime) - pc.totalPausedTime)
	}

	// After resume or during normal operation:
	// Game elapsed = real elapsed - total paused time
	realElapsed := time.Now().Sub(pc.realStartTime)
	gameElapsed := realElapsed - pc.totalPausedTime
	return pc.gameStartTime.Add(gameElapsed)
}

// RealTime returns actual wall clock time (unaffected by pause)
func (pc *PausableClock) RealTime() time.Time {
	return time.Now()
}

// Pause stops game time advancement
func (pc *PausableClock) Pause() {
	if pc.isPaused.CompareAndSwap(false, true) {
		pc.mu.Lock()
		defer pc.mu.Unlock()
		pc.pauseStartTime = time.Now()
	}
}

// Resume continues game time advancement
func (pc *PausableClock) Resume() {
	if pc.isPaused.CompareAndSwap(true, false) {
		pc.mu.Lock()

		if !pc.pauseStartTime.IsZero() {
			// Calculate pause duration and add to total
			pauseDuration := time.Now().Sub(pc.pauseStartTime)
			pc.totalPausedTime += pauseDuration
			pc.pauseStartTime = time.Time{}

			pc.mu.Unlock()

		} else {
			pc.mu.Unlock()
		}
	}
}

// IsPaused returns current pause state
func (pc *PausableClock) IsPaused() bool {
	return pc.isPaused.Load()
}

// GetTotalPauseDuration returns cumulative pause time
func (pc *PausableClock) GetTotalPauseDuration() time.Duration {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	total := pc.totalPausedTime
	if pc.isPaused.Load() && !pc.pauseStartTime.IsZero() {
		// Include current pause duration
		total += time.Now().Sub(pc.pauseStartTime)
	}
	return total
}

// GetCurrentPauseDuration returns duration of current pause (0 if not paused)
func (pc *PausableClock) GetCurrentPauseDuration() time.Duration {
	if !pc.isPaused.Load() {
		return 0
	}

	pc.mu.RLock()
	defer pc.mu.RUnlock()

	if pc.pauseStartTime.IsZero() {
		return 0
	}
	return time.Now().Sub(pc.pauseStartTime)
}
