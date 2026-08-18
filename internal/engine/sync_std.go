//go:build !wasm

package engine

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/status"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// LockHoldWarn [wall] is the update-mutex hold time above which a hold is reported.
// Race builds instrument every access, so the threshold scales with them.
var LockHoldWarn = lockHoldWarn()

func lockHoldWarn() time.Duration {
	if core.RaceEnabled {
		return 200 * time.Millisecond
	}
	return 20 * time.Millisecond
}

// sampleLocks gates hold-time stamping. Refreshed once per tick rather than
// probed on every acquire: this is the hottest lock in the process.
var sampleLocks atomic.Bool

// SetLockSampling enables hold-time stamping; called once per tick from
// processTick so the decision costs one atomic load per acquire
func SetLockSampling(on bool) { sampleLocks.Store(on) }

// UpdateMutex wraps sync.Mutex for game tick serialization.
// Hold time is sampled only while debug logging is active.
type UpdateMutex struct {
	acquired time.Time // holder-exclusive between Lock and Unlock
	mu       sync.Mutex
}

func (m *UpdateMutex) Lock() {
	m.mu.Lock()
	m.mark()
}

func (m *UpdateMutex) TryLock() bool {
	if !m.mu.TryLock() {
		return false
	}
	m.mark()
	return true
}

func (m *UpdateMutex) Unlock() {
	m.report()
	m.mu.Unlock()
}

// mark stamps the acquisition when sampling is active
func (m *UpdateMutex) mark() {
	if sampleLocks.Load() {
		m.acquired = time.Now()
		return
	}
	m.acquired = time.Time{}
}

// report emits holds exceeding LockHoldWarn, with the holder's call chain,
// and asks the flight recorder for the window around the stall
func (m *UpdateMutex) report() {
	if m.acquired.IsZero() {
		return
	}
	held := time.Since(m.acquired)
	m.acquired = time.Time{}
	if held <= LockHoldWarn {
		return
	}
	vlog.Trace("lock", vlog.LevelWarn, 4, "msg", "long hold", "us", held.Microseconds())
	status.Trigger(status.TrigLock)
}
