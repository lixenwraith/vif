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
	resumeCallbacks []ResumeCallback

	// For external access to real time
	realTimeProvider TimeProvider
}

// ResumeCallback is called when clock resumes from pause
type ResumeCallback func(pauseDuration time.Duration)

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
		// Return time at pause point
		return pc.gameStartTime.Add(pc.pauseStartTime.Sub(pc.realStartTime) - pc.totalPausedTime)
	}

	// Calculate game time: current_real_time - start_time - total_paused
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
			pauseDuration := time.Now().Sub(pc.pauseStartTime)
			pc.totalPausedTime += pauseDuration
			pc.pauseStartTime = time.Time{}

			// Make a copy of callbacks to call outside lock
			callbacks := make([]ResumeCallback, len(pc.resumeCallbacks))
			copy(callbacks, pc.resumeCallbacks)
			pc.mu.Unlock()

			// Notify listeners about resume (outside lock to prevent deadlock)
			for _, cb := range callbacks {
				cb(pauseDuration)
			}
		} else {
			pc.mu.Unlock()
		}
	}
}

// OnResume registers a callback for pause resume events
func (pc *PausableClock) OnResume(cb ResumeCallback) {
	pc.mu.Lock()
	pc.mu.Unlock()
	pc.resumeCallbacks = append(pc.resumeCallbacks, cb)
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